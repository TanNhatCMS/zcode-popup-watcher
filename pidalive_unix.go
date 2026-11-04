//go:build !windows

package main

import (
	"os"
	"syscall"
)

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// signalDaemon gui SIGTERM cho daemon (chi dung tren Unix).
func signalDaemon(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}
