package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

// TrayOptions la tuy chon chung cho lenh tray (ca Windows
// systray lan non-Windows daemon).
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

// handleStatus in trang thai daemon + app tray Go + log cuoi
// + so deal da thay (port handleStatus ban Bun).
func handleStatus() int {
	checker.EnsureStateDir()
	if pid, ok := readPidFrom(checker.PidFile()); ok {
		if pidAlive(pid) {
			fmt.Printf("[tray] Daemon DANG CHAY (PID %d).\n", pid)
		} else {
			fmt.Printf("[tray] PID file con %d nhung tien trinh da chet (stale).\n", pid)
		}
	} else {
		fmt.Println("[tray] Daemon KHONG chay.")
	}
	if pid, ok := readPidFrom(checker.GotrayPidFile()); ok {
		if pidAlive(pid) {
			fmt.Printf("[tray] App tray Go DANG CHAY (PID %d).\n", pid)
		} else {
			fmt.Printf("[tray] gotray.pid con %d nhung tien trinh da chet (stale) - app tray khong con, chay lai lenh tray de mo lai.\n", pid)
		}
	} else {
		fmt.Println("[tray] App tray Go KHONG chay (khong co gotray.pid). Chay lenh tray de mo app.")
	}
	if logText, err := os.ReadFile(checker.LogFile()); err == nil {
		fmt.Println("[tray] 10 dong log cuoi:")
		lines := strings.Split(strings.TrimSpace(string(logText)), "\n")
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
	} else {
		fmt.Println("[tray] Chua co log file.")
	}
	seen := checker.LoadSeen()
	fmt.Printf("[tray] Da thay %d deal (seen file).\n", len(seen))
	return 0
}

// handleStop dung daemon: SIGTERM (Unix) roi doi 3s, neu con song
// thi SIGKILL; tren Windows dung Kill truc tiep (Go khong ho tro
// SIGTERM tren Windows, con Node thi TerminateProcess nen hanh vi
// tuong duong). Port handleStop ban Bun.
func handleStop() int {
	pid, ok := readPidFrom(checker.PidFile())
	if !ok {
		fmt.Println("[tray] Khong co daemon nao dang chay (khong thay PID file).")
		return 0
	}
	if !pidAlive(pid) {
		fmt.Printf("[tray] PID %d da chet roi, don PID file cu.\n", pid)
		_ = os.Remove(checker.PidFile())
		return 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("[tray] Khong tim thay tien trinh %d: %v\n", pid, err)
		return 1
	}
	if err := signalDaemon(proc); err != nil {
		fmt.Printf("[tray] Khong gui duoc tin hieu dung toi PID %d: %v\n", pid, err)
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
	fmt.Printf("[tray] Da thoat daemon (PID %d). App tray Go (neu dang chay) khong bi anh huong, thoat bang menu Thoat trong tray.\n", pid)
	return 0
}

// formatDealsMessage tao thong bao deal moi (dung chung cho
// check/watch/daemon/tray, giong ban Bun).
func formatDealsMessage(fresh []checker.Deal) string {
	lines := []string{"ZCode co deal moi! Mo app va bam nut nhan trong app.", ""}
	for _, deal := range fresh {
		lines = append(lines, fmt.Sprintf("- [%s] %s (priority %v)", deal.Position, deal.CampaignID, deal.Priority))
		if deal.Title != "" {
			lines = append(lines, "  "+deal.Title)
		}
		if deal.Detail != "" {
			lines = append(lines, "  "+deal.Detail)
		}
	}
	lines = append(lines, "", "Tool nay chi bao, khong tu nhan thay ban.")
	return strings.Join(lines, "\n")
}

func freshDealKeys(deals []checker.Deal) []string {
	keys := make([]string, 0, len(deals))
	for _, deal := range deals {
		keys = append(keys, deal.Key)
	}
	return keys
}
