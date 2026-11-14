// claim.go — logic "nhận deal" dùng chung cho mọi chế độ chạy:
//
//  1. Deal mới có nút nhận (claim_zcode_plan / open_url) được ghi vào
//     pending-claim.json để không mất dù popup bị đóng/tắt máy.
//  2. Windows tray/check: hiện popup, NGƯỜI DÙNG bấm "Nhận deal" mới
//     gọi API claim. Không bấm = không claim, không có auto-claim.
//  3. Sau khi claim thành công, report "confirm" về marketing/touch
//     (giống app gốc report sau khi người dùng bấm nút); bỏ qua deal
//     thì report "cancel".
//  4. CLI fallback (Linux/macOS, hoặc khi popup không hiện được):
//     `zcode-deal claim` nhận deal đang chờ trong pending-claim.json.
//
// Hợp đồng API port từ source ZCode gốc — xem internal/checker/marketing.go.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

const defaultLocale = "en-US"
const defaultAppVersion = "3.17.2"

func localeOrDefault() string {
	if v := strings.TrimSpace(os.Getenv("ZCODE_LOCALE")); v != "" {
		return v
	}
	return defaultLocale
}

func appVersionOrDefault() string {
	if v := strings.TrimSpace(os.Getenv("ZCODE_APP_VERSION")); v != "" {
		return v
	}
	return defaultAppVersion
}

// claimContext gom thông tin xác thực + ngôn ngữ cần cho claim/report.
// Token chỉ nằm trong RAM, không ghi ra đâu cả.
type claimContext struct {
	token      string
	deviceMid  string
	locale     string
	appVersion string
}

func newClaimContext(token, deviceMid string) claimContext {
	return claimContext{
		token:      token,
		deviceMid:  deviceMid,
		locale:     localeOrDefault(),
		appVersion: appVersionOrDefault(),
	}
}

func resolveClaimContext(configDir string) (claimContext, error) {
	deviceMid, token, err := resolveAuth(configDir)
	if err != nil {
		return claimContext{}, err
	}
	return newClaimContext(token, deviceMid), nil
}

// firstClaimable trả deal có thể nhận đầu tiên, hoặc nil.
func firstClaimable(fresh []checker.Deal) *checker.Deal {
	for i := range fresh {
		if fresh[i].Claimable() {
			return &fresh[i]
		}
	}
	return nil
}

// recordPendingClaim lưu deal có thể nhận đầu tiên vào
// pending-claim.json — luôn chạy kể cả khi tắt popup, để `zcode-deal
// claim` vẫn nhận được deal (fallback trên Linux/macOS/CLI).
func recordPendingClaim(fresh []checker.Deal) {
	deal := firstClaimable(fresh)
	if deal == nil {
		return
	}
	checker.SavePendingClaim(*deal)
	logLine(fmt.Sprintf("[claim] Deal có thể nhận: %s (plan %s) - đợi bạn bấm popup hoặc chạy `zcode-deal claim`.",
		deal.Key, deal.PlanID))
}

// offerDealClaim xử lý các deal mới: lưu pending-claim, hiện popup
// (Windows) và claim khi người dùng bấm. wait=true chỉ dùng cho
// `check --once` để tiến trình không thoát trước khi popup đóng.
func offerDealClaim(fresh []checker.Deal, ctx claimContext, wait bool) {
	deal := firstClaimable(fresh)
	if deal == nil {
		return
	}
	recordPendingClaim(fresh)

	handle := func() {
		choice := showDealPopup(popupTitle(*deal), popupDetail(*deal), deal.ButtonLabel)
		switch choice {
		case "claim":
			summary, ok := claimDeal(*deal, ctx)
			if ok {
				alertBox("ZCode Deal - Nhận thành công!", summary, false)
			} else {
				alertBox("ZCode Deal - Chưa nhận được", summary, true)
			}
		case "open":
			logLine(OpenZcode())
		case "later":
			logLine("[claim] Bạn chọn để sau. Nhận bất cứ lúc nào bằng: zcode-deal claim")
			reportActionSafe(ctx, deal.CampaignID, "cancel")
		default:
			// "none"/"busy": không có popup hoặc popup khác đang mở,
			// deal vẫn nằm trong pending-claim.json.
		}
	}
	if wait {
		handle()
		return
	}
	go handle()
}

