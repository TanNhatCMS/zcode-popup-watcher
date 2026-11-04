//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

// runTray tren non-Windows: khoi dong daemon tach roi (giong ban
// Bun truoc day), lenh thoat ngay, tat duoc terminal. Icon tray
// goc khong co tren Linux/macOS (can GTK/AppIndicator rieng),
// daemon bao bang thong bao he thong.
func runTray(opts TrayOptions) int {
	checker.EnsureStateDir()
	if pid, ok := readPidFrom(checker.PidFile()); ok && pidAlive(pid) {
		fmt.Printf("[tray] Daemon da chay roi (PID %d).\n", pid)
		fmt.Println("[tray] Xem trang thai: zcode-deal status | Thoat: zcode-deal stop")
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("[tray] LOI: khong xac dinh duoc duong dan binary de tach daemon: %v\n", err)
		return 2
	}
	childArgs := []string{"__tray-child", "--interval", fmt.Sprintf("%d", opts.IntervalSec)}
	if opts.ConfigDir != "" {
		childArgs = append(childArgs, "--config-dir", opts.ConfigDir)
	}
	if opts.AutoOpen {
		childArgs = append(childArgs, "--open")
	}
	cmd := exec.Command(exe, childArgs...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Printf("[tray] LOI: khong khoi dong duoc daemon: %v\n", err)
		return 1
	}
	_ = cmd.Process.Release()
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if pid, ok := readPidFrom(checker.PidFile()); ok && pidAlive(pid) {
			fmt.Printf("[tray] Daemon da chay (PID %d). Ban co the tat cua so terminal nay.\n", pid)
			fmt.Printf("[tray] Log: %s\n", checker.LogFile())
			fmt.Println("[tray] He nay khong co menu tray goc: daemon chay nen + thong bao he thong, thoat bang stop.")
			fmt.Println("[tray] Xem trang thai: zcode-deal status | Thoat: zcode-deal stop")
			return 0
		}
	}
	fmt.Println("[tray] LOI: daemon khong ghi PID file. Xem log (neu co):")
	if logText, err := os.ReadFile(checker.LogFile()); err == nil {
		lines := strings.Split(strings.TrimSpace(string(logText)), "\n")
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		for _, line := range lines {
			fmt.Println(line)
		}
	} else {
		fmt.Println("(chua co log file)")
	}
	return 1
}

// sleepWithTrigger cho het ms, nhung neu co file trigger thi tieu
// thu file va tra ve true ngay (can check ngay). Port ban Bun.
func sleepWithTrigger(ms time.Duration) bool {
	step := 2 * time.Second
	remaining := ms
	for remaining > 0 {
		if _, err := os.Stat(checker.TriggerFile()); err == nil {
			_ = os.Remove(checker.TriggerFile())
			return true
		}
		wait := step
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)
		remaining -= wait
	}
	return false
}

// runTrayChild la daemon con do lenh tray tao ra (chi non-Windows).
func runTrayChild(opts TrayOptions) int {
	checker.EnsureStateDir()
	configDir := checker.ConfigDir(opts.ConfigDir)
	interval := time.Duration(opts.IntervalSec) * time.Second
	locale := os.Getenv("ZCODE_LOCALE")
	if locale == "" {
		locale = "en-US"
	}
	appVersion := os.Getenv("ZCODE_APP_VERSION")
	if appVersion == "" {
		appVersion = "3.17.2"
	}
	cleanup := func() { removePidFileIfMine(os.Getpid()) }
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		logLine(fmt.Sprintf("[tray-child] Nhan %s, thoat.", sig))
		cleanup()
		os.Exit(0)
	}()
	_ = os.WriteFile(checker.PidFile(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	logLine(fmt.Sprintf("[tray-child] Daemon bat dau (PID %d), interval %ds.", os.Getpid(), opts.IntervalSec))
	deviceMid, token, err := resolveAuth(configDir)
	if err != nil {
		logLine(fmt.Sprintf("[tray-child] LOI auth: %v", err))
		cleanup()
		return 2
	}
	if token == "" {
		logLine("[tray-child] Luu y: khong co token, dang check an danh.")
	}
	seq := 0
	failures := 0
	seen := checker.LoadSeen()
	for {
		deals, err := checker.QueryDeals(token, deviceMid, locale, appVersion, seq)
		if err != nil {
			failures++
			delay := 30 * time.Second * time.Duration(1<<minInt(failures-1, 5))
			if delay > 10*time.Minute {
				delay = 10 * time.Minute
			}
			logLine(fmt.Sprintf("[tray-child] Check loi (%v). Doi %s.", err, delay))
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
		logLine(fmt.Sprintf("[tray-child] Co %d delivery, %d moi: %s", len(deals), len(fresh), keys))
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
			Notify("ZCode - Co deal moi!", message)
			if opts.AutoOpen {
				logLine(OpenZcode())
			}
		}
		sleepWithTrigger(interval)
	}
}
