package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

const sampleSnapshot = `{"code":0,"data":{"server_time":1793000000,"language":"en-US","deliveries":[
	{"campaign_id":"sample-night-deal","priority":80,"resource_position":"banner","banner":{"buttons":[{"text":{"format":"plaintext","content":"Nhận ngay"},"action":{"type":"claim_zcode_plan","args":{"plan_id":"plan-sample-banner"}}}],"success_popup":{"title":{"format":"plaintext","content":"Nhận thành công"}}}},
	{"campaign_id":"sample-popup","priority":90,"resource_position":"popup","popup":{"title":{"format":"plaintext","content":"Deal đêm"},"description":{"format":"plaintext","content":"300M token GLM-5.3-Flash"},"buttons":[{"text":{"format":"plaintext","content":"Nhận"},"action":{"type":"claim_zcode_plan","args":{"plan_id":"plan-sample-123"}}},{"text":{"format":"plaintext","content":"Đóng"},"action":{"type":"close"}}]}},
	{"campaign_id":"sample-banner-duplicate","priority":10,"resource_position":"banner","banner":{"buttons":[]}}
]}}`

type checkOptions struct {
	configDir  string
	interval   time.Duration
	locale     string
	appVersion string
	autoOpen   bool
	noPopup    bool
	noWindow   bool   // --no-window: bỏ qua khung giờ check
	account    string // --account: override tài khoản check (flag > settings)
	from       string
	to         string
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
	// Nền là settings lưu bền (menu tray chỉnh được); flag CLI đè lên.
	settings := checker.LoadDealSettings()
	account := settings.CheckAccount
	if v := strings.TrimSpace(args["account"]); v != "" {
		account = v
	}
	from, to := settings.CheckFrom, settings.CheckTo
	if v := strings.TrimSpace(args["from"]); v != "" {
		from = v
	}
	if v := strings.TrimSpace(args["to"]); v != "" {
		to = v
	}
	return checkOptions{
		configDir:  args["config-dir"],
		interval:   time.Duration(intervalSec) * time.Second,
		locale:     localeOrDefault(),
		appVersion: appVersionOrDefault(),
		autoOpen:   bools["open"],
		noPopup:    bools["no-popup"],
		noWindow:   bools["no-window"],
		account:    account,
		from:       from,
		to:         to,
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
		fmt.Printf("  - %s | %s | %s", deal.Key, deal.Title, deal.Detail)
		if deal.PlanID != "" {
			fmt.Printf(" | plan nhận được: %s (nút %q)", deal.PlanID, deal.ButtonLabel)
		}
		fmt.Println()
	}
	return 0
}

func runDryRun(opts checkOptions) int {
	summary := checker.DescribeConfig(checker.ConfigDir(opts.configDir))
	settings := checker.LoadDealSettings()
	_ = settings
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
	fmt.Printf("[checker] API: %s, locale: %s, app-version: %s, interval: %ds\n", checker.DefaultMarketingBase, opts.locale, opts.appVersion, int(opts.interval.Seconds()))
	fmt.Printf("[checker] Check account: %s\n", opts.account)
	fmt.Printf("[checker] Khung giờ check: %s-%s giờ VN (window=%v, --no-window=%v)\n", opts.from, opts.to, settings.CheckWindowEnabled, opts.noWindow)
	fmt.Println("[checker] Chế độ: GET marketing/touch để báo deal. Claim chỉ chạy khi bạn bấm")
	fmt.Println("[checker] \"Nhận deal\" trên popup hoặc gõ y ở lệnh claim — không auto-claim, không report gì khi chỉ xem.")
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
	// Tài khoản gọi API check: --account (CLI) > settings.check_account
	// (menu tray chỉnh) > mặc định là tài khoản đang login ZCode.
	checkToken, checkLabel := checkTokenFor(opts.account, token)
	if checkLabel != "" {
		fmt.Printf("[checker] Check deal bằng tài khoản: %s\n", checkLabel)
	}
	seq := 0
	failures := 0
	seen := checker.LoadSeen()
	settings := checker.LoadDealSettings() // đọc lại để lấy check_window_enabled (menu tray chỉnh live)
	windowOn := settings.CheckWindowEnabled && !opts.noWindow
	window := checker.DealSettings{CheckFrom: opts.from, CheckTo: opts.to}
	// --once là hành động chủ động của người dùng → bỏ qua khung giờ.
	checkNowPending := opts.once
	outsideLogged := false
	wait := time.Duration(0)
	for {
		if wait > 0 {
			time.Sleep(wait)
		}
		wait = opts.interval
		if windowOn && !checkNowPending {
			if _, minutes := vnNow(); !window.InCheckWindow(minutes) {
				if !outsideLogged {
					fmt.Printf("[checker] Ngoài khung giờ check (%s-%s giờ VN) — KHÔNG gọi API, đợi 60s mỗi vòng.\n", opts.from, opts.to)
					outsideLogged = true
				}
				wait = 60 * time.Second
				continue
			}
			outsideLogged = false
		}
		checkNowPending = false
		deals, err := checker.QueryDeals(checkToken, deviceMid, opts.locale, opts.appVersion, seq)
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
			wait = delay
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
			if !opts.noPopup {
				// Popup nhận deal (Windows): người dùng bấm "Nhận deal"
				// tool mới claim. --once block chờ chọn xong rồi thoát.
				offerDealClaim(fresh, newClaimContext(checkToken, deviceMid), opts.once)
			} else {
				// Không hiện popup: vẫn lưu deal nhận được để
				// `zcode-deal claim` dùng được từ terminal.
				recordPendingClaim(fresh)
			}
			if opts.autoOpen {
				fmt.Println(OpenZcode())
			}
		}
		if opts.once {
			return 0
		}
	}
}

// checkTokenFor trả token dùng gọi API check deal: --account (CLI) >
// settings.check_account (menu tray chỉnh) > token mặc định đang login
// ZCode. Trả thêm nhãn tài khoản khi dùng tài khoản cockpit.
func checkTokenFor(accountSetting, defaultToken string) (string, string) {
	if accountSetting == "" || strings.EqualFold(accountSetting, "default") {
		return defaultToken, ""
	}
	for _, acc := range checker.ListCockpitZcodeAccounts() {
		if strings.EqualFold(acc.Email, accountSetting) || strings.EqualFold(acc.ID, accountSetting) {
			if acc.JWT == "" {
				break
			}
			return acc.JWT, acc.Email
		}
	}
	logLine(fmt.Sprintf("[checker] Không tìm thấy/JWT rỗng cho tài khoản check %q — dùng tài khoản mặc định.", accountSetting))
	return defaultToken, ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
