// main.go -- Điểm vào duy nhất của CLI zcode-deal (bản Go).
//
// Build:
//
//	go build -o dist/zcode-deal .                          # Linux/macOS
//	GOOS=windows go build -ldflags "-H windowsgui -s -w" -o dist/zcode-deal.exe .  # Windows (không console)
//
// CLI này chỉ BÁO deal, không tự nhận thay bạn. Xem README.md.
package main

import (
	"fmt"
	"os"
)

const version = "2.0.0"

const helpText = `zcode-deal ` + version + ` - Theo dõi deal ZCode, chỉ báo, không tự nhận.

Cách dùng:
  zcode-deal <lệnh> [tùy chọn]
  zcode-deal
      Không tham số: tự mở tray (tương đương lệnh tray).

Lệnh:
  check [--once] [--interval GIÂY] [--config-dir DIR] [--open] [--dry-run] [--self-test]
      Kiểm tra deal qua API marketing/touch. Mặc định poll liên tục
      mỗi 600 giây; --once chỉ kiểm tra 1 lần rồi thoát.
  watch [tùy chọn như check]
      Giống check (poll liên tục).
  tray [--interval GIÂY] [--config-dir DIR] [--open]
      Trên Windows: mở app tray Go (icon + menu native).
      Ngoài Windows: chạy daemon tách rời, lệnh thoát ngay, tắt được
      terminal.
  status
      Daemon / app tray Go có đang chạy không? Xem vài dòng log cuối.
  stop
      Dùng daemon (app tray Go thoát bằng menu Thoát trong tray).
  reminder [--from HH:MM] [--to HH:MM] [--open]
      Nhắc theo khung giờ deal đêm (mặc định 17:55-22:10 giờ VN),
      1 lần/ngày, không gọi API.
  reminder --test
      Báo ngay để xem thử, không đợi khung giờ.
  self-test
      Test parse snapshot mẫu, không gọi API, không cần token.
  help
      Hiện bảng này.
  --version, -v
      In version.

Ví dụ:
  zcode-deal check --once
  zcode-deal tray --open
  zcode-deal status
  zcode-deal stop
  zcode-deal reminder --test

State nằm ở ~/.zcode/mod/. Token chỉ đọc từ ~/.zcode/v2 trên cùng
máy/cùng user với ZCode, không bao giờ in ra hay ghi ra file.
`

// parseArgs port hàm parseArgs bản Bun: --key value hoặc --flag.
func parseArgs(argv []string) (map[string]string, map[string]bool) {
	args := map[string]string{}
	bools := map[string]bool{}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if len(arg) < 3 || arg[:2] != "--" {
			continue
		}
		key := arg[2:]
		if i+1 < len(argv) && len(argv[i+1]) >= 2 && argv[i+1][:2] != "--" {
			args[key] = argv[i+1]
			i++
		} else {
			bools[key] = true
		}
	}
	return args, bools
}

func trayOptionsFromArgs(args map[string]string, bools map[string]bool) TrayOptions {
	intervalSec := 600
	if v, ok := args["interval"]; ok {
		if n, err := parseInt(v); err == nil && n > 0 {
			intervalSec = n
		}
	}
	return TrayOptions{
		IntervalSec: intervalSec,
		ConfigDir:   args["config-dir"],
		AutoOpen:    bools["open"],
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 {
		return runTray(TrayOptions{IntervalSec: 600})
	}
	command := argv[0]
	rest := argv[1:]
	args, bools := parseArgs(rest)

	switch command {
	case "help", "--help", "-h":
		fmt.Print(helpText)
		return 0
	case "--version", "-v", "version":
		fmt.Printf("zcode-deal %s\n", version)
		return 0
	case "check", "watch":
		return runCheck(args, bools)
	case "tray":
		return runTray(trayOptionsFromArgs(args, bools))
	case "status":
		return handleStatus()
	case "stop":
		return handleStop()
	case "reminder":
		return runReminder(args, bools)
	case "self-test":
		return runSelfTest()
	case "__tray-child":
		return runTrayChild(trayOptionsFromArgs(args, bools))
	default:
		fmt.Printf("[cli] Lệnh không biết: %s\n\n", command)
		fmt.Print(helpText)
		return 2
	}
}
