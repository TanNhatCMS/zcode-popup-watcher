//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// pidAlive tren Windows: OpenProcess thanh cong nghia la tien
// trinh con ton tai (Go khong ho tro kill(pid, 0) tren Windows).
func pidAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}

// signalDaemon tren Windows: Go khong ho tro SIGTERM, dung Kill
// (TerminateProcess) luon - tuong duong hanh vi Node process.kill
// SIGTERM tren Windows truoc day.
func signalDaemon(proc *os.Process) error {
	return proc.Kill()
}
