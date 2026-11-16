// marketing.go — các API nhận deal (claim) port từ source ZCode gốc:
//
//	POST /api/v1/marketing/touch/action          (marketingTouchService.ts)
//	GET  /api/v1/zcode-plan/billing/preview      (bigmodelCodingPlanSubscriptionProvider.ts)
//	POST /api/v1/zcode-plan/billing/claim        (bigmodelCodingPlanSubscriptionProvider.ts)
//
// Hợp đồng khớp app gốc: claim gửi {plan_id} với header Authorization +
// X-ZCode-App-Version + X-Platform (process.platform-arch, ví dụ
// win32-x64); report gửi {campaign_id, action_type: confirm|cancel} với
// cùng header như GET touch. Bản UI open-source của ZCode không gửi thêm
// header xác thực request nào khác (proof rỗng), nên tool cũng vậy.
//
// Token chỉ nằm trong RAM và đi làm header; không bao giờ in ra, ghi
// log hay ghi file. Claim chỉ chạy khi NGƯỜI DÙNG bấm nút (popup hoặc
// lệnh claim) — không có auto-claim.
package checker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

// DefaultMarketingBase là origin gốc của ZCode API. Set
// ZCODE_MARKETING_URL để đổi origin này khi test với server mẫu cục bộ
// (áp dụng cho TẤT CẢ endpoint marketing/billing, không chỉ touch).
const DefaultMarketingBase = "https://zcode.z.ai"

func marketingBase() string {
	if override := strings.TrimSpace(os.Getenv("ZCODE_MARKETING_URL")); override != "" {
		return strings.TrimRight(override, "/")
	}
	return DefaultMarketingBase
}

func marketingEndpoint(path string) string {
	return marketingBase() + path
}

const apiTimeout = 15 * time.Second

var apiHTTPClient = &http.Client{Timeout: apiTimeout}

// clientPlatformKey dựng chuỗi X-Platform giống app gốc:
// process.platform + "-" + process.arch, ví dụ "win32-x64".
// Go gọi GOOS "windows" còn Node gọi "win32"; arch "amd64" của Go
// tương ứng "x64" của Node.
func clientPlatformKey() string {
	platform := nodePlatform(runtime.GOOS)
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "ia32"
	}
	return platform + "-" + arch
}

// authHeaders dựng header đầy đủ cho mọi request tới zcode endpoint:
// bộ source headers 11 mục mà ApiClient của app gốc inject (thiếu bộ này
// là gateway drop connection → EOF, hoặc security check 3007 với
// billing) + Authorization khi có token.
func authHeaders(token, deviceMid, locale, appVersion string) map[string]string {
	headers := buildZCodeSourceHeaders(SourceHeaderOptions{
		AppVersion:     appVersion,
		DeviceMid:      deviceMid,
		ClientLanguage: locale,
	})
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return headers
}

// readResponseBody đọc toàn bộ response; body rỗng là lỗi rõ ràng
// (EOF khi decode) — gateway/WAF thỉnh thoảng chặn billing endpoint.
func readResponseBody(resp *http.Response, what string) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 160 {
			snippet = snippet[:160]
		}
		return nil, fmt.Errorf("HTTP %d từ %s (Content-Type: %s) %s", resp.StatusCode, resp.Header.Get("Content-Type"), what, snippet)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("phản hồi rỗng (0 bytes) từ %s — gateway/WAF có thể đã chặn, thử lại sau", what)
	}
	return body, nil
}

