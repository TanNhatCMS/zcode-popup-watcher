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
	trayOpts       TrayOptions
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

	mDeal := systray.AddMenuItem("Có deal mới", "Bấm để mở ZCode nhận deal")
	mDeal.Hide()
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("Mở ZCode", "Mở app ZCode")
	mCheck := systray.AddMenuItem("Kiểm tra ngay", "Kiểm tra deal ngay, không đợi hết chu kỳ")
	mLog := systray.AddMenuItem("Mở log", "Mở file log")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Thoát", "Thoát app tray")

	checker.EnsureStateDir()
	_ = os.WriteFile(checker.GotrayPidFile(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	logLine(fmt.Sprintf("[gotray] Tray Go đã chạy (PID %d), interval %ds.", os.Getpid(), trayOpts.IntervalSec))

	go trayHandleMenuClicks(mDeal, mOpen, mCheck, mLog, mQuit)
	go trayPollLoop(mDeal)
}

func trayHandleMenuClicks(mDeal, mOpen, mCheck, mLog, mQuit *systray.MenuItem) {
	for {
		select {
		case <-mDeal.ClickedCh:
			logLine(OpenZcode())
		case <-mOpen.ClickedCh:
			logLine(OpenZcode())
		case <-mCheck.ClickedCh:
			requestTrayCheckNow()
		case <-mLog.ClickedCh:
			if err := openFile(checker.LogFile()); err != nil {
				logLine(fmt.Sprintf("Không mở được log: %v", err))
			} else {
				logLine(fmt.Sprintf("Đã gọi mở log: %s", checker.LogFile()))
			}
		case <-mQuit.ClickedCh:
			systray.Quit()
			return
		case <-trayQuitCh:
			return
		}
	}
}

func trayPollLoop(mDeal *systray.MenuItem) {
	config := checker.ConfigDir(trayOpts.ConfigDir)
	locale := os.Getenv("ZCODE_LOCALE")
	if locale == "" {
		locale = "en-US"
	}
	appVersion := os.Getenv("ZCODE_APP_VERSION")
	if appVersion == "" {
		appVersion = "3.17.2"
	}
	deviceMid, token, err := resolveAuth(config)
	if err != nil {
		logLine(fmt.Sprintf("[gotray] LỖI auth: %v", err))
		systray.SetTooltip("ZCode Deal Watcher - LỖI auth")
		return
	}
	if token == "" {
		logLine("[gotray] Lưu ý: không có token, đang check ẩn danh.")
	}
	interval := time.Duration(trayOpts.IntervalSec) * time.Second
	if interval < 300*time.Second {
		logLine("[gotray] Cảnh báo: interval dưới 300s dễ bị 429.")
	}
	runPoll(token, deviceMid, locale, appVersion, interval, false, pollCallbacks{
		log: func(f string, a ...any) { logLine(fmt.Sprintf("[gotray] "+f, a...)) },
		onError: func(err error) {
			systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - lỗi check: %v", err))
		},
		onSuccess: func(deals, fresh []checker.Deal) {
			systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - check cuối: %d delivery, %d mới", len(deals), len(fresh)))
		},
		onFresh: func(fresh []checker.Deal, message string) {
			ids := make([]string, 0, len(fresh))
			for _, deal := range fresh {
				ids = append(ids, deal.Key)
			}
			checker.WriteNotifyFile(strings.Join(ids, ","), "ZCode - Có deal mới!", message)
			systray.SetIcon(iconAlert)
			systray.SetTooltip("ZCode Deal Watcher - CÓ DEAL MỚI! Bấm vào menu deal để mở ZCode.")
			mDeal.SetTitle("Có deal mới - bấm để mở ZCode")
			mDeal.Show()
			Notify("ZCode - Có deal mới!", message)
			if trayOpts.AutoOpen {
				logLine(OpenZcode())
			}
		},
		waitInterval: trayWaitOrCheckNow,
		waitError:    trayWaitOrCheckNow,
	})
}

func trayWaitOrCheckNow(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-trayCheckNowCh:
		return true
	case <-trayQuitCh:
		return false
	}
}

func trayOnExit() {
	close(trayQuitCh)
	_ = os.Remove(checker.GotrayPidFile())
	logLine("[gotray] App tray đã thoát.")
}
