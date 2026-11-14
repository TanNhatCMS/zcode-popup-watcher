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
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
// PlanID/ButtonLabel/ClaimURL có giá trị khi deal có nút nhận được
// (action "claim_zcode_plan" / "open_url" trong payload app gốc).
// Desc là mô tả đã chuyển sang text thuần (format html/markdown được
// strip như app gốc — xem text.go). Hero là ảnh quảng cáo (marketing
// asset, app gốc download + verify SHA-256 trước khi vẽ).
type Deal struct {
	Key         string
	CampaignID  string
	Position    string
	Priority    any
	Title       string
	Detail      string // dòng tóm tắt cho toast/log (desc + nhãn nút)
	Desc        string // mô tả đầy đủ dạng text thuần cho popup
	PlanID      string // action claim_zcode_plan → plan nhận được
	ButtonLabel string // nhãn nút nhận, ví dụ "Nhận ngay"
	ClaimURL    string // action open_url → link mở (nếu không phải claim)
	HeroSrc     string // URL ảnh banner/hero
	HeroSHA256  string // sha256 của ảnh (verify fail-closed như app)
	ZcodePlan   *ZcodePlanHeroArgs // args.zcode_plan — dữ liệu vé (số token, hạn)
}

// Claimable trả về true nếu deal có nút nhận tool có thể thay bấm
// (claim qua API billing/claim) hoặc mở link claim.
func (d Deal) Claimable() bool {
	return d.PlanID != "" || d.ClaimURL != ""
}

// heroAsset đọc visual marketing kiểu image: {type:"image",
// image:{default:{src, sha256}}}, trả về (src, sha256) hoặc rỗng.
func heroAsset(visual any) (string, string) {
	m, ok := visual.(map[string]any)
	if !ok {
		return "", ""
	}
	if visualType, _ := m["type"].(string); visualType != "image" {
		return "", "" // video/bundle: popup native không vẽ được, bỏ qua
	}
	image, ok := m["image"].(map[string]any)
	if !ok {
		return "", ""
	}
	def, ok := image["default"].(map[string]any)
	if !ok {
		return "", ""
	}
	src, _ := def["src"].(string)
	sha, _ := def["sha256"].(string)
	return strings.TrimSpace(src), strings.TrimSpace(sha)
}

