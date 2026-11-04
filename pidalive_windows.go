//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// pidAlive trên Windows: OpenProcess thành công nghia là tiến
// trinh còn tồn tại (Go không ho tro kill(pid, 0) trên Windows).
func pidAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}

// signalDaemon trên Windows: Go không ho tro SIGTERM, dùng Kill
// (TerminateProcess) luon - tương đương hanh vi Node process.kill
// SIGTERM trên Windows trước đây.
func signalDaemon(proc *os.Process) error {
	return proc.Kill()
}
