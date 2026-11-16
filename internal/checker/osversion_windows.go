//go:build windows

package checker

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// osVersionString trả chuỗi phiên bản Windows kiểu Node os.version()
// (vd "10.0.26300") qua RtlGetVersion; lỗi → "Windows".
func osVersionString() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
