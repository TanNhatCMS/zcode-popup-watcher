package checker

import (
	"os/user"
	"runtime"
	"strings"
)

// nodePlatform doi GOOS cua Go sang ten platform cua Node
// (os.platform()) ma ZCode dung trong fallback secret. Dac biet:
// Go goi la "windows", Node goi la "win32" — dung "windows" thi
// secret sai, giai ma that bai (da gap that tren Windows cua user
// ngay 2026-11-04: tray bao LOI auth du token ton tai).
// "darwin"/"linux" thi 2 ben giong nhau.
func nodePlatform(goos string) string {
	if goos == "windows" {
		return "win32"
	}
	return goos
}

func runtimeGOOS() string {
	return nodePlatform(runtime.GOOS)
}

// currentUsername tra ve username khong kem domain (neu co dang
// DOMAIN\user thi lay phan sau dau \) cho khop voi fallback
// secret cua ZCode (Node os.userInfo().username).
func currentUsername() string {
	u, err := user.Current()
	if err != nil || u == nil || u.Username == "" {
		return "unknown"
	}
	name := u.Username
	if idx := strings.LastIndex(name, `\`); idx >= 0 {
		name = name[idx+1:]
	}
	if name == "" {
		return "unknown"
	}
	return name
}
