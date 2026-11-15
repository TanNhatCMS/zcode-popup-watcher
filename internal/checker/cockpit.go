// cockpit.go — đọc danh sách tài khoản ZCode từ cockpit-tools
// (https://github.com/TanNhatCMS/cockpit-tools) để popup cho phép
// CHỌN tài khoản nhận deal:
//
//	~/.cockpit_tools/zcode_accounts.json            — index (plaintext)
//	~/.cockpit_tools/zcode_accounts/<id>.json       — chi tiết MÃ HÓA
//	  AES-256-GCM envelope {version, nonce, ciphertext} (account_file_
//	  crypto.rs + secure_account_storage.rs), key 32-byte base64 lưu
//	  ~/.cockpit_tools/secure-account-storage.key
//
// Data dir resolve giống cockpit: COCKPIT_TOOLS_DATA_DIR >
// ~/.cockpit_tools (profile dev → ~/.cockpit_tools_dev). Chỉ ĐỌC, không
// ghi/sửa gì; JWT dùng để claim thay mặt tài khoản đã chọn, không bao
// giờ in ra hay ghi file.
package checker

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CockpitAccount là 1 tài khoản ZCode lưu trong cockpit-tools.
type CockpitAccount struct {
	ID          string
	Email       string
	DisplayName string
	PlanType    string
	JWT         string // zcode_jwt_token — dùng để claim thay user
}

// cockpitDataDir resolve thư mục dữ liệu của cockpit-tools giống
// data_paths.rs của cockpit: env COCKPIT_TOOLS_DATA_DIR ưu tiên, sau
// đó ~/.cockpit_tools (profile dev → ~/.cockpit_tools_dev).
func cockpitDataDir() string {
	if base := strings.TrimSpace(os.Getenv("COCKPIT_TOOLS_DATA_DIR")); base != "" {
		return base
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	suffix := ""
	if strings.EqualFold(strings.TrimSpace(os.Getenv("COCKPIT_TOOLS_PROFILE")), "dev") {
		suffix = "_dev"
	}
	return filepath.Join(home, ".cockpit_tools"+suffix)
}

// cockpitAccountKey đọc key 32-byte từ secure-account-storage.key
// (file chứa key base64 chuẩn, có thể có xuống dòng).
func cockpitAccountKey(dataDir string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, "secure-account-storage.key"))
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("key file không phải base64 hợp lệ: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("key dài %d bytes, kỳ vọng 32", len(key))
	}
	return key, nil
}

// decryptCockpitAccountFile giải mã envelope AES-256-GCM của cockpit
// (account_file_crypto.rs: nonce base64 12 bytes, ciphertext base64
// có GCM tag ở cuối). File legacy plaintext (không có envelope) được
// passthrough — cockpit cũng load legacy như vậy.
func decryptCockpitAccountFile(content []byte, key []byte) ([]byte, error) {
	var envelope struct {
		Version    int    `json:"version"`
		Algorithm  string `json:"algorithm"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		return nil, err
	}
	if envelope.Nonce == "" || envelope.Ciphertext == "" || envelope.Algorithm != "AES-256-GCM" {
		// Không phải envelope mã hóa → trả nguyên văn (file legacy).
		return content, nil
	}
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelope.Nonce))
	if err != nil {
		return nil, fmt.Errorf("nonce base64 lỗi: %w", err)
	}
	if len(nonce) != 12 {
		return nil, fmt.Errorf("nonce dài %d bytes, kỳ vọng 12", len(nonce))
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelope.Ciphertext))
	if err != nil {
		return nil, fmt.Errorf("ciphertext base64 lỗi: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("giải mã AES-256-GCM thất bại (key hoặc dữ liệu sai): %w", err)
	}
	return plain, nil
}

// ListCockpitZcodeAccounts đọc index zcode_accounts.json rồi giải mã
// từng file chi tiết để lấy zcode_jwt_token. Lỗi từng account bỏ qua
// (account hỏng không chặn các account khác); không có cockpit-tools
// hoặc không giải mã được key → rỗng (chỉ còn tài khoản mặc định).
func ListCockpitZcodeAccounts() []CockpitAccount {
	dataDir := cockpitDataDir()
	indexPath := filepath.Join(dataDir, "zcode_accounts.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil
	}
	var index struct {
		Accounts []struct {
			ID       string `json:"id"`
			Email    string `json:"email"`
			PlanType string `json:"plan_type"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil
	}
	key, err := cockpitAccountKey(dataDir)
	if err != nil {
		return nil
	}
	accountsDir := filepath.Join(dataDir, "zcode_accounts")
	var out []CockpitAccount
	for _, entry := range index.Accounts {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		acc := CockpitAccount{
			ID:       id,
			Email:    strings.TrimSpace(entry.Email),
			PlanType: strings.TrimSpace(entry.PlanType),
		}
		// File chi tiết mã hóa AES-256-GCM — giải mã để lấy JWT.
		fileData, err := os.ReadFile(filepath.Join(accountsDir, id+".json"))
		if err != nil {
			continue
		}
		plain, err := decryptCockpitAccountFile(fileData, key)
		if err != nil {
			continue
		}
		var detail struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			PlanType    string `json:"plan_type"`
			JWT         string `json:"zcode_jwt_token"`
		}
		if err := json.Unmarshal(plain, &detail); err != nil {
			continue
		}
		acc.JWT = strings.TrimSpace(detail.JWT)
		if acc.JWT == "" {
			// Không có JWT thì không claim được — bỏ qua luôn ở tầng
			// checker để mọi caller nhận danh sách "claim được".
			continue
		}
		if acc.Email == "" {
			acc.Email = strings.TrimSpace(detail.Email)
		}
		if acc.PlanType == "" {
			acc.PlanType = strings.TrimSpace(detail.PlanType)
		}
		acc.DisplayName = strings.TrimSpace(detail.DisplayName)
		out = append(out, acc)
	}
	return out
}
