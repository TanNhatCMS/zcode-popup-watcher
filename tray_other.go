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

// runTray trên non-Windows: khởi động daemon tách rời (giống bạn
// Bun trước đây), lệnh thoát ngay, tắt được terminal. Icon tray
// gốc không có trên Linux/macOS (cần GTK/AppIndicator riêng),
// daemon báo bằng thông báo hệ thống.
func runTray(opts TrayOptions) int {
	checker.EnsureStateDir()
	if pid, ok := readPidFrom(checker.PidFile()); ok && pidAlive(pid) {
		fmt.Printf("[tray] Daemon đã chạy rồi (PID %d).\n", pid)
		fmt.Println("[tray] Xem trạng thái: zcode-deal status | Thoát: zcode-deal stop")
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("[tray] LỖI: không xác định được đường dẫn binary để tách daemon: %v\n", err)
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
		fmt.Printf("[tray] LỖI: không khởi động được daemon: %v\n", err)
		return 1
	}
	_ = cmd.Process.Release()
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if pid, ok := readPidFrom(checker.PidFile()); ok && pidAlive(pid) {
			fmt.Printf("[tray] Daemon đã chạy (PID %d). Bạn có thể tắt cửa sổ terminal này.\n", pid)
			fmt.Printf("[tray] Log: %s\n", checker.LogFile())
			fmt.Println("[tray] Hệ này không có menu tray gốc: daemon chạy nền + thông báo hệ thống, thoát bằng stop.")
			fmt.Println("[tray] Xem trạng thái: zcode-deal status | Thoát: zcode-deal stop")
			return 0
		}
	}
	fmt.Println("[tray] LỖI: daemon không ghi PID file. Xem log (nếu có):")
	if logText, err := os.ReadFile(checker.LogFile()); err == nil {
		lines := strings.Split(strings.TrimSpace(string(logText)), "\n")
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		for _, line := range lines {
			fmt.Println(line)
		}
	} else {
		fmt.Println("(chưa có log file)")
	}
	return 1
}

// sleepWithTrigger cho hết ms, nhưng nếu có file trigger thì tieu
// thử file và trả về true ngay (cần check ngay). Port bản Bun.
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

// runTrayChild là daemon con do lệnh tray tạo ra (chỉ non-Windows).
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
		logLine(fmt.Sprintf("[tray-child] Nhận %s, thoát.", sig))
		cleanup()
		os.Exit(0)
	}()
	_ = os.WriteFile(checker.PidFile(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	logLine(fmt.Sprintf("[tray-child] Daemon bắt đầu (PID %d), interval %ds.", os.Getpid(), opts.IntervalSec))
	deviceMid, token, err := resolveAuth(configDir)
	if err != nil {
		logLine(fmt.Sprintf("[tray-child] LỖI auth: %v", err))
		cleanup()
		return 2
	}
	if token == "" {
		logLine("[tray-child] Lưu ý: không có token, đang check ẩn danh.")
	}
	return runPoll(token, deviceMid, locale, appVersion, interval, false, pollCallbacks{
		log: func(f string, a ...any) { logLine(fmt.Sprintf("[tray-child] "+f, a...)) },
		onFresh: func(fresh []checker.Deal, message string) {
			ids := make([]string, 0, len(fresh))
			for _, deal := range fresh {
				ids = append(ids, deal.Key)
			}
			checker.WriteNotifyFile(strings.Join(ids, ","), "ZCode - Có deal mới!", message)
			Notify("ZCode - Có deal mới!", message)
			if opts.AutoOpen {
				logLine(OpenZcode())
			}
		},
		waitInterval: func(d time.Duration) bool { sleepWithTrigger(d); return true },
		waitError:    func(d time.Duration) bool { time.Sleep(d); return true },
	})
}
