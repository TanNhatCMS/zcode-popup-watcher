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
)

func requestTrayCheckNow() {
	select {
	case trayCheckNowCh <- struct{}{}:
	default:
	}
}

// runTray tren Windows: app systray (menu native, icon, toast).
// Ham nay block cho den khi user chon Thoat trong menu.
func runTray(opts TrayOptions) int {
	trayOpts = opts
	systray.Run(trayOnReady, trayOnExit)
	return 0
}

// runTrayChild khong co y nghia tren Windows (tray la systray,
// khong co daemon con). Giua lai stub de main.go goi chung duoc.
func runTrayChild(opts TrayOptions) int {
	fmt.Println("[tray] Lenh noi bo __tray-child chi dung tren non-Windows. Tren Windows, lenh tray mo app systray.")
	return 2
}

func trayOnReady() {
	systray.SetIcon(iconNormal)
	systray.SetTitle("ZCode Deal Watcher")
	systray.SetTooltip("ZCode Deal Watcher - dang theo doi deal")

	mDeal := systray.AddMenuItem("Co deal moi", "Bam de mo ZCode nhan deal")
	mDeal.Hide()
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("Mo ZCode", "Mo app ZCode")
	mCheck := systray.AddMenuItem("Kiem tra ngay", "Kiem tra deal ngay, khong doi het chu ky")
	mLog := systray.AddMenuItem("Mo log", "Mo file log")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Thoat", "Thoat app tray")

	checker.EnsureStateDir()
	_ = os.WriteFile(checker.GotrayPidFile(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	logLine(fmt.Sprintf("[gotray] Tray Go da chay (PID %d), interval %ds.", os.Getpid(), trayOpts.IntervalSec))

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
				logLine(fmt.Sprintf("Khong mo duoc log: %v", err))
			} else {
				logLine(fmt.Sprintf("Da goi mo log: %s", checker.LogFile()))
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
		logLine(fmt.Sprintf("[gotray] LOI auth: %v", err))
		systray.SetTooltip("ZCode Deal Watcher - LOI auth")
		return
	}
	if token == "" {
		logLine("[gotray] Luu y: khong co token, dang check an danh.")
	}
	interval := time.Duration(trayOpts.IntervalSec) * time.Second
	if interval < 300*time.Second {
		logLine("[gotray] Canh bao: interval duoi 300s de bi 429.")
	}
	seen := checker.LoadSeen()
	failures := 0
	for {
		deals, err := checker.QueryDeals(token, deviceMid, locale, appVersion, traySeq)
		traySeq++
		if err != nil {
			failures++
			delay := 30 * time.Second * time.Duration(1<<minInt(failures-1, 5))
			if delay > 10*time.Minute {
				delay = 10 * time.Minute
			}
			logLine(fmt.Sprintf("[gotray] Check loi (%v). Doi %s.", err, delay))
			systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - loi check: %v", err))
			if !trayWaitOrCheckNow(delay) {
				return
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
			keys = "(rong)"
		}
		logLine(fmt.Sprintf("[gotray] Co %d delivery, %d moi: %s", len(deals), len(fresh), keys))
		systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - check cuoi: %d delivery, %d moi", len(deals), len(fresh)))
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
			checker.WriteNotifyFile(strings.Join(ids, ","), "ZCode - Co deal moi!", message)
			systray.SetIcon(iconAlert)
			systray.SetTooltip("ZCode Deal Watcher - CO DEAL MOI! Bam vao menu deal de mo ZCode.")
			mDeal.SetTitle("Co deal moi - bam de mo ZCode")
			mDeal.Show()
			Notify("ZCode - Co deal moi!", message)
			if trayOpts.AutoOpen {
				logLine(OpenZcode())
			}
		}
		if !trayWaitOrCheckNow(interval) {
			return
		}
	}
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
	logLine("[gotray] App tray da thoat.")
}
