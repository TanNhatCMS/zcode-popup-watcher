package checker

import (
	"os"
	"path/filepath"
)

// ListConfigFiles chi liet ke ten file, khong doc noi dung nhay cam.
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

// Mask an token: chi hien 8 ky tu dau, giong ban Bun.
func Mask(value string) string {
	if value == "" {
		return "(khong co)"
	}
	if len(value) <= 8 {
		return "(co, da an)"
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
	// ReadZcodeJWT tra false ca khi khong co key lan khi giai ma
	// loi; dry-run chi can biet "co hay khong" nen chap nhan.
	return ConfigSummary{
		ConfigDir:      configDir,
		Files:          ListConfigFiles(configDir),
		DeviceMid:      deviceMid,
		HasDeviceMid:   hasMid,
		CredentialKeys: ReadCredentialKeys(configDir),
		HasJWT:         hasJWT,
	}
}

// PidFile la file PID cua daemon (check/tray tren non-Windows).
func PidFile() string { return filepath.Join(StateDir(), "deal-checker.pid") }

// TriggerFile la file trigger "kiem tra ngay" cho daemon.
func TriggerFile() string { return filepath.Join(StateDir(), "check-now.trigger") }
