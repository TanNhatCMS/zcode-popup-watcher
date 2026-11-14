// account.go — thông tin tài khoản ZCode đang đăng nhập, lấy từ
// OIDC userinfo endpoint mà app gốc dùng (zaiProviderAdapter.ts:
// GET {userinfo} + Authorization: Bearer <zcodejwttoken>, response
// user ở root hoặc trong data; tên hiển thị resolve theo thứ tự
// name → preferred_username → email → sub/id).
//
// App gốc cấu hình 2 provider cho zcode JWT: Z.ai global
// (chat.z.ai/api/oauth/userinfo) và Z.ai CN (zcode.z.ai/api/v1/oauth/
// profile) — tool thử lần lượt theo thứ tự đó. Đặt ZCODE_ACCOUNT_URL
// để ghi đè khi test với server mẫu cục bộ.
//
// Chỉ lấy vài field hiển thị an toàn; token không bao giờ in ra hay
// ghi file, picture không dùng (popup native không tải ảnh người).
package checker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// AccountInfo là thông tin tài khoản đang đăng nhập ZCode.
type AccountInfo struct {
	ID       string
	Username string
	Email    string
	Picture  string
	Source   string // endpoint trả về thông tin
}

func accountEndpoints() []string {
	if override := strings.TrimSpace(os.Getenv("ZCODE_ACCOUNT_URL")); override != "" {
		return []string{strings.TrimRight(override, "/")}
	}
	return []string{
		"https://chat.z.ai/api/oauth/userinfo",
		DefaultMarketingBase + "/api/v1/oauth/profile",
	}
}

// FetchAccountInfo gọi userinfo với token hiện tại; thử lần lượt các
// endpoint, endpoint đầu tiên trả về thông tin hợp lệ sẽ thắng.
func FetchAccountInfo(token string) (AccountInfo, error) {
	if strings.TrimSpace(token) == "" {
		return AccountInfo{}, fmt.Errorf("chưa có token đăng nhập")
	}
	var lastErr error
	for _, endpoint := range accountEndpoints() {
		info, err := fetchAccountFrom(endpoint, token)
		if err != nil {
			lastErr = err
			continue
		}
		return info, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("không endpoint userinfo nào hợp lệ")
	}
	return AccountInfo{}, lastErr
}

func fetchAccountFrom(endpoint, token string) (AccountInfo, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return AccountInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return AccountInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AccountInfo{}, fmt.Errorf("HTTP %d từ %s", resp.StatusCode, endpoint)
	}
	var root map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return AccountInfo{}, err
	}
	info, err := parseAccountUser(root)
	if err != nil {
		return AccountInfo{}, err
	}
	info.Source = endpoint
	return info, nil
}

// parseAccountUser trích user từ payload: app gốc chấp nhận user ở
// root hoặc trong data ("const user = userinfoPayload.data ?? user").
func parseAccountUser(root map[string]any) (AccountInfo, error) {
	user := root
	if data, ok := root["data"].(map[string]any); ok {
		user = data
	}
	str := func(key string) string {
		v, _ := user[key].(string)
		return strings.TrimSpace(v)
	}
	info := AccountInfo{
		ID:       firstNonEmpty(str("sub"), str("user_id"), str("id")),
		Username: firstNonEmpty(str("name"), str("preferred_username"), str("email"), str("sub"), str("id")),
		Email:    str("email"),
		Picture:  str("picture"),
	}
	if info.ID == "" && info.Username == "" && info.Email == "" {
		return info, fmt.Errorf("phản hồi không có thông tin user")
	}
	return info, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
