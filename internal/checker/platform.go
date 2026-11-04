package checker

import (
	"os/user"
	"runtime"
	"strings"
)

// nodePlatform đợi GOOS của Go sáng tên platform của Node
// (os.platform()) mà ZCode dùng trong fallback secret. Dac biết:
// Go gọi là "windows", Node gọi là "win32" — dùng "windows" thì
// secret sai, giải mã that bai (đã gap that trên Windows của user
// ngay 2026-11-04: tray báo LỖI auth du token tồn tại).
// "darwin"/"linux" thì 2 bên giống nhau.
func nodePlatform(goos string) string {
	if goos == "windows" {
		return "win32"
	}
	return goos
}

func runtimeGOOS() string {
	return nodePlatform(runtime.GOOS)
}

// currentUsername trả về username không kèm domain (nếu có đang
// DOMAIN\user thì lấy phần sau dấu \) cho khop với fallback
// secret của ZCode (Node os.userInfo().username).
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
