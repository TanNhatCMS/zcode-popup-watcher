package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

const sampleSnapshot = `{"code":0,"data":{"server_time":1793000000,"language":"en-US","deliveries":[
	{"campaign_id":"sample-night-deal","priority":80,"resource_position":"banner","banner":{"buttons":[{"text":{"format":"plaintext","content":"Nhan ngay"}}],"success_popup":{"title":{"format":"plaintext","content":"Nhan thanh cong"}}}},
	{"campaign_id":"sample-popup","priority":90,"resource_position":"popup","popup":{"title":{"format":"plaintext","content":"Deal dem"},"description":{"format":"plaintext","content":"300M token GLM-5.3-Flash"},"buttons":[{"text":{"format":"plaintext","content":"Nhan"}}]}},
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
		fmt.Printf("[checker] self-test LOI: %v\n", err)
		return 1
	}
	fmt.Printf("[checker] self-test OK: %d delivery (banner 1 + popup 1, ban trung vi tri da bi loai):\n", len(deals))
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
		files = "(khong thay file nao)"
	}
	fmt.Printf("[checker] files trong v2: %s\n", files)
	mid := "(khong co)"
	if summary.HasDeviceMid && len(summary.DeviceMid) > 8 {
		mid = summary.DeviceMid[:8] + "..."
	}
	fmt.Printf("[checker] deviceMid: %s\n", mid)
	keys := strings.Join(summary.CredentialKeys, ", ")
	if keys == "" {
		keys = "(khong co)"
	}
	fmt.Printf("[checker] credential keys: %s\n", keys)
	hasJwt := "khong"
	if summary.HasJWT {
		hasJwt = "co"
	}
	fmt.Printf("[checker] co zcodejwttoken: %s\n", hasJwt)
	fmt.Printf("[checker] API: %s, locale: %s, app-version: %s, interval: %ds\n", checker.APIURL, opts.locale, opts.appVersion, int(opts.interval.Seconds()))
	fmt.Println("[checker] Che do: chi GET marketing/touch, khong POST action, khong claim, khong in token.")
	return 0
}

// resolveAuth tra ve deviceMid + token (env ghi de > credentials.json),
// giong thu tu ban Bun. Loi giai ma tra ve error de fail-closed.
func resolveAuth(configDir string) (string, string, error) {
	deviceMid, ok := checker.ReadDeviceMid(configDir)
	if !ok {
		return "", "", fmt.Errorf("khong tim thay deviceMid trong telemetry-state.json")
	}
	token := strings.TrimSpace(os.Getenv("ZCODE_JWT_TOKEN"))
	if token == "" {
		jwt, ok := checker.ReadZcodeJWT(configDir)
		if !ok {
			// Khong co key hoac giai ma loi: phan biet bang cach thu
			// doc lai - neu key ton tai ma giai ma loi thi bao loi.
			if hasKey(configDir) {
				return "", "", fmt.Errorf("khong giai ma duoc zcodejwttoken (sai secret hoac khac user/may voi ZCode)")
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
		fmt.Println("[checker] Canh bao: interval duoi 300s de bi 429. Tool van chay theo so ban dat.")
	}
	configDir := checker.ConfigDir(opts.configDir)
	deviceMid, token, err := resolveAuth(configDir)
	if err != nil {
		fmt.Printf("[checker] LOI: %v\n", err)
		if strings.Contains(err.Error(), "deviceMid") {
			fmt.Println("[checker] Mo ZCode 1 lan de no tao file, hoac kiem tra lai ZCODE_DATA_BASE_DIR.")
		} else {
			fmt.Println("[checker] Thuong do chay khac user/may voi ZCode, hoac ZCODE_CREDENTIAL_SECRET khac voi luc ZCode ma hoa.")
		}
		return 2
	}
	if token == "" {
		fmt.Println("[checker] Luu y: khong co token, dang check an danh. Deal gan tai khoan co the khong hien.")
	} else {
		fmt.Println("[checker] Da co token tu config (khong hien token ra day).")
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
			fmt.Printf("[checker] Check loi (%v). Doi %s roi thu lai.\n", err, delay)
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
			keys = "(rong)"
		}
		fmt.Printf("[checker] Co %d delivery, %d moi: %s\n", len(deals), len(fresh), keys)
		if len(fresh) > 0 {
			for _, deal := range fresh {
				seen[deal.Key] = true
			}
			checker.SaveSeen(seen)
			Notify("ZCode - Co deal moi!", formatDealsMessage(fresh))
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
