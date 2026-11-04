// main.go -- Tray app Go cho ZCode Deal Watcher.
//
// Thay the app Electron (126MB Chromium) bang 1 file exe Go duy
// nhat (~10MB): icon + menu native qua lib systray, khong
// PowerShell, khong script roi, khong can cai them gi.
// Cross-build Windows tu Linux: GOOS=windows go build.
//
// Chi BAO deal, khong tu nhan thay ban. Khong POST action.
// Token chi doc tu ~/.zcode/v2 tren cung may/user, khong in,
// khong ghi file, chi song trong RAM luc chay.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"

	"github.com/TanNhatCMS/zcode-popup-watcher/tray-go/internal/checker"
)

const version = "1.0.0"

//go:embed assets/icon.ico
var iconNormal []byte

//go:embed assets/icon-alert.ico
var iconAlert []byte

var (
	intervalSec = flag.Int("interval", 600, "Chu ky check (giay, khong dat duoi 300)")
	configDir   = flag.String("config-dir", "", "Thu muc config ZCode (mac dinh ~/.zcode/v2)")
	autoOpen    = flag.Bool("open", false, "Tu mo ZCode ngay khi co deal")
	showVersion = flag.Bool("version", false, "In version roi thoat")
)

var (
	checkNowCh = make(chan struct{}, 1)
	quitCh     = make(chan struct{})
	seq        = 0
)

func logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Println(line)
	checker.AppendLog(line)
}

func openPath(target string) string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("Khong mo duoc %s: %v", target, err)
	}
	return fmt.Sprintf("Da goi mo: %s", target)
}

func openZcode() string {
	var candidates []string
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates,
				filepath.Join(local, "Programs", "ZCode", "ZCode.exe"),
				filepath.Join(local, "ZCode", "ZCode.exe"))
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			candidates = append(candidates, filepath.Join(pf, "ZCode", "ZCode.exe"))
		}
	} else if runtime.GOOS == "darwin" {
		candidates = append(candidates, "/Applications/ZCode.app")
	} else {
		candidates = append(candidates, "/usr/bin/zcode", "/usr/local/bin/zcode")
	}
	for _, target := range candidates {
		if _, err := os.Stat(target); err == nil {
			return openPath(target)
		}
	}
	return "Khong tim thay ZCode tren may nay, ban mo app thu cong nhe."
}

func requestCheckNow() {
	select {
	case checkNowCh <- struct{}{}:
	default:
	}
}

func onReady() {
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
	pidFile := checker.GotrayPidFile()
	_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	logf("[gotray] Tray Go da chay (PID %d), interval %ds.", os.Getpid(), *intervalSec)

	go handleMenuClicks(mDeal, mOpen, mCheck, mLog, mQuit)
	go pollLoop(mDeal)
}

func handleMenuClicks(mDeal, mOpen, mCheck, mLog, mQuit *systray.MenuItem) {
	for {
		select {
		case <-mDeal.ClickedCh:
			logf("%s", openZcode())
		case <-mOpen.ClickedCh:
			logf("%s", openZcode())
		case <-mCheck.ClickedCh:
			requestCheckNow()
		case <-mLog.ClickedCh:
			logf("%s", openPath(checker.LogFile()))
		case <-mQuit.ClickedCh:
			systray.Quit()
			return
		case <-quitCh:
			return
		}
	}
}

func pollLoop(mDeal *systray.MenuItem) {
	config := checker.ConfigDir(*configDir)
	locale := os.Getenv("ZCODE_LOCALE")
	if locale == "" {
		locale = "en-US"
	}
	appVersion := os.Getenv("ZCODE_APP_VERSION")
	if appVersion == "" {
		appVersion = "3.17.2"
	}
	deviceMid, ok := checker.ReadDeviceMid(config)
	if !ok {
		logf("[gotray] LOI auth: khong tim thay deviceMid trong telemetry-state.json (%s).", config)
		systray.SetTooltip("ZCode Deal Watcher - LOI: khong co deviceMid")
		return
	}
	token := strings.TrimSpace(os.Getenv("ZCODE_JWT_TOKEN"))
	if token == "" {
		if jwt, ok := checker.ReadZcodeJWT(config); ok {
			token = jwt
		}
	}
	if token == "" {
		logf("[gotray] Luu y: khong co token, dang check an danh.")
	}
	interval := time.Duration(*intervalSec) * time.Second
	if interval < 300*time.Second {
		logf("[gotray] Canh bao: interval duoi 300s de bi 429.")
	}
	seen := checker.LoadSeen()
	failures := 0
	for {
		deals, err := checker.QueryDeals(token, deviceMid, locale, appVersion, seq)
		seq++
		if err != nil {
			failures++
			delay := 30 * time.Second * time.Duration(1<<min(failures-1, 5))
			if delay > 10*time.Minute {
				delay = 10 * time.Minute
			}
			logf("[gotray] Check loi (%v). Doi %s.", err, delay)
			systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - loi check: %v", err))
			if !waitOrCheckNow(delay) {
				return
			}
			continue
		}
		failures = 0
		var fresh []checker.Deal
		for _, d := range deals {
			if !seen[d.Key] {
				fresh = append(fresh, d)
			}
		}
		keys := make([]string, 0, len(deals))
		for _, d := range deals {
			keys = append(keys, d.Key)
		}
		logf("[gotray] Co %d delivery, %d moi: %s", len(deals), len(fresh), strings.Join(keys, ", "))
		systray.SetTooltip(fmt.Sprintf("ZCode Deal Watcher - check cuoi: %d delivery, %d moi", len(deals), len(fresh)))
		if len(fresh) > 0 {
			for _, d := range fresh {
				seen[d.Key] = true
			}
			checker.SaveSeen(seen)
			lines := []string{"ZCode co deal moi! Mo app va bam nut nhan trong app.", ""}
			for _, d := range fresh {
				lines = append(lines, fmt.Sprintf("- [%s] %s (priority %v)", d.Position, d.CampaignID, d.Priority))
				if d.Title != "" {
					lines = append(lines, "  "+d.Title)
				}
				if d.Detail != "" {
					lines = append(lines, "  "+d.Detail)
				}
			}
			lines = append(lines, "", "Tool nay chi bao, khong tu nhan thay ban.")
			message := strings.Join(lines, "\n")
			ids := make([]string, 0, len(fresh))
			for _, d := range fresh {
				ids = append(ids, d.Key)
			}
			checker.WriteNotifyFile(strings.Join(ids, ","), "ZCode - Co deal moi!", message)
			systray.SetIcon(iconAlert)
			systray.SetTooltip("ZCode Deal Watcher - CO DEAL MOI! Bam vao icon de mo ZCode.")
			mDeal.SetTitle("Co deal moi - bam de mo ZCode")
			mDeal.Show()
			_ = beeep.Notify("ZCode - Co deal moi!", message, "")
			if *autoOpen {
				logf("%s", openZcode())
			}
		}
		if !waitOrCheckNow(interval) {
			return
		}
	}
}

// waitOrCheckNow cho het d nhung tra ve false neu app dang thoat,
// tra ve true neu het thoi gian hoac user bam "Kiem tra ngay".
func waitOrCheckNow(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-checkNowCh:
		return true
	case <-quitCh:
		return false
	}
}

func onExit() {
	close(quitCh)
	_ = os.Remove(checker.GotrayPidFile())
	logf("[gotray] App tray da thoat.")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Printf("zcode-deal-tray (Go) %s\n", version)
		return
	}
	systray.Run(onReady, onExit)
}
