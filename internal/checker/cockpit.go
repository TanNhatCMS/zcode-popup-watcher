// cockpit.go — đọc danh sách tài khoản ZCode từ cockpit-tools
// (https://github.com/TanNhatCMS/cockpit-tools) để popup cho phép
// CHỌN tài khoản nhận deal:
//
//	~/.cockpit_tools/zcode_accounts.json        — index (summary)
//	~/.cockpit_tools/zcode_accounts/<id>.json   — chi tiết, chứa
//	                                              zcode_jwt_token (plain)
//
// Data dir resolve giống cockpit: COCKPIT_TOOLS_DATA_DIR >
// ~/.cockpit_tools (profile dev → ~/.cockpit_tools_dev). File chỉ
// đọc, không ghi/sửa gì; JWT dùng để claim thay mặt tài khoản đã
// chọn, không bao giờ in ra hay ghi file.
package checker

import (
	"encoding/json"
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
// đó ~/.cockpit_tools (dev profile → ~/.cockpit_tools_dev).
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

// ListCockpitZcodeAccounts đọc index zcode_accounts.json rồi load
// zcode_jwt_token từ từng file chi tiết. Lỗi từng file bỏ qua (account
// hỏng không chặn các account khác); không có cockpit-tools → rỗng.
func ListCockpitZcodeAccounts() []CockpitAccount {
	indexPath := filepath.Join(cockpitDataDir(), "zcode_accounts.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil
	}
	var index struct {
		CurrentAccountID string `json:"current_account_id"`
		Accounts         []struct {
			ID       string `json:"id"`
			Email    string `json:"email"`
			PlanType string `json:"plan_type"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil
	}
	accountsDir := filepath.Join(cockpitDataDir(), "zcode_accounts")
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
		// File chi tiết chứa zcode_jwt_token plain — đọc lỗi thì bỏ qua
		// account này (không có JWT thì không claim được).
		fileData, err := os.ReadFile(filepath.Join(accountsDir, id+".json"))
		if err != nil {
			continue
		}
		var detail struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			PlanType    string `json:"plan_type"`
			JWT         string `json:"zcode_jwt_token"`
		}
		if err := json.Unmarshal(fileData, &detail); err != nil {
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