func popupTitle(deal checker.Deal) string {
	if strings.TrimSpace(deal.Title) != "" {
		return deal.Title
	}
	return "Có deal mới có thể nhận!"
}

func popupDetail(deal checker.Deal) string {
	lines := []string{}
	if detail := strings.TrimSpace(deal.Detail); detail != "" {
		lines = append(lines, detail)
	}
	if deal.PlanID != "" {
		lines = append(lines, fmt.Sprintf("(plan: %s)", deal.PlanID))
	}
	lines = append(lines, "", "Bấm \"Nhận deal\" để nhận ngay. Tool chỉ claim khi bạn bấm.")
	return strings.Join(lines, "\n")
}

// claimDeal thực hiện 1 lần claim theo hành động của người dùng; trả
// về (tóm tắt kết quả, thành công hay không). Caller chịu trách nhiệm
// hiển thị: popup dùng alertBox, CLI in console — claimDeal không tự
// mở MessageBox để không chặn tiến trình chạy không đầu cuối.
func claimDeal(deal checker.Deal, ctx claimContext) (string, bool) {
	if ctx.token == "" {
		logLine("[claim] Không claim được: chưa có token đăng nhập.")
		return "Chưa có token đăng nhập trong ~/.zcode/v2.\nMở ZCode, đăng nhập rồi chạy lại tool.", false
	}
	// Deal dạng link (open_url): mở link thay vì gọi API.
	if deal.PlanID == "" && deal.ClaimURL != "" {
		logLine("[claim] Deal là link nhận: " + deal.ClaimURL)
		if err := openURL(deal.ClaimURL); err != nil {
			logLine(fmt.Sprintf("[claim] Không mở được link: %v", err))
			return fmt.Sprintf("Không mở được link nhận deal: %v", err), false
		}
		reportActionSafe(ctx, deal.CampaignID, "confirm")
		logLine("[claim] Đã mở link nhận deal.")
		return "Đã mở link nhận deal:\n" + deal.ClaimURL, true
	}
	logLine(fmt.Sprintf("[claim] Đang nhận deal %s (plan %s) theo lệnh của bạn...", deal.Key, deal.PlanID))
	result, err := checker.ClaimPlan(ctx.token, deal.PlanID, ctx.appVersion)
	if err != nil {
		logLine(fmt.Sprintf("[claim] LỖI claim: %v", err))
		return fmt.Sprintf("Không nhận được deal: %v", err), false
	}
	if !result.Success {
		message := result.Message
		if message == "" {
			message = fmt.Sprintf("Server từ chối yêu cầu nhận (code %d).", result.Code)
		}
		logLine(fmt.Sprintf("[claim] Server từ chối (code %d): %s", result.Code, message))
		return message, false
	}
	// Claim thành công — giống app gốc: report confirm sau khi nhận.
	checker.ClearPendingClaim()
	reportActionSafe(ctx, deal.CampaignID, "confirm")
	summary := planSummary(result)
	logLine("[claim] Nhận deal THÀNH CÔNG: " + strings.ReplaceAll(summary, "\n", " | "))
	return summary, true
}

func planSummary(result checker.ClaimResult) string {
	name := result.Plan.Name
	if name == "" {
		name = result.PlanID
	}
	var lines []string
	lines = append(lines, name)
	for _, ent := range result.Plan.Entitlements {
		if ent.ShowName == "" {
			continue
		}
		line := "- " + ent.ShowName
		if ent.GrantUnits > 0 {
			line += fmt.Sprintf(" (%s %s %s)", formatUnits(ent.GrantUnits), ent.UnitType, ent.Period)
		}
		lines = append(lines, line)
	}
	if len(lines) == 1 && result.Plan.Description != "" {
		lines = append(lines, result.Plan.Description)
	}
	return strings.Join(lines, "\n")
}

func formatUnits(units float64) string {
	if units == float64(int64(units)) {
		return fmt.Sprintf("%d", int64(units))
	}
	return fmt.Sprintf("%g", units)
}

