// quota.go — dữ liệu + format hiển thị "quota" của plan (số token lớn
// và dòng quyền lợi) port từ heroData() của app gốc
// (marketingResources.ts): app đọc args.zcode_plan trong hero của
// delivery, lấy entitlements có meter == "model_usage", number lớn là
// grant_units lớn nhất comma-format, dòng benefit là
// "show_name · N unit_type".
package checker

import "fmt"

// ZcodePlanHeroArgs là args.zcode_plan trong hero của delivery — dữ
// liệu template tấm vé sau khi nhận (ảnh 2 của app ZCode).
type ZcodePlanHeroArgs struct {
	Name         string
	EndsAt       int64 // epoch giây (app gốc: new Date(ends_at * 1000))
	Entitlements []ClaimEntitlement
}

// PlanQuota là dữ liệu hiển thị số token lớn, dạng như banner
// "300,000,000 tokens" của app.
type PlanQuota struct {
	AmountRaw   int64  // 300000000 (đã nhân đơn vị M/K nếu có)
	AmountWord  string // "Tokens"
	AmountShort string // "300M" (viết tắt cho dòng benefit)
	Model       string // "GLM-5.3-Flash" — show_name của entitlement đầu
}

// QuotaFromEntitlements tính quota từ entitlements (ưu tiên các
// entitlement meter == "model_usage" như app gốc). Trả nil nếu không
// có entitlement đọc được số.
func QuotaFromEntitlements(ents []ClaimEntitlement) *PlanQuota {
	var best *ClaimEntitlement
	for i := range ents {
		if ents[i].Meter != "model_usage" {
			continue
		}
		if best == nil || ents[i].GrantUnits > best.GrantUnits {
			best = &ents[i]
		}
	}
	if best == nil {
		// Không khai báo meter vẫn thử entitlement đầu tiên có số.
		for i := range ents {
			if ents[i].GrantUnits > 0 {
				best = &ents[i]
				break
			}
		}
	}
	if best == nil || best.GrantUnits <= 0 {
		return nil
	}
	raw := int64(best.GrantUnits)
	word := best.UnitType
	short := fmt.Sprintf("%d", raw)
	switch best.UnitType {
	case "M": // 300 M = 300 triệu
		raw *= 1_000_000
		short = fmt.Sprintf("%dM", int64(best.GrantUnits))
		word = "Tokens"
	case "K":
		raw *= 1_000
		short = fmt.Sprintf("%dK", int64(best.GrantUnits))
		word = "Tokens"
	}
	if word == "" {
		word = "Tokens"
	}
	return &PlanQuota{
		AmountRaw:   raw,
		AmountWord:  word,
		AmountShort: short,
		Model:       best.ShowName,
	}
}

// FormatAmount render số token lớn kiểu app: "300,000,000 Tokens".
func (q PlanQuota) FormatAmount() string {
	return formatComma(q.AmountRaw) + " " + q.AmountWord
}

// FormatBenefit render dòng quyền lợi gọn kiểu vé: "GLM-5.3-Flash ·
// 300M Tokens".
func (q PlanQuota) FormatBenefit() string {
	model := q.Model
	if model == "" {
		model = "ZCode"
	}
	return model + " · " + q.AmountShort + " " + q.AmountWord
}

// formatComma thêm dấu phẩy ngăn cách nghìn: 300000000 → "300,000,000"
// (khớp Intl.NumberFormat("en-US") của app gốc).
func formatComma(n int64) string {
	digits := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(digits)+len(digits)/3)
	for i, c := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
