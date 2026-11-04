// main.go -- Diem vao duy nhat cua CLI zcode-deal (ban Go).
//
// Build:
//
//	go build -o dist/zcode-deal .                          # Linux/macOS
//	GOOS=windows go build -ldflags "-H windowsgui -s -w" -o dist/zcode-deal.exe .  # Windows (khong console)
//
// CLI nay chi BAO deal, khong tu nhan thay ban. Xem README.md.
package main

import (
	"fmt"
	"os"
)

const version = "2.0.0"

const helpText = `zcode-deal ` + version + ` - Theo doi deal ZCode, chi bao, khong tu nhan.

Cach dung:
  zcode-deal <lenh> [tuy chon]
  zcode-deal
      Khong tham so: tu mo tray (tuong duong lenh tray).

Lenh:
  check [--once] [--interval GIAY] [--config-dir DIR] [--open] [--dry-run] [--self-test]
      Kiem tra deal qua API marketing/touch. Mac dinh poll lien tuc
      moi 600 giay; --once chi kiem tra 1 lan roi thoat.
  watch [tuy chon nhu check]
      Giong check (poll lien tuc).
  tray [--interval GIAY] [--config-dir DIR] [--open]
      Tren Windows: mo app tray Go (icon + menu native).
      Ngoai Windows: chay daemon tach roi, lenh thoat ngay, tat duoc
      terminal.
  status
      Daemon / app tray Go co dang chay khong? Xem vai dong log cuoi.
  stop
      Dung daemon (app tray Go thoat bang menu Thoat trong tray).
  reminder [--from HH:MM] [--to HH:MM] [--open]
      Nhac theo khung gio deal dem (mac dinh 17:55-22:10 gio VN),
      1 lan/ngay, khong goi API.
  reminder --test
      Bao ngay de xem thu, khong doi khung gio.
  self-test
      Test parse snapshot mau, khong goi API, khong can token.
  help
      Hien bang nay.
  --version, -v
      In version.

Vi du:
  zcode-deal check --once
  zcode-deal tray --open
  zcode-deal status
  zcode-deal stop
  zcode-deal reminder --test

State nam o ~/.zcode/mod/. Token chi doc tu ~/.zcode/v2 tren cung
may/cung user voi ZCode, khong bao gio in ra hay ghi ra file.
`

// parseArgs port ham parseArgs ban Bun: --key value hoac --flag.
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
		fmt.Printf("[cli] Lenh khong biet: %s\n\n", command)
		fmt.Print(helpText)
		return 2
	}
}