// ReportTouchAction báo hành động của người dùng về server, giống
// service.report() của app: POST /api/v1/marketing/touch/action với
// body {"campaign_id": ..., "action_type": "confirm"|"cancel"}.
// App gốc coi report là fire-and-forget (lỗi chỉ log), tool cũng vậy.
// actionType phải là "confirm" hoặc "cancel" — các giá trị khác trả lỗi.
func ReportTouchAction(token, deviceMid, locale, appVersion, campaignID, actionType string) error {
	if actionType != "confirm" && actionType != "cancel" {
		return fmt.Errorf("action_type không hợp lệ: %q (chỉ confirm|cancel)", actionType)
	}
	if strings.TrimSpace(campaignID) == "" {
		return fmt.Errorf("campaign_id rỗng")
	}
	body, _ := json.Marshal(map[string]string{
		"campaign_id": campaignID,
		"action_type": actionType,
	})
	req, err := http.NewRequest(http.MethodPost, marketingEndpoint("/api/v1/marketing/touch/action"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	for key, value := range authHeaders(token, deviceMid, locale, appVersion) {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d từ marketing/touch/action", resp.StatusCode)
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Code != 0 {
		return fmt.Errorf("marketing/touch/action code != 0 (got %d)", envelope.Code)
	}
	return nil
}

// ClaimPlanPreview là 1 plan nhận được từ endpoint preview.
type ClaimPlanPreview struct {
	PlanID       string
	Name         string
	Description  string
	Priority     float64
	Entitlements []ClaimEntitlement
}

// ClaimEntitlement là 1 quyền lợi trong plan (token, quota...).
type ClaimEntitlement struct {
	EntitlementID string
	ShowName      string
	Meter         string
	UnitType      string
	Period        string
	GrantUnits    float64
}

// PreviewClaimPlans gọi GET /api/v1/zcode-plan/billing/preview để liệt
// kê plan có thể nhận (dùng cho `zcode-deal claim --list`). Cần token.
func PreviewClaimPlans(token, appVersion string) ([]ClaimPlanPreview, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("cần token đăng nhập để xem plan nhận được")
	}
	u := marketingEndpoint("/api/v1/zcode-plan/billing/preview") +
		"?app_version=" + url.QueryEscape(appVersion) +
		"&platform=" + url.QueryEscape(clientPlatformKey())
	headers := map[string]string{
		"Authorization":       "Bearer " + token,
		"X-ZCode-App-Version": appVersion,
		"X-Platform":          clientPlatformKey(),
	}
	// GET idempotent → retry 3 lần với backoff khi gateway drop response
	// (EOF/rỗng) — bug thật gặp trên billing endpoint 2026-11-16.
	var body []byte
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 700 * time.Millisecond)
		}
		body, lastErr = httpGetJSON(u, headers)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	var payload struct {
		Code any    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Plans []struct {
				PlanID       string  `json:"plan_id"`
				Name         string  `json:"name"`
				Description  string  `json:"description"`
				Priority     float64 `json:"priority"`
				Entitlements []struct {
					EntitlementID string  `json:"entitlement_id"`
					ShowName      string  `json:"show_name"`
					Meter         string  `json:"meter"`
					UnitType      string  `json:"unit_type"`
					Period        string  `json:"period"`
					GrantUnits    float64 `json:"grant_units"`
				} `json:"entitlements"`
			} `json:"plans"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	// App gốc: code là số, nhưng chấp nhận thêm chuỗi số ("0") — billing
	// endpoint dùng envelope nền tảng (code=0 là thành công).
	if code := numberCode(payload.Code); code != 0 {
		msg := strings.TrimSpace(payload.Msg)
		if msg == "" {
			msg = fmt.Sprintf("billing/preview code != 0 (got %v)", payload.Code)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var plans []ClaimPlanPreview
	for _, raw := range payload.Data.Plans {
		plan := ClaimPlanPreview{
			PlanID:      strings.TrimSpace(raw.PlanID),
			Name:        strings.TrimSpace(raw.Name),
			Description: strings.TrimSpace(raw.Description),
			Priority:    raw.Priority,
		}
		if plan.PlanID == "" {
			continue
		}
		for _, ent := range raw.Entitlements {
			id := strings.TrimSpace(ent.EntitlementID)
			if id == "" {
				continue
			}
			plan.Entitlements = append(plan.Entitlements, ClaimEntitlement{
				EntitlementID: id,
				ShowName:      strings.TrimSpace(ent.ShowName),
				Meter:         strings.TrimSpace(ent.Meter),
				UnitType:      strings.TrimSpace(ent.UnitType),
				Period:        strings.TrimSpace(ent.Period),
				GrantUnits:    ent.GrantUnits,
			})
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// ClaimResult là kết quả 1 lần claim plan. EndsAt là hạn dùng của
// plan (epoch GIÂY — app gốc convert `ends_at * 1000` sang ms khi hiển
// thị "Valid until").
type ClaimResult struct {
	Success bool
	Code    int
	Message string
	PlanID  string
	EndsAt  int64
	Plan    ClaimPlanPreview
	// Uncertain: request đã gửi nhưng mất response (EOF/WAF) — không biết
	// server đã nhận hay chưa; KHÔNG được tự retry (double claim).
	Uncertain bool
}

// FindPlanPreview match plan theo planID trong danh sách preview để
// popup hiển thị entitlement thật của deal (giống app gốc hiện chi
// tiết plan trong luồng nhận). Trả nil nếu không thấy.
func FindPlanPreview(plans []ClaimPlanPreview, planID string) *ClaimPlanPreview {
	for i := range plans {
		if plans[i].PlanID == planID {
			return &plans[i]
		}
	}
	return nil
}

// httpGetJSON thực GET với headers cho trước, trả body đã đọc; status
// != 200 hay body rỗng đều là lỗi (retryable cho caller quyết định).
func httpGetJSON(url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return readResponseBody(resp, url)
}

// ClaimPlan gọi POST /api/v1/zcode-plan/billing/claim với body
// {"plan_id": ...} — đúng endpoint và header app gốc dùng khi người
// dùng bấm nút nhận deal. Response {code:0, data:{plan}} là thành công;
// code khác 0 là lỗi (data.message là câu thông điệp đã dịch sẵn của
// server; 1001-1005 là lỗi dứt điểm không nên thử lại).
func ClaimPlan(token, planID, appVersion string) (ClaimResult, error) {
	if strings.TrimSpace(token) == "" {
		return ClaimResult{Code: 401, Message: "chưa có token đăng nhập"}, fmt.Errorf("chưa có token đăng nhập")
	}
	if strings.TrimSpace(planID) == "" {
		return ClaimResult{}, fmt.Errorf("plan_id rỗng")
	}
	body, _ := json.Marshal(map[string]string{"plan_id": planID})
	req, err := http.NewRequest(http.MethodPost, marketingEndpoint("/api/v1/zcode-plan/billing/claim"), bytes.NewReader(body))
	if err != nil {
		return ClaimResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ZCode-App-Version", appVersion)
	req.Header.Set("X-Platform", clientPlatformKey())
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return ClaimResult{}, err
	}
	defer resp.Body.Close()
	respBody, err := readResponseBody(resp, "/api/v1/zcode-plan/billing/claim")
	if err != nil {
		// POST KHÔNG idempotent — không tự retry (double claim nguy hiểm);
		// đánh dấu uncertain: yêu cầu CÓ THỂ đã vào server nhưng mất response.
		return ClaimResult{Code: -1, Uncertain: true, Message: "phản hồi bị mất (EOF/WAF)"}, err
	}
	var payload struct {
		Code any `json:"code"`
		Data struct {
			Message string `json:"message"`
			Plan    struct {
				PlanID       string  `json:"plan_id"`
				Name         string  `json:"name"`
				Description  string  `json:"description"`
				Priority     float64 `json:"priority"`
				EndsAt       float64 `json:"ends_at"`
				Entitlements []struct {
					EntitlementID string  `json:"entitlement_id"`
					ShowName      string  `json:"show_name"`
					Meter         string  `json:"meter"`
					UnitType      string  `json:"unit_type"`
					Period        string  `json:"period"`
					GrantUnits    float64 `json:"grant_units"`
				} `json:"entitlements"`
			} `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return ClaimResult{}, err
	}
	code := numberCode(payload.Code)
	result := ClaimResult{
		Code:    code,
		Message: strings.TrimSpace(payload.Data.Message),
		PlanID:  planID,
	}
	if code != 0 || strings.TrimSpace(payload.Data.Plan.PlanID) == "" {
		result.Success = false
		return result, nil
	}
	result.Success = true
	result.Plan = ClaimPlanPreview{
		PlanID:      strings.TrimSpace(payload.Data.Plan.PlanID),
		Name:        strings.TrimSpace(payload.Data.Plan.Name),
		Description: strings.TrimSpace(payload.Data.Plan.Description),
		Priority:    payload.Data.Plan.Priority,
	}
	if payload.Data.Plan.EndsAt >= 0 {
		result.EndsAt = int64(payload.Data.Plan.EndsAt)
	}
	for _, ent := range payload.Data.Plan.Entitlements {
		id := strings.TrimSpace(ent.EntitlementID)
		if id == "" {
			continue
		}
		result.Plan.Entitlements = append(result.Plan.Entitlements, ClaimEntitlement{
			EntitlementID: id,
			ShowName:      strings.TrimSpace(ent.ShowName),
			Meter:         strings.TrimSpace(ent.Meter),
			UnitType:      strings.TrimSpace(ent.UnitType),
			Period:        strings.TrimSpace(ent.Period),
			GrantUnits:    ent.GrantUnits,
		})
	}
	return result, nil
}

// numberCode ép code về số: app gốc gặp cả code dạng số và chuỗi số
// ("0"); code không xác định thì trả -1 (không bao giờ coi là thành công).
func numberCode(raw any) int {
	switch value := raw.(type) {
	case float64:
		return int(value)
	case string:
		if isAllDigits(value) {
			return atoiSafe(value)
		}
	}
	return -1
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
