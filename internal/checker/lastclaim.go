// lastclaim.go — vé lần NHẬN DEAL GẦN NHẤT, lưu để menu "Có deal mới -
// ấn xem chi tiết" mở lại được sau khi đã claim (pending-claim.json bị
// xoá sau khi nhận). File ~/.zcode/mod/last-claim.json — không chứa
// token, chỉ dữ liệu hiển thị.
package checker

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LastClaim là bản ghi lần nhận deal gần nhất.
type LastClaim struct {
	PlanName    string `json:"plan_name"`
	AmountRaw   int64  `json:"amount_raw"`
	AmountWord  string `json:"amount_word"`
	AmountShort string `json:"amount_short"`
	Model       string `json:"model"`
	Account     string `json:"account"`
	EndsAt      int64  `json:"ends_at"`
	ClaimedAt   string `json:"claimed_at"`
}

// LastClaimFile là đường dẫn file lưu vé lần nhận gần nhất.
func LastClaimFile() string {
	return filepath.Join(StateDir(), "last-claim.json")
}

// SaveLastClaim ghi vé lần nhận gần nhất (ghi đè — chỉ cần bản mới nhất).
func SaveLastClaim(l LastClaim) error {
	EnsureStateDir()
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(LastClaimFile(), data, 0o644)
}

// LoadLastClaim đọc vé lần nhận gần nhất; chưa có/lỗi → false.
func LoadLastClaim() (LastClaim, bool) {
	data, err := os.ReadFile(LastClaimFile())
	if err != nil {
		return LastClaim{}, false
	}
	var l LastClaim
	if err := json.Unmarshal(data, &l); err != nil {
		return LastClaim{}, false
	}
	if l.PlanName == "" && l.Model == "" {
		return LastClaim{}, false
	}
	return l, true
}
