package checker

import (
	"os/user"
	"runtime"
	"strings"
)

func runtimeGOOS() string {
	return runtime.GOOS
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
