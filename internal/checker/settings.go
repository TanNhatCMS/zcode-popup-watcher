// settings.go — tuỳ chọn chạy nền của tool, lưu tại
// ~/.zcode/mod/deal-settings.json:
//
//	check_account       — tài khoản dùng gọi API check deal:
//	                      "default" (đang login trong ZCode) hoặc
//	                      email/id của tài khoản trong cockpit-tools.
//	check_window_enabled — true: NGOÀI khung giờ KHÔNG gọi API check
//	                      (deal đêm thường phát ~18h); chỉ ngủ chờ.
//	check_from/check_to  — khung giờ HH:MM theo giờ VN, mặc định
//	                      18:00–23:00 (deal đêm thường phát 18h).
//
// Sửa từ menu tray (hoặc sửa file) có hiệu lực ngay vòng poll kế
// tiếp — poll loop nạp lại settings mỗi vòng, không cần restart.
package checker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// DealSettings là bộ tuỳ chọn check deal của tool.
type DealSettings struct {
	CheckAccount       string `json:"check_account"`
	CheckWindowEnabled bool   `json:"check_window_enabled"`
	CheckFrom          string `json:"check_from"`
	CheckTo            string `json:"check_to"`
	// CheckInterval là tần suất poll (giây) khi settings đặt > 0;
	// 0 = dùng interval mặc định / flag --interval của CLI.
	CheckInterval int `json:"check_interval"`
}

const (
	defaultCheckFrom    = "18:00"
	defaultCheckTo      = "23:00"
	checkAccountDefault = "default"
)

// DefaultDealSettings trả bộ giá trị mặc định: check bằng tài khoản
// đang login ZCode, chỉ check trong khung 18:00–23:00 giờ VN.
func DefaultDealSettings() DealSettings {
	return DealSettings{
		CheckAccount:       checkAccountDefault,
		CheckWindowEnabled: true,
		CheckFrom:          defaultCheckFrom,
		CheckTo:            defaultCheckTo,
	}
}

// DealSettingsFile là đường dẫn file settings trong state dir.
func DealSettingsFile() string {
	return filepath.Join(StateDir(), "deal-settings.json")
}

// LoadDealSettings đọc file settings; thiếu field nào thì giữ giá trị
// mặc định của field đó, file hỏng thì về mặc định toàn bộ (fail-open).
func LoadDealSettings() DealSettings {
	s := DefaultDealSettings()
	data, err := os.ReadFile(DealSettingsFile())
	if err != nil {
		return s
	}
	var saved DealSettings
	if err := json.Unmarshal(data, &saved); err != nil {
		return s
	}
	if strings.TrimSpace(saved.CheckAccount) != "" {
		s.CheckAccount = strings.TrimSpace(saved.CheckAccount)
	}
	s.CheckWindowEnabled = saved.CheckWindowEnabled
	if t := strings.TrimSpace(saved.CheckFrom); t != "" {
		s.CheckFrom = t
	}
	if t := strings.TrimSpace(saved.CheckTo); t != "" {
		s.CheckTo = t
	}
	if saved.CheckInterval > 0 {
		s.CheckInterval = saved.CheckInterval
	}
	return s
}

// ParseHHMM là wrapper công khai của parseHHMM cho UI cài đặt validate
// giờ người dùng nhập ("HH:MM" giờ VN).
func ParseHHMM(s string) (int, bool) {
	return parseHHMM(s)
}

// SaveDealSettings ghi settings (atomic không cần thiết — file nhỏ,
// ghi đè trực tiếp; hỏng 1 lần ghi chỉ làm settings về default).
func SaveDealSettings(s DealSettings) error {
	EnsureStateDir()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(DealSettingsFile(), data, 0o644)
}

// parseHHMM đọc "HH:MM" → số phút trong ngày; sai định dạng trả false.
func parseHHMM(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	// atoiCheck trả bool = "hợp lệ" (không phải err!) — điều kiện hỏng
	// là !ok.
	h, okH := atoiCheck(parts[0])
	m, okM := atoiCheck(parts[1])
	if !okH || !okM || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func atoiCheck(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 2 {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// InCheckWindow kiểm tra số phút trong ngày (giờ VN) có nằm trong
// khung check không. from > to coi như khung qua nửa đêm (22:00–02:00).
// Cấu hình sai/from==to → fail-open (luôn trong khung) để lỗi cấu hình
// không âm thầm tắt check.
func (s DealSettings) InCheckWindow(minutes int) bool {
	from, ok1 := parseHHMM(s.CheckFrom)
	to, ok2 := parseHHMM(s.CheckTo)
	if !ok1 || !ok2 || from == to {
		return true
	}
	if from <= to {
		return minutes >= from && minutes <= to
	}
	return minutes >= from || minutes <= to
}

// ResolveCheckAccount trả tài khoản cockpit tương ứng
// settings.check_account; found=false nghĩa là dùng tài khoản mặc định
// (đang login trong ZCode) hoặc không tìm thấy (caller fallback + log).
func (s DealSettings) ResolveCheckAccount() (CockpitAccount, bool) {
	if s.CheckAccount == "" || strings.EqualFold(s.CheckAccount, checkAccountDefault) {
		return CockpitAccount{}, false
	}
	for _, acc := range ListCockpitZcodeAccounts() {
		if strings.EqualFold(acc.Email, s.CheckAccount) || strings.EqualFold(acc.ID, s.CheckAccount) {
			return acc, true
		}
	}
	return CockpitAccount{}, false
}
