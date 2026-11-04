package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

const sampleSnapshot = `{"code":0,"data":{"server_time":1793000000,"language":"en-US","deliveries":[
	{"campaign_id":"sample-night-deal","priority":80,"resource_position":"banner","banner":{"buttons":[{"text":{"format":"plaintext","content":"Nhận ngay"}}],"success_popup":{"title":{"format":"plaintext","content":"Nhận thành công"}}}},
	{"campaign_id":"sample-popup","priority":90,"resource_position":"popup","popup":{"title":{"format":"plaintext","content":"Deal đêm"},"description":{"format":"plaintext","content":"300M token GLM-5.3-Flash"},"buttons":[{"text":{"format":"plaintext","content":"Nhận"}}]}},
	{"campaign_id":"sample-banner-duplicate","priority":10,"resource_position":"banner","banner":{"buttons":[]}}
]}}`

type checkOptions struct {
	configDir  string
	interval   time.Duration
	locale     string
	appVersion string
	autoOpen   bool
	once       bool
	dryRun     bool
	selfTest   bool
}

func defaultCheckOptions(args map[string]string, bools map[string]bool) checkOptions {
	intervalSec := 600
	if v, ok := args["interval"]; ok {
		if n, err := parseInt(v); err == nil && n > 0 {
			intervalSec = n
		}
	}
	locale := os.Getenv("ZCODE_LOCALE")
	if locale == "" {
		locale = "en-US"
	}
	appVersion := os.Getenv("ZCODE_APP_VERSION")
	if appVersion == "" {
		appVersion = "3.17.2"
	}
	return checkOptions{
		configDir:  args["config-dir"],
		interval:   time.Duration(intervalSec) * time.Second,
		locale:     locale,
		appVersion: appVersion,
		autoOpen:   bools["open"],
		once:       bools["once"],
		dryRun:     bools["dry-run"],
		selfTest:   bools["self-test"],
	}
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func runSelfTest() int {
	deals, err := checker.ParseSnapshot([]byte(sampleSnapshot))
	if err != nil {
		fmt.Printf("[checker] self-test LỖI: %v\n", err)
		return 1
	}
	fmt.Printf("[checker] self-test OK: %d delivery (banner 1 + popup 1, bản trùng vị trí đã bị loại):\n", len(deals))
	for _, deal := range deals {
		fmt.Printf("  - %s | %s | %s\n", deal.Key, deal.Title, deal.Detail)
	}
	return 0
}

func runDryRun(opts checkOptions) int {
	summary := checker.DescribeConfig(checker.ConfigDir(opts.configDir))
	fmt.Printf("[checker] config dir: %s\n", summary.ConfigDir)
	files := strings.Join(summary.Files, ", ")
	if files == "" {
		files = "(không thấy file nào)"
	}
	fmt.Printf("[checker] files trong v2: %s\n", files)
	mid := "(không có)"
	if summary.HasDeviceMid && len(summary.DeviceMid) > 8 {
		mid = summary.DeviceMid[:8] + "..."
	}
	fmt.Printf("[checker] deviceMid: %s\n", mid)
	keys := strings.Join(summary.CredentialKeys, ", ")
	if keys == "" {
		keys = "(không có)"
	}
	fmt.Printf("[checker] credential keys: %s\n", keys)
	hasJwt := "không"
	if summary.HasJWT {
		hasJwt = "có"
	}
	fmt.Printf("[checker] có zcodejwttoken: %s\n", hasJwt)
	fmt.Printf("[checker] API: %s, locale: %s, app-version: %s, interval: %ds\n", checker.APIURL, opts.locale, opts.appVersion, int(opts.interval.Seconds()))
	fmt.Println("[checker] Chế độ: chỉ GET marketing/touch, không POST action, không claim, không in token.")
	return 0
}

// resolveAuth trả về deviceMid + token (env ghi đè > credentials.json),
// giống thứ tự bản Bun. Lỗi giải mã trả về error để fail-closed.
func resolveAuth(configDir string) (string, string, error) {
	deviceMid, ok := checker.ReadDeviceMid(configDir)
	if !ok {
		return "", "", fmt.Errorf("không tìm thấy deviceMid trong telemetry-state.json")
	}
	token := strings.TrimSpace(os.Getenv("ZCODE_JWT_TOKEN"))
	if token == "" {
		jwt, ok := checker.ReadZcodeJWT(configDir)
		if !ok {
			// Không có key hoặc giải mã lỗi: phân biệt bằng cách thử
			// đọc lại - nếu key tồn tại mà giải mã lỗi thì báo lỗi.
			if hasKey(configDir) {
				return "", "", fmt.Errorf("không giải mã được zcodejwttoken (sai secret hoặc khác user/máy với ZCode)")
			}
			token = ""
		} else {
			token = jwt
		}
	}
	return deviceMid, token, nil
}

func hasKey(configDir string) bool {
	for _, key := range checker.ReadCredentialKeys(configDir) {
		if key == "zcodejwttoken" {
			return true
		}
	}
	return false
}

func runCheck(args map[string]string, bools map[string]bool) int {
	opts := defaultCheckOptions(args, bools)
	if opts.selfTest {
		return runSelfTest()
	}
	if opts.dryRun {
		return runDryRun(opts)
	}
	if opts.interval < 300*time.Second {
		fmt.Println("[checker] Cảnh báo: interval dưới 300s dễ bị 429. Tool vẫn chạy theo số bạn đặt.")
	}
	configDir := checker.ConfigDir(opts.configDir)
	deviceMid, token, err := resolveAuth(configDir)
	if err != nil {
		fmt.Printf("[checker] LỖI: %v\n", err)
		if strings.Contains(err.Error(), "deviceMid") {
			fmt.Println("[checker] Mở ZCode 1 lần để nó tạo file, hoặc kiểm tra lại ZCODE_DATA_BASE_DIR.")
		} else {
			fmt.Println("[checker] Thường do chạy khác user/máy với ZCode, hoặc ZCODE_CREDENTIAL_SECRET khác với lúc ZCode mã hóa.")
		}
		return 2
	}
	if token == "" {
		fmt.Println("[checker] Lưu ý: không có token, đang check ẩn danh. Deal gắn tài khoản có thể không hiện.")
	} else {
		fmt.Println("[checker] Đã có token từ config (không hiện token ra đây).")
	}
	seq := 0
	failures := 0
	seen := checker.LoadSeen()
	for {
		deals, err := checker.QueryDeals(token, deviceMid, opts.locale, opts.appVersion, seq)
		if err != nil {
			failures++
			delay := 30 * time.Second * time.Duration(1<<minInt(failures-1, 5))
			if delay > 10*time.Minute {
				delay = 10 * time.Minute
			}
			fmt.Printf("[checker] Check lỗi (%v). Đợi %s rồi thử lại.\n", err, delay)
			if opts.once {
				return 1
			}
			time.Sleep(delay)
			continue
		}
		seq++
		failures = 0
		var fresh []checker.Deal
		for _, deal := range deals {
			if !seen[deal.Key] {
				fresh = append(fresh, deal)
			}
		}
		keys := strings.Join(freshDealKeys(deals), ", ")
		if keys == "" {
			keys = "(rỗng)"
		}
		fmt.Printf("[checker] Có %d delivery, %d mới: %s\n", len(deals), len(fresh), keys)
		if len(fresh) > 0 {
			for _, deal := range fresh {
				seen[deal.Key] = true
			}
			checker.SaveSeen(seen)
			Notify("ZCode - Có deal mới!", formatDealsMessage(fresh))
			if opts.autoOpen {
				fmt.Println(OpenZcode())
			}
		}
		if opts.once {
			return 0
		}
		time.Sleep(opts.interval)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
