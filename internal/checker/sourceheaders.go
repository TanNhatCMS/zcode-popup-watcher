// sourceheaders.go — mirror 1:1 của buildZCodeSourceHeadersFromContext
// (packages/shared/src/zcode-source-headers.ts trong ZCode gốc): ApiClient
// của app gốc tự inject bộ header này vào MỌI request tới zcode endpoint
// (kể cả billing — thiếu là gateway drop connection → EOF, hoặc security
// check từ chối 3007). Tool phải gửi đúng bộ này.
package checker

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

const (
	zcodeEndpointOrigin = "https://zcode.z.ai"
	osCategoryWindows   = "windows"
	sourceTitle         = "electron"
	releaseChannel      = "production"
)

// goArchToNodeArch: Go gọi amd64, Node gọi x64 — header X-Platform dùng
// tên của Node (process.arch).
func goArchToNodeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	}
	return goarch
}

// clientTimezoneName trả tên timezone hiện tại (ASCII, giống
// Intl.DateTimeFormat().resolvedOptions().timeZone của Node).
func clientTimezoneName() string {
	name, _ := time.Now().Zone()
	name = strings.TrimSpace(name)
	if name == "" || !isPrintableASCII(name) {
		return "unknown"
	}
	return name
}

func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// SourceHeaderOptions là đầu vào của buildZCodeSourceHeaders.
type SourceHeaderOptions struct {
	AppVersion     string // vd "3.17.2"
	DeviceMid      string // từ telemetry-state.json
	ClientLanguage string // vd "en-US"
}

// buildZCodeSourceHeaders dựng bộ 11 header source mà ApiClient của app
// gốc inject vào mọi request tới zcode endpoint. DeviceMid/language rỗng
// sẽ bị bỏ (app gốc cũng bỏ khi thiếu — không giả mạo giá trị).
func buildZCodeSourceHeaders(opts SourceHeaderOptions) map[string]string {
	platform := nodePlatform(runtime.GOOS)
	arch := goArchToNodeArch(runtime.GOARCH)
	appVersion := normalizeHeaderValue(opts.AppVersion)
	clientLanguage := normalizeHeaderValue(opts.ClientLanguage)
	if clientLanguage == "" {
		clientLanguage = "unknown"
	}
	clientTimezone := normalizeHeaderValue(clientTimezoneName())
	if clientTimezone == "" {
		clientTimezone = "unknown"
	}
	osVersion := normalizeHeaderValue(osVersionString())
	deviceMid := normalizeHeaderValue(opts.DeviceMid)

	headers := map[string]string{
		"User-Agent":          fmt.Sprintf("ZCode/%s", appVersionOrUnknown(appVersion)),
		"HTTP-Referer":        zcodeEndpointOrigin,
		"X-Title":             fmt.Sprintf("Z Code@%s", sourceTitle),
		"X-Client-Language":   clientLanguage,
		"X-Client-Timezone":   clientTimezone,
		"X-Os-Category":       osCategoryForPlatform(platform),
		"X-Release-Channel":   releaseChannel,
		"X-ZCode-App-Version": appVersion,
		"X-Platform":          fmt.Sprintf("%s-%s", platform, arch),
	}
	if osVersion != "" {
		headers["X-Os-Version"] = osVersion
	}
	if deviceMid != "" {
		headers["X-Device-Mid"] = deviceMid
	}
	return headers
}

// osVersionString do từng platform implement (osversion_*.go).
func normalizeHeaderValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !isPrintableASCII(trimmed) {
		return ""
	}
	return trimmed
}

func appVersionOrUnknown(appVersion string) string {
	if appVersion == "" {
		return "unknown"
	}
	return appVersion
}

// osCategoryForPlatform khớp normalizeOsCategory của app gốc.
func osCategoryForPlatform(platform string) string {
	switch platform {
	case "darwin":
		return "macos"
	case "windows", "win32":
		return "windows"
	case "linux":
		return "linux"
	}
	return platform
}
