// Package checker port logic kiểm tra deal ZCode từ deal-checker.ts
// (Bun) sáng Go. Chỉ dùng stdlib: HTTP, AES-256-GCM, JSON, file.
// State dùng chung với CLI o ~/.zcode/mod (seen_deals.json,
// deal-checker.log, deal-notify.json) để deal đã thấy không bị
// báo lại, du chạy bằng CLI hay tray Go.
package checker

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var APIURL = "https://zcode.z.ai/api/v1/marketing/touch"

const DefaultInterval = 600 * time.Second

// StateDir trả về ~/.zcode/mod (HOME trên Linux/macOS,
// USERPROFILE trên Windows qua os.UserHomeDir).
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".zcode", "mod")
}

func SeenFile() string      { return filepath.Join(StateDir(), "seen_deals.json") }
func LogFile() string       { return filepath.Join(StateDir(), "deal-checker.log") }
func NotifyFile() string    { return filepath.Join(StateDir(), "deal-notify.json") }
func GotrayPidFile() string { return filepath.Join(StateDir(), "gotray.pid") }

func EnsureStateDir() {
	_ = os.MkdirAll(StateDir(), 0o755)
}

// AppendLog ghi 1 dòng có timestamp vào log, giu tới đã ~200 dòng
// cuối để log không phi to vo han.
func AppendLog(line string) {
	EnsureStateDir()
	stamped := fmt.Sprintf("[%s] %s\n", time.Now().UTC().Format(time.RFC3339), line)
	existing, _ := os.ReadFile(LogFile())
	combined := string(existing) + stamped
	lines := strings.Split(combined, "\n")
	if len(lines) > 201 {
		lines = lines[len(lines)-201:]
	}
	_ = os.WriteFile(LogFile(), []byte(strings.Join(lines, "\n")), 0o644)
}

// ConfigDir trả về thử mục ~/.zcode/v2, ton trong
// $ZCODE_DATA_BASE_DIR nếu có (giống ZCode).
func ConfigDir(override string) string {
	if override != "" {
		return override
	}
	if base := strings.TrimSpace(os.Getenv("ZCODE_DATA_BASE_DIR")); base != "" {
		return filepath.Join(base, ".zcode", "v2")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".zcode", "v2")
}

func readJSONFile(path string) (map[string]any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, false
	}
	return parsed, true
}

