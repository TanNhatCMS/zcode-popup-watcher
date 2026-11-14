//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/getlantern/systray"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

//go:embed assets/icon.ico
var iconNormal []byte

//go:embed assets/icon-alert.ico
var iconAlert []byte

var (
	trayCheckNowCh = make(chan struct{}, 1)
	trayQuitCh     = make(chan struct{})
	traySeq        = 0
	trayOpts       TrayOptions
	// trayCtx là auth của phiên tray, resolve MỘT lần trong trayOnReady
	// trước khi các goroutine menu/poll chạy — menu "ấn xem chi tiết"
	// dùng nó để mở popup nhận deal mà không phải đọc lại credentials.
	trayCtx claimContext
)

func requestTrayCheckNow() {
	select {
	case trayCheckNowCh <- struct{}{}:
	default:
	}
}

// runTray trên Windows: app systray (menu native, icon, toast).
// Hàm này block cho den khi user chọn Thoát trong menu.
func runTray(opts TrayOptions) int {
	trayOpts = opts
	systray.Run(trayOnReady, trayOnExit)
	return 0
}

// runTrayChild không có y nghia trên Windows (tray là systray,
// không có daemon con). Giua lại stub để main.go gọi chung được.
func runTrayChild(opts TrayOptions) int {
	fmt.Println("[tray] Lệnh nội bộ __tray-child chỉ dùng trên non-Windows. Trên Windows, lệnh tray mở app systray.")
	return 2
}