// reportActionSafe gọi marketing/touch/action; lỗi chỉ log — app gốc
// cũng coi report là fire-and-forget.
func reportActionSafe(ctx claimContext, campaignID, action string) {
	if err := checker.ReportTouchAction(ctx.token, ctx.deviceMid, ctx.locale, ctx.appVersion, campaignID, action); err != nil {
		logLine(fmt.Sprintf("[claim] Report %s cho %s lỗi (bỏ qua): %v", action, campaignID, err))
	}
}

// runClaim là lệnh `zcode-deal claim`: nhận deal đang chờ trong
// pending-claim.json (fallback khi không có popup hoặc muốn claim
// bằng terminal). --list xem các plan nhận được qua billing/preview.
func runClaim(args map[string]string, bools map[string]bool) int {
	configDir := checker.ConfigDir(args["config-dir"])
	ctx, err := resolveClaimContext(configDir)
	if err != nil {
		fmt.Printf("[claim] LỖI auth: %v\n", err)
		fmt.Println("[claim] Thường do chạy khác user/máy với ZCode, hoặc chưa mở ZCode lần nào.")
		return 2
	}
	if bools["list"] {
		plans, err := checker.PreviewClaimPlans(ctx.token, ctx.appVersion)
		if err != nil {
			fmt.Printf("[claim] LỖI preview: %v\n", err)
			return 1
		}
		if len(plans) == 0 {
			fmt.Println("[claim] Hiện không có plan nào nhận được (billing/preview rỗng).")
			return 0
		}
		fmt.Printf("[claim] %d plan có thể nhận:\n", len(plans))
		for _, plan := range plans {
			fmt.Printf("  - %s | %s | %s\n", plan.PlanID, plan.Name, plan.Description)
			for _, ent := range plan.Entitlements {
				fmt.Printf("      · %s (%s %s %s)\n", ent.ShowName, formatUnits(ent.GrantUnits), ent.UnitType, ent.Period)
			}
		}
		return 0
	}
	pending, ok := checker.LoadPendingClaim()
	if !ok {
		fmt.Println("[claim] Không có deal nào đang chờ nhận (pending-claim.json rỗng).")
		fmt.Println("[claim] Đợi tool báo deal mới, hoặc mở ZCode xem trong app.")
		return 1
	}
	fmt.Printf("[claim] Deal đang chờ: [%s] %s\n", pending.Position, pending.Title)
	fmt.Printf("[claim] Chi tiết: %s\n", pending.Detail)
	if pending.PlanID == "" && pending.ClaimURL == "" {
		fmt.Println("[claim] Deal này không có nút nhận tool claim được — mở ZCode để nhận trong app.")
		return 1
	}
	deal := checker.Deal{
		Key:         pending.Position + ":" + pending.CampaignID,
		CampaignID:  pending.CampaignID,
		Position:    pending.Position,
		Title:       pending.Title,
		Detail:      pending.Detail,
		PlanID:      pending.PlanID,
		ButtonLabel: pending.ButtonLabel,
		ClaimURL:    pending.ClaimURL,
	}
	fmt.Print("[claim] Nhận deal này ngay? (y/N): ")
	answer := readConsoleLine()
	if strings.TrimSpace(strings.ToLower(answer)) != "y" {
		fmt.Println("[claim] Đã hủy. Deal vẫn giữ trong pending-claim.json, chạy lại `zcode-deal claim` khi muốn nhận.")
		return 0
	}
	summary, ok := claimDeal(deal, ctx)
	if ok {
		fmt.Println("[claim] NHẬN THÀNH CÔNG:")
		fmt.Println(summary)
		return 0
	}
	fmt.Println("[claim] KHÔNG nhận được:")
	fmt.Println(summary)
	return 1
}

// readConsoleLine đọc 1 dòng từ stdin (dùng cho xác nhận y/N của
// lệnh claim). Lỗi đọc coi như trả lời rỗng (mặc định không nhận).
func readConsoleLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return ""
	}
	return scanner.Text()
}

// openURL mở link (open_url action) bằng trình duyệt mặc định.
func openURL(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
