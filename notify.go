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

// logLine in ra console + ghi vao log chung (giong ban Bun).
func logLine(line string) {
	fmt.Println(line)
	checker.AppendLog(line)
}

// Notify hien thong bao he thong (beeep: toast tren Windows,
// notification tren macOS/Linux). Neu khong bao duoc thi in ra
// console + bip, khong im lang mat thong bao (giong ban Bun).
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

// openApp mo 1 file thuc thi/app (ZCode). Tren Linux chay truc
// tiep file, khong qua xdg-open (xdg-open khong mo duoc binary).
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

// openFile mo 1 file tai lieu (log) bang app mac dinh.
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

// OpenZcode mo app ZCode neu that su tim thay file thuc thi,
// khong doan mo. Tra ve chuoi mo ta ket qua (giong ban Bun).
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
			return fmt.Sprintf("Da goi mo ZCode: %s", target)
		}
	}
	return "Khong tim thay ZCode tren may nay, ban mo app thu cong nhe."
}
