package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/gen2brain/beeep"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

// logLine in ra console + ghi vào log chung (giống bản Bun).
func logLine(line string) {
	fmt.Println(line)
	checker.AppendLog(line)
}

// Notify hiện thông báo hệ thống (beeep: toast trên Windows,
// notification trên macOS/Linux). Nếu không báo được thì in ra
// console + bip, không im lang mat thông báo (giống bản Bun).
func Notify(title, message string) {
	short := message
	if len([]rune(short)) > 900 {
		short = string([]rune(short)[:900]) + "..."
	}
	if err := beeep.Notify(title, short, ""); err != nil {
		fmt.Printf("\n=== %s ===\n%s\n", title, message)
		fmt.Print("\x07")
	}
}

// openApp mở 1 file thuc thì/app (ZCode). Trên Linux chạy trực
// tiếp file, không qua xdg-open (xdg-open không mở được binary).
func openApp(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command(target)
	}
	return cmd.Start()
}

// openFile mở 1 file tai lieu (log) bằng app mặc định.
func openFile(target string) error {
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

// OpenZcode mở app ZCode nếu that su tìm thấy file thuc thì,
// không doan mở. Trả về chuoi mô tả ket qua (giống bản Bun).
func OpenZcode() string {
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
		if _, err := os.Stat(target); err != nil {
			continue
		}
		if err := openApp(target); err == nil {
			return fmt.Sprintf("Đã gọi mở ZCode: %s", target)
		}
	}
	return "Không tìm thấy ZCode trên máy này, bạn mở app thủ công nhé."
}