func trayOnReady() {
	systray.SetIcon(iconNormal)
	systray.SetTitle("ZCode Deal Watcher")
	systray.SetTooltip("ZCode Deal Watcher - đang theo dõi deal")

	// Resolve auth ngay khi mở tray để cả poll loop lẫn menu popup
	// dùng chung một claimContext.
	config := checker.ConfigDir(trayOpts.ConfigDir)
	deviceMid, token, authErr := resolveAuth(config)
	if authErr != nil {
		logLine(fmt.Sprintf("[gotray] LỖI auth: %v", authErr))
		systray.SetTooltip("ZCode Deal Watcher - LỖI auth")
	} else {
		trayCtx = newClaimContext(token, deviceMid)
		if token == "" {
			logLine("[gotray] Lưu ý: không có token, đang check ẩn danh.")
		}
	}

	mDeal := systray.AddMenuItem("Có deal mới - ấn xem chi tiết", "Mở popup nhận deal")
	mDeal.Hide()
	// Có deal đang chờ (pending-claim.json) thì hiện menu ngay từ đầu
	// — người dùng có thể mở lại popup mà không đợi deal mới.
	if _, hasPending := checker.LoadPendingClaim(); hasPending {
		mDeal.Show()
	}
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("Mở ZCode", "Mở app ZCode")
	mCheck := systray.AddMenuItem("Kiểm tra ngay", "Kiểm tra deal ngay, không đợi hết chu kỳ")
	mSettingsClick := systray.AddMenuItem("Cài đặt…", "Mở cửa sổ cài đặt: khởi động cùng hệ thống, tài khoản check deal, khung giờ + tần suất")
	mInfo := systray.AddMenuItem("Thông tin", "Thông tin app, phiên bản, nhật ký, kiểm tra cập nhật")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Thoát", "Thoát app tray")

	checker.EnsureStateDir()
	_ = os.WriteFile(checker.GotrayPidFile(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	// Dọn file .old của đợt tự cập nhật trước (exe đổi tên khi đang chạy).
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
	logLine(fmt.Sprintf("[gotray] Tray Go đã chạy (PID %d), interval %ds.", os.Getpid(), trayOpts.IntervalSec))

	go trayHandleMenuClicks(mDeal, mOpen, mCheck, mQuit)
	go func() {
		for range mSettingsClick.ClickedCh {
			showCheckSettingsWindow()
		}
	}()
	go func() {
		for range mInfo.ClickedCh {
			showInfoWindow()
		}
	}()
	if authErr == nil {
		go trayPollLoop(mDeal, token, deviceMid)
	}
}

// trayShowDealPopup mở lại popup nhận deal cho deal đang chờ (đọc từ
// pending-claim.json — deal mới nhất tool đã báo). Không có deal chờ
// thì mở ZCode thay thế.
func trayShowDealPopup() {
	pending, ok := checker.LoadPendingClaim()
	if !ok {
		logLine("[gotray] Chưa có deal nào đang chờ nhận — mở ZCode thay thế.")
		logLine(OpenZcode())
		return
	}
	logLine(fmt.Sprintf("[gotray] Mở popup cho deal đang chờ: %s", pending.Position+":"+pending.CampaignID))
	runClaimPopupFlow(dealFromPending(pending), trayCtx)
}

func trayHandleMenuClicks(mDeal, mOpen, mCheck, mQuit *systray.MenuItem) {
	for {
		select {
		case <-mDeal.ClickedCh:
			trayShowDealPopup()
		case <-mOpen.ClickedCh:
			logLine(OpenZcode())
		case <-mCheck.ClickedCh:
			requestTrayCheckNow()
		case <-mQuit.ClickedCh:
			systray.Quit()
			return
		case <-trayQuitCh:
			return
		}
	}
}

func trayPollLoop(mDeal *systray.MenuItem, token, deviceMid string) {
	ctx := trayCtx
	baseInterval := time.Duration(trayOpts.IntervalSec) * time.Second
	if baseInterval < 300*time.Second {
		logLine("[gotray] Cảnh báo: interval dưới 300s dễ bị 429.")
	}
	seen := checker.LoadSeen()
	failures := 0
	wait := time.Duration(0)
	pendingCheckNow := false
	outsideLogged := false
	for {
		proceed, checkNow := trayWaitOrCheckNow(wait)
		if !proceed {
			return
		}
		pendingCheckNow = pendingCheckNow || checkNow
		// Nạp lại settings mỗi vòng: đổi account/khung giờ/tần suất từ
		// cửa sổ cài đặt có hiệu lực ngay vòng kế tiếp.
		settings := checker.LoadDealSettings()
		checkToken, checkLabel := checkTokenFor(settings.CheckAccount, token)
		_ = checkLabel
		interval := baseInterval
		if settings.CheckInterval > 0 {
			interval = time.Duration(settings.CheckInterval) * time.Second
		}
		wait = interval
		// Ngoài khung giờ check → KHÔNG gọi API, ngủ 60s rồi nạp lại
		// settings. "Kiểm tra ngay" bypass khung giờ 1 lần.
		if settings.CheckWindowEnabled && !pendingCheckNow {
			if _, minutes := vnNow(); !settings.InCheckWindow(minutes) {
				if !outsideLogged {
					logLine(fmt.Sprintf("[gotray] Ngoài khung giờ check (%s-%s giờ VN) — KHÔNG gọi API, đợi 60s mỗi vòng.", settings.CheckFrom, settings.CheckTo))
					outsideLogged = true
				}
				wait = 60 * time.Second
				continue
			}
			outsideLogged = false
		}
		wasCheckNow := pendingCheckNow
		pendingCheckNow = false
		deals, err := checker.QueryDeals(checkToken, deviceMid, ctx.locale, ctx.appVersion, traySeq)
		traySeq++
		if err != nil {
			failures++
			delay := 30 * time.Second * time.Duration(1<<minInt(failures-1, 5))
			if delay > 10*time.Minute {
				delay = 10 * time.Minute
			}
			logLine(fmt.Sprintf("[gotray] Check lỗi (%v). Đợi %s.", err, delay))
			systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - lỗi check: %v", err))
			wait = delay
			// Check do người dùng bấm → báo lỗi bằng popup cho rõ.
			if wasCheckNow {
				go alertBox("ZCode Deal - Kiểm tra lỗi",
					fmt.Sprintf("Kiểm tra deal lỗi lúc %s giờ VN:\n%v", vnClockLabel(), err), true)
			}
			continue
		}
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
		logLine(fmt.Sprintf("[gotray] Có %d delivery, %d mới: %s", len(deals), len(fresh), keys))
		systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - check cuối: %d delivery, %d mới", len(deals), len(fresh)))
		if len(fresh) > 0 {
			for _, deal := range fresh {
				seen[deal.Key] = true
			}
			checker.SaveSeen(seen)
			message := formatDealsMessage(fresh)
			ids := make([]string, 0, len(fresh))
			for _, deal := range fresh {
				ids = append(ids, deal.Key)
			}
			checker.WriteNotifyFile(strings.Join(ids, ","), "ZCode - Có deal mới!", message)
			systray.SetIcon(iconAlert)
			systray.SetTooltip("ZCode Deal Watcher - CÓ DEAL MỚI! Bấm \"Nhận deal\" trên popup để nhận.")
			mDeal.SetTitle("Có deal mới - ấn xem chi tiết")
			mDeal.Show()
			Notify("ZCode - Có deal mới!", message)
			// Popup nhận deal: hiện cửa sổ nổi, người dùng bấm
			// "Nhận deal" tool mới claim (chạy nền, không chặn poll).
			offerDealClaim(fresh, ctx, false)
			if trayOpts.AutoOpen {
				logLine(OpenZcode())
			}
			// Check do người dùng bấm + deal không claim được từ tool →
			// vẫn cần popup trạng thái (popup claim đã hiện nếu claimable).
			if wasCheckNow && firstClaimable(fresh) == nil {
				go alertBox("ZCode Deal - Kiểm tra xong",
					fmt.Sprintf("Có %d deal mới lúc %s giờ VN nhưng không có nút nhận tool thay được — mở ZCode để nhận.\n\n%s",
						len(fresh), vnClockLabel(), message), false)
			}
		} else if wasCheckNow {
			// Người dùng vừa bấm "Kiểm tra ngay" → phải thông báo kể cả
			// khi không có deal mới (popup trạng thái, chạy nền).
			go alertBox("ZCode Deal - Kiểm tra xong",
				fmt.Sprintf("KHÔNG có deal mới lúc %s giờ VN.\n\nDelivery hiện có: %d\nTài khoản check: %s\nLần check sau: theo khung giờ/cài đặt",
					vnClockLabel(), len(deals), checkAccountDisplay(checkLabel)), false)
		}
		wait = interval
	}
}

// trayWaitOrCheckNow chờ đợi giữa các vòng poll: hết timer →
// (true, false); "Kiểm tra ngay" → (true, true) — bypass khung giờ 1
// lần; Thoát → (false, false).
func trayWaitOrCheckNow(d time.Duration) (bool, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true, false
	case <-trayCheckNowCh:
		return true, true
	case <-trayQuitCh:
		return false, false
	}
}

func trayOnExit() {
	close(trayQuitCh)
	_ = os.Remove(checker.GotrayPidFile())
	logLine("[gotray] App tray đã thoát.")
}
