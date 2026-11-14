// update.go — kiểm tra + tải bản cập nhật từ GitHub Releases của
// TanNhatCMS/zcode-popup-watcher. Chỉ đọc API công khai; việc thay file
// exe và khởi động lại do main xử lý SAU KHI người dùng xác nhận
// (tải có xác nhận, không tự update ngầm).
package checker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// UpdateRepo là repo GitHub phát hành bản cập nhật (export cho hiển
// thị link tải thủ công trong UI).
const UpdateRepo = "TanNhatCMS/zcode-popup-watcher"
const updateAssetName = "zcode-deal.exe"

// ReleaseUpdate là kết quả 1 lần kiểm tra bản mới.
type ReleaseUpdate struct {
	LatestTag string // "v2.2.0"
	Current   string // "2.1.0"
	HasUpdate bool
	AssetName string
	AssetURL  string
	AssetSize int64
}

// CheckUpdate gọi GitHub Releases latest và so sánh với phiên bản hiện
// tại. Không có release nào / API lỗi → trả lỗi (caller hiển thị).
func CheckUpdate(currentVersion string) (ReleaseUpdate, error) {
	req, err := http.NewRequest(http.MethodGet,
		"https://api.github.com/repos/"+UpdateRepo+"/releases/latest", nil)
	if err != nil {
		return ReleaseUpdate{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "zcode-deal")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseUpdate{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ReleaseUpdate{}, fmt.Errorf("GitHub API trả HTTP %d (chưa có release?)", resp.StatusCode)
	}
	var payload struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ReleaseUpdate{}, err
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return ReleaseUpdate{}, fmt.Errorf("release không có tag_name")
	}
	result := ReleaseUpdate{
		LatestTag: strings.TrimSpace(payload.TagName),
		Current:   currentVersion,
	}
	for _, asset := range payload.Assets {
		if asset.Name == updateAssetName {
			result.AssetName = asset.Name
			result.AssetURL = asset.BrowserDownloadURL
			result.AssetSize = asset.Size
			break
		}
	}
	result.HasUpdate = CompareVersions(result.LatestTag, currentVersion) > 0
	return result, nil
}

// CompareVersions so sánh "v2.2.0" với "2.1.0": trả 1 nếu a mới hơn,
// 0 nếu bằng, -1 nếu a cũ hơn. Bỏ tiền tố v; bỏ hậu tố không số
// (ví dụ "-beta") ở từng phần.
func CompareVersions(a, b string) int {
	pa, pb := parseVersion(a), parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] > pb[i] {
			return 1
		}
		if pa[i] < pb[i] {
			return -1
		}
	}
	return 0
}

// parseVersion "v2.1.0-beta" → [2, 1, 0].
func parseVersion(s string) [3]int {
	var out [3]int
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V"))
	for i, part := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		digits := ""
		for _, r := range part {
			if r >= '0' && r <= '9' {
				digits += string(r)
				continue
			}
			break
		}
		if digits == "" {
			continue
		}
		n, _ := strconv.Atoi(digits)
		out[i] = n
	}
	return out
}

// DownloadFile tải url vào dest (ghi đè). Caller kiểm tra kích thước/
// hợp lệ trước khi thay exe.
func DownloadFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "zcode-deal")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d khi tải bản cập nhật", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}
	if n < 1024*1024 { // exe của tool ~10MB — nhỏ hơn 1MB chắc chắn hỏng
		return fmt.Errorf("bản tải về quá nhỏ (%d bytes) — file hỏng", n)
	}
	return nil
}
