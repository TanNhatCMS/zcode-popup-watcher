//go:build windows

package main

import (
	"os"
	"strconv"

	"golang.org/x/sys/windows/registry"
)

// Autostart qua HKCU\Software\Microsoft\Windows\CurrentVersion\Run —
// cơ chế chuẩn Windows cho "khởi động cùng hệ thống" theo user,
// không cần quyền admin. Lệnh ghi là "<đường dẫn exe>" tray để mở
// thẳng app tray lúc đăng nhập.

const (
	autostartRunKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValueName = "zcode-deal"
	autostartTooltip   = "Tự mở zcode-deal khi đăng nhập Windows"
	autostartMenuTitle = "Khởi động cùng hệ thống"
)

// autostartEnabled đọc registry: có giá trị zcode-deal trong Run key.
func autostartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(autostartValueName)
	return err == nil && value != ""
}

// setAutostart bật/tắt auto-start. Bật: ghi lệnh chạy exe hiện tại;
// tắt: xoá giá trị (không có key/value coi như đã tắt, không lỗi).
func setAutostart(enable bool) error {
	if enable {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		// Quote đường dẫn để an toàn với khoảng trắng trong profile.
		return key.SetStringValue(autostartValueName, strconv.Quote(exe)+" tray")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		return nil // key không tồn tại = chưa bật, coi như thành công
	}
	defer key.Close()
	if err := key.DeleteValue(autostartValueName); err != nil {
		return nil // value không tồn tại = đã tắt rồi
	}
	return nil
}