// zcodePlanArgs đọc args.zcode_plan từ visual — dữ liệu tấm vé sau khi
// nhận (heroData() trong marketingResources.ts của app gốc): plan name,
// ends_at (epoch giây), entitlements (meter "model_usage" là quota
// chính). Trả nil nếu visual không khai báo.
func zcodePlanArgs(visual any) *ZcodePlanHeroArgs {
	m, ok := visual.(map[string]any)
	if !ok {
		return nil
	}
	args, ok := m["args"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := args["zcode_plan"].(map[string]any)
	if !ok {
		return nil
	}
	plan := &ZcodePlanHeroArgs{}
	if v, ok := raw["name"].(string); ok {
		plan.Name = strings.TrimSpace(v)
	}
	if v, ok := raw["ends_at"].(float64); ok && v >= 0 {
		plan.EndsAt = int64(v)
	}
	if ents, ok := raw["entitlements"].([]any); ok {
		for _, e := range ents {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			ent := ClaimEntitlement{}
			if v, ok := em["meter"].(string); ok {
				ent.Meter = strings.TrimSpace(v)
			}
			if v, ok := em["show_name"].(string); ok {
				ent.ShowName = strings.TrimSpace(v)
			}
			if v, ok := em["unit_type"].(string); ok {
				ent.UnitType = strings.TrimSpace(v)
			}
			if v, ok := em["grant_units"].(float64); ok {
				ent.GrantUnits = v
			}
			if ent.ShowName == "" && ent.GrantUnits == 0 {
				continue
			}
			plan.Entitlements = append(plan.Entitlements, ent)
		}
	}
	if plan.Name == "" && len(plan.Entitlements) == 0 {
		return nil
	}
	return plan
}

func textContent(node any) string {
	if m, ok := node.(map[string]any); ok {
		if content, ok := m["content"].(string); ok {
			return strings.TrimSpace(content)
		}
	}
	return ""
}

// parseButtons đọc mảng buttons của payload app gốc: mỗi nút có
// {text: {format, content}, action: {type, args}} (marketingTouch.ts).
func parseButtons(buttons any) []map[string]any {
	list, ok := buttons.([]any)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, b := range list {
		if m, ok := b.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

func buttonText(button map[string]any) string {
	if text, ok := button["text"].(map[string]any); ok {
		return textContent(text)
	}
	return ""
}

// buttonClaimArgs trả về (plan_id) nếu nút có action claim_zcode_plan.
func buttonClaimPlanID(button map[string]any) string {
	action, ok := button["action"].(map[string]any)
	if !ok {
		return ""
	}
	if actionType, _ := action["type"].(string); actionType != "claim_zcode_plan" {
		return ""
	}
	args, ok := action["args"].(map[string]any)
	if !ok {
		return ""
	}
	planID, _ := args["plan_id"].(string)
	return strings.TrimSpace(planID)
}

// buttonURL trả về url nếu nút có action open_url (chỉ nhận http/https,
// giống schema marketingTouch.ts của app gốc).
func buttonURL(button map[string]any) string {
	action, ok := button["action"].(map[string]any)
	if !ok {
		return ""
	}
	if actionType, _ := action["type"].(string); actionType != "open_url" {
		return ""
	}
	args, ok := action["args"].(map[string]any)
	if !ok {
		return ""
	}
	raw, _ := args["url"].(string)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return raw
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
		var title, detail, desc, planID, buttonLabel, claimURL, heroSrc, heroSHA string
		var zplan *ZcodePlanHeroArgs
		if position == "banner" {
			banner, _ := delivery["banner"].(map[string]any)
			// App gốc clickBanner: nút action != "close" đầu tiên là nút chính.
			for _, button := range parseButtons(banner["buttons"]) {
				action, _ := button["action"].(map[string]any)
				if actionType, _ := action["type"].(string); actionType == "close" {
					continue
				}
				if id := buttonClaimPlanID(button); id != "" {
					planID, buttonLabel = id, buttonText(button)
				} else if link := buttonURL(button); link != "" {
					claimURL, buttonLabel = link, buttonText(button)
				}
				break
			}
			// Banner không có title/description riêng; nội dung hiển thị
			// nằm ở success_popup (app chỉ hiện banner khi bấm xong).
			if sp, ok := banner["success_popup"].(map[string]any); ok {
				tFormat, tContent := formattedText(sp["title"])
				title = plainText(tFormat, tContent)
				dFormat, dContent := formattedText(sp["description"])
				desc = plainText(dFormat, dContent)
				zplan = zcodePlanArgs(sp["hero"])
			}
			heroSrc, heroSHA = heroAsset(banner["background"])
			labels := []string{}
			for _, button := range parseButtons(banner["buttons"]) {
				if label := buttonText(button); label != "" {
					labels = append(labels, label)
				}
			}
			detail = strings.Join(labels, " / ")
			if detail == "" {
				detail = "(banner, không có nút đọc được)"
			}
		} else {
			popup, _ := delivery["popup"].(map[string]any)
			tFormat, tContent := formattedText(popup["title"])
			title = plainText(tFormat, tContent)
			dFormat, dContent := formattedText(popup["description"])
			desc = plainText(dFormat, dContent)
			heroSrc, heroSHA = heroAsset(popup["hero"])
			zplan = zcodePlanArgs(popup["hero"])
			labels := []string{}
			for _, button := range parseButtons(popup["buttons"]) {
				if label := buttonText(button); label != "" {
					labels = append(labels, label)
				}
				// Nút claim ưu tiên hơn link: planID tìm thấy thì không
				// ghi đè bằng open_url nữa.
				if id := buttonClaimPlanID(button); id != "" && planID == "" {
					planID, buttonLabel = id, buttonText(button)
				} else if link := buttonURL(button); link != "" && claimURL == "" && planID == "" {
					claimURL = link
					buttonLabel = buttonText(button)
				}
			}
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
			Key:         position + ":" + campaignID,
			CampaignID:  campaignID,
			Position:    position,
			Priority:    delivery["priority"],
			Title:       title,
			Detail:      detail,
			Desc:        desc,
			PlanID:      planID,
			ButtonLabel: buttonLabel,
			ClaimURL:    claimURL,
			HeroSrc:     heroSrc,
			HeroSHA256:  heroSHA,
			ZcodePlan:   zplan,
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

// QueryDeals gọi GET marketing/touch, timeout 15s, giống app gốc.
func QueryDeals(token, deviceMid, locale, appVersion string, seq int) ([]Deal, error) {
	u := marketingEndpoint("/api/v1/marketing/touch") + "?seq=" + fmt.Sprintf("%d", seq)
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
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d từ marketing/touch", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
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

// PendingClaim là deal có thể nhận đang chờ người dùng quyết định
// (bấm popup hoặc chạy `zcode-deal claim`). File này KHÔNG chứa
// token — claim đọc token từ ~/.zcode/v2 tại thời điểm chạy.
type PendingClaim struct {
	CampaignID  string `json:"campaign_id"`
	Position    string `json:"position"`
	PlanID      string `json:"plan_id,omitempty"`
	ButtonLabel string `json:"button_label,omitempty"`
	ClaimURL    string `json:"claim_url,omitempty"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	SavedAt     string `json:"saved_at"`
}

func PendingClaimFile() string { return filepath.Join(StateDir(), "pending-claim.json") }

// SavePendingClaim ghi deal chờ nhận (ghi đè deal cũ — deal mới ưu
// tiên) bằng cách ghi tmp rồi rename để không ghi dở dang.
func SavePendingClaim(deal Deal) {
	pending := PendingClaim{
		CampaignID:  deal.CampaignID,
		Position:    deal.Position,
		PlanID:      deal.PlanID,
		ButtonLabel: deal.ButtonLabel,
		ClaimURL:    deal.ClaimURL,
		Title:       deal.Title,
		Detail:      deal.Detail,
		SavedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return
	}
	EnsureStateDir()
	tmp := PendingClaimFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		AppendLog(fmt.Sprintf("[claim] không ghi được pending-claim: %v", err))
		return
	}
	_ = os.Rename(tmp, PendingClaimFile())
}

func LoadPendingClaim() (PendingClaim, bool) {
	data, err := os.ReadFile(PendingClaimFile())
	if err != nil {
		return PendingClaim{}, false
	}
	var pending PendingClaim
	if err := json.Unmarshal(data, &pending); err != nil {
		return PendingClaim{}, false
	}
	if strings.TrimSpace(pending.CampaignID) == "" {
		return PendingClaim{}, false
	}
	return pending, true
}

func ClearPendingClaim() {
	_ = os.Remove(PendingClaimFile())
}
