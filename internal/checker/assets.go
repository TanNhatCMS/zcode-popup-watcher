// assets.go — download marketing asset (ảnh hero/banner) kèm kiểm tra
// SHA-256, port hành vi asset gate của app gốc: asset phải khớp đúng
// hash server khai báo trong payload, sai thì bỏ (fail-closed), không
// vẽ nội dung lạ.
package checker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	assetTimeout   = 5 * time.Second
	maxAssetBytes  = 10 << 20 // 10MB, đủ cho banner; quá là bất thường
	assetUserAgent = "zcode-deal"
)

// assetHTTPClient tách riêng client của asset (timeout ngắn hơn API,
// không dùng chung timeout 15s của claim/query).
var assetHTTPClient = &http.Client{Timeout: assetTimeout}

// FetchMarketingAsset tải asset từ src và xác minh SHA-256 khớp
// expectedSHA (hex lowercase, app gốc gửi kèm mỗi asset). Trả về bytes
// ảnh hợp lệ hoặc error — caller fallback popup text-only khi lỗi.
func FetchMarketingAsset(src, expectedSHA string) ([]byte, error) {
	if src == "" || expectedSHA == "" {
		return nil, fmt.Errorf("asset thiếu src hoặc sha256")
	}
	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", assetUserAgent)
	resp, err := assetHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d khi tải asset", resp.StatusCode)
	}
	// Đọc tối đa maxAssetBytes+1 để phát hiện payload quá to trước khi
	// phí RAM hash.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAssetBytes {
		return nil, fmt.Errorf("asset quá lớn (> %d bytes)", maxAssetBytes)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != expectedSHA {
		return nil, fmt.Errorf("asset SHA-256 không khớp (got %s, want %s)", got, expectedSHA)
	}
	return data, nil
}
