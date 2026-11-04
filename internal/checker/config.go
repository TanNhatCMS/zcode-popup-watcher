package checker

import (
	"os"
	"path/filepath"
)

// ListConfigFiles chỉ liệt kê tên file, không đọc nội dùng nhạy cảm.
func ListConfigFiles(configDir string) []string {
	names := []string{
		"telemetry-state.json",
		"credentials.json",
		"config.json",
		"onboarding-record.json",
		"data-sync-state.json",
	}
	var found []string
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(configDir, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}

func ReadCredentialKeys(configDir string) []string {
	data, ok := readJSONFile(filepath.Join(configDir, "credentials.json"))
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

// Mask ẩn token: chỉ hiện 8 ký tự đầu, giống bản Bun.
func Mask(value string) string {
	if value == "" {
		return "(không có)"
	}
	if len(value) <= 8 {
		return "(có, đã ẩn)"
	}
	return value[:8] + "..."
}

type ConfigSummary struct {
	ConfigDir      string
	Files          []string
	DeviceMid      string
	HasDeviceMid   bool
	CredentialKeys []string
	HasJWT         bool
}

func DescribeConfig(configDir string) ConfigSummary {
	deviceMid, hasMid := ReadDeviceMid(configDir)
	_, hasJWT := ReadZcodeJWT(configDir)
	// ReadZcodeJWT tra false cả khi không có key lần khi giải mã
	// lỗi; dry-run chỉ cần biết "có hay không" nền chap nhận.
	return ConfigSummary{
		ConfigDir:      configDir,
		Files:          ListConfigFiles(configDir),
		DeviceMid:      deviceMid,
		HasDeviceMid:   hasMid,
		CredentialKeys: ReadCredentialKeys(configDir),
		HasJWT:         hasJWT,
	}
}

// PidFile là file PID của daemon (check/tray trên non-Windows).
func PidFile() string { return filepath.Join(StateDir(), "deal-checker.pid") }

// TriggerFile là file trigger "kiểm tra ngay" cho daemon.
func TriggerFile() string { return filepath.Join(StateDir(), "check-now.trigger") }
