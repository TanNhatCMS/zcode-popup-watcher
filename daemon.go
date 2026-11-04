package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

// TrayOptions là tùy chọn chung cho lệnh tray (cả Windows
// systray lẫn non-Windows daemon).
type TrayOptions struct {
	IntervalSec int
	ConfigDir   string
	AutoOpen    bool
}

func readPidFrom(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func removePidFileIfMine(pid int) {
	if existing, ok := readPidFrom(checker.PidFile()); ok && existing == pid {
		_ = os.Remove(checker.PidFile())
	}
}

// handleStatus in trạng thái daemon + app tray Go + log cuối
// + số deal đã thấy (port handleStatus bản Bun).
func handleStatus() int {
	checker.EnsureStateDir()
	if pid, ok := readPidFrom(checker.PidFile()); ok {
		if pidAlive(pid) {
			fmt.Printf("[tray] Daemon DANG CHAY (PID %d).\n", pid)
		} else {
			fmt.Printf("[tray] PID file còn %d nhưng tiến trình đã chết (stale).\n", pid)
		}
	} else {
		fmt.Println("[tray] Daemon KHÔNG chạy.")
	}
	if pid, ok := readPidFrom(checker.GotrayPidFile()); ok {
		if pidAlive(pid) {
			fmt.Printf("[tray] App tray Go DANG CHAY (PID %d).\n", pid)
		} else {
			fmt.Printf("[tray] gotray.pid còn %d nhưng tiến trình đã chết (stale) - app tray không còn, chạy lại lệnh tray để mở lại.\n", pid)
		}
	} else {
		fmt.Println("[tray] App tray Go KHÔNG chạy (không có gotray.pid). Chạy lệnh tray để mở app.")
	}
	if logText, err := os.ReadFile(checker.LogFile()); err == nil {
		fmt.Println("[tray] 10 dòng log cuối:")
		lines := strings.Split(strings.TrimSpace(string(logText)), "\n")
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
	} else {
		fmt.Println("[tray] Chưa có log file.")
	}
	seen := checker.LoadSeen()
	fmt.Printf("[tray] Đã thấy %d deal (seen file).\n", len(seen))
	return 0
}

// handleStop dùng daemon: SIGTERM (Unix) rồi đợi 3s, nếu còn sống
// thì SIGKILL; trên Windows dùng Kill trực tiếp (Go không ho tro
// SIGTERM trên Windows, còn Node thì TerminateProcess nền hanh vi
// tương đương). Port handleStop bản Bun.
func handleStop() int {
	pid, ok := readPidFrom(checker.PidFile())
	if !ok {
		fmt.Println("[tray] Không có daemon nào đang chạy (không thấy PID file).")
		return 0
	}
	if !pidAlive(pid) {
		fmt.Printf("[tray] PID %d đã chết rồi, dọn PID file cũ.\n", pid)
		_ = os.Remove(checker.PidFile())
		return 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("[tray] Không tìm thấy tiến trình %d: %v\n", pid, err)
		return 1
	}
	if err := signalDaemon(proc); err != nil {
		fmt.Printf("[tray] Không gửi được tin hiệu dùng tới PID %d: %v\n", pid, err)
		return 1
	}
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if !pidAlive(pid) {
			break
		}
	}
	if pidAlive(pid) {
		_ = proc.Kill()
	}
	_ = os.Remove(checker.PidFile())
	fmt.Printf("[tray] Đã thoát daemon (PID %d). App tray Go (nếu đang chạy) không bị ảnh hưởng, thoát bằng menu Thoát trong tray.\n", pid)
	return 0
}

// formatDealsMessage tạo thông báo deal mới (dùng chung cho
// check/watch/daemon/tray, giống bản Bun).
func formatDealsMessage(fresh []checker.Deal) string {
	lines := []string{"ZCode có deal mới! Mở app và bấm nút nhận trong app.", ""}
	for _, deal := range fresh {
		lines = append(lines, fmt.Sprintf("- [%s] %s (priority %v)", deal.Position, deal.CampaignID, deal.Priority))
		if deal.Title != "" {
			lines = append(lines, "  "+deal.Title)
		}
		if deal.Detail != "" {
			lines = append(lines, "  "+deal.Detail)
		}
	}
	lines = append(lines, "", "Tool này chỉ báo, không tự nhận thay bạn.")
	return strings.Join(lines, "\n")
}

func freshDealKeys(deals []checker.Deal) []string {
	keys := make([]string, 0, len(deals))
	for _, deal := range deals {
		keys = append(keys, deal.Key)
	}
	return keys
}