func ReadDeviceMid(configDir string) (string, bool) {
	data, ok := readJSONFile(filepath.Join(configDir, "telemetry-state.json"))
	if !ok {
		return "", false
	}
	value, ok := data["deviceMid"].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// cipherKey trả về SHA256(secret), secret giống ZCode:
// $ZCODE_CREDENTIAL_SECRET nếu có, nếu không thì fallback
// "zcode-credential-fallback:<platform>:<homedir>:<username>".
// Username trên Windows có thể kèm domain (DOMAIN\user) thì cắt
// lấy phần sau dấu \ cho khop với Node os.userInfo().username.
func cipherKey() []byte {
	if secret := strings.TrimSpace(os.Getenv("ZCODE_CREDENTIAL_SECRET")); secret != "" {
		sum := sha256.Sum256([]byte(secret))
		return sum[:]
	}
	home, _ := os.UserHomeDir()
	username := currentUsername()
	platform := runtimeGOOS()
	secret := fmt.Sprintf("zcode-credential-fallback:%s:%s:%s", platform, home, username)
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func DecryptCredential(value string) (string, error) {
	const prefix = "enc:v1:"
	if !strings.HasPrefix(value, prefix) {
		return value, nil
	}
	parts := strings.Split(value[len(prefix):], ".")
	if len(parts) != 3 {
		return "", errors.New("credential ciphertext format invalid")
	}
	iv, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	authTag, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	cipherText, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	if len(iv) != 12 {
		return "", errors.New("credential IV length invalid")
	}
	if len(authTag) != 16 {
		return "", errors.New("credential authTag length invalid")
	}
	block, err := aes.NewCipher(cipherKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, iv, append(cipherText, authTag...), nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func ReadZcodeJWT(configDir string) (string, bool) {
	data, ok := readJSONFile(filepath.Join(configDir, "credentials.json"))
	if !ok {
		return "", false
	}
	raw, ok := data["zcodejwttoken"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", false
	}
	plain, err := DecryptCredential(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(plain) == "" {
		return "", false
	}
	return strings.TrimSpace(plain), true
}

// Deal là 1 delivery đã loc (mỗi vị trí chỉ lấy 1 cài đầu tiên).
type Deal struct {
	Key        string
	CampaignID string
	Position   string
	Priority   any
	Title      string
	Detail     string
}

func textContent(node any) string {
	if m, ok := node.(map[string]any); ok {
		if content, ok := m["content"].(string); ok {
			return strings.TrimSpace(content)
		}
	}
	return ""
}

func buttonLabels(buttons any) []string {
	list, ok := buttons.([]any)
	if !ok {
		return nil
	}
	var labels []string
	for _, b := range list {
		if m, ok := b.(map[string]any); ok {
			if label := textContent(m["text"]); label != "" {
				labels = append(labels, label)
			}
		}
	}
	return labels
}

// ParseSnapshot loc snapshot marketing/touch thành deal,
// giống app gốc: mỗi vị trí (banner/popup) chỉ lấy 1 cài đầu tiên.
func ParseSnapshot(payload []byte) ([]Deal, error) {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, err
	}
	code, _ := root["code"].(float64)
	if code != 0 {
		return nil, fmt.Errorf("snapshot code != 0 (got %v)", root["code"])
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		return nil, errors.New("snapshot missing data")
	}
	deliveries, ok := data["deliveries"].([]any)
	if !ok {
		return nil, errors.New("snapshot missing data.deliveries")
	}
	seenPositions := map[string]bool{}
	var results []Deal
	for _, raw := range deliveries {
		delivery, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		campaignID, _ := delivery["campaign_id"].(string)
		position, _ := delivery["resource_position"].(string)
		if strings.TrimSpace(campaignID) == "" {
			continue
		}
		if position != "banner" && position != "popup" {
			continue
		}
		if seenPositions[position] {
			continue
		}
		seenPositions[position] = true
		var title, detail string
		if position == "banner" {
			banner, _ := delivery["banner"].(map[string]any)
			labels := buttonLabels(banner["buttons"])
			if sp, ok := banner["success_popup"].(map[string]any); ok {
				title = textContent(sp["title"])
			}
			detail = strings.Join(labels, " / ")
			if detail == "" {
				detail = "(banner, không có nút đọc được)"
			}
		} else {
			popup, _ := delivery["popup"].(map[string]any)
			title = textContent(popup["title"])
			desc := textContent(popup["description"])
			labels := buttonLabels(popup["buttons"])
			parts := []string{}
			if desc != "" {
				parts = append(parts, desc)
			}
			if len(labels) > 0 {
				parts = append(parts, strings.Join(labels, " / "))
			}
			detail = strings.Join(parts, " - ")
		}
		results = append(results, Deal{
			Key:        position + ":" + campaignID,
			CampaignID: campaignID,
			Position:   position,
			Priority:   delivery["priority"],
			Title:      title,
			Detail:     detail,
		})
	}
	return results, nil
}

func LoadSeen() map[string]bool {
	seen := map[string]bool{}
	data, err := os.ReadFile(SeenFile())
	if err != nil {
		return seen
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return seen
	}
	for _, key := range keys {
		seen[key] = true
	}
	return seen
}

func SaveSeen(seen map[string]bool) {
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	data, _ := json.MarshalIndent(keys, "", "  ")
	EnsureStateDir()
	if err := os.WriteFile(SeenFile(), data, 0o644); err != nil {
		AppendLog(fmt.Sprintf("[gotray] không ghi được seen file: %v", err))
	}
}

// QueryDeals gọi GET marketing/touch, timeout 15s.
func QueryDeals(token, deviceMid, locale, appVersion string, seq int) ([]Deal, error) {
	if override := strings.TrimSpace(os.Getenv("ZCODE_MARKETING_URL")); override != "" {
		APIURL = override
	}
	u := APIURL + "?seq=" + fmt.Sprintf("%d", seq)
	if _, err := url.Parse(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Device-Mid", deviceMid)
	req.Header.Set("X-Client-Language", locale)
	req.Header.Set("X-ZCode-App-Version", appVersion)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d từ marketing/touch", resp.StatusCode)
	}
	var body []byte
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 8192)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	body = buf
	return ParseSnapshot(body)
}

// WriteNotifyFile ghi file notify (chỉ key campaign, tieu để,
// nội dùng - không bao giờ có token) bằng cách ghi tmp rồi rename.
func WriteNotifyFile(id, title, message string) {
	EnsureStateDir()
	payload := map[string]string{
		"id":      id,
		"title":   title,
		"message": message,
		"time":    time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	tmp := NotifyFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, NotifyFile())
}
