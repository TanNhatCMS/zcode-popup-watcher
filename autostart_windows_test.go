//go:build windows

package main

import "testing"

func TestSetAutostartRoundTrip(t *testing.T) {
	// Bật → enabled → tắt → disabled; test tự dọn để không để lại
	// giá trị trong HKCU Run của người dùng.
	if err := setAutostart(true); err != nil {
		t.Fatalf("setAutostart(true): %v", err)
	}
	if !autostartEnabled() {
		t.Fatal("đã bật nhưng autostartEnabled() trả false")
	}
	if err := setAutostart(false); err != nil {
		t.Fatalf("setAutostart(false): %v", err)
	}
	if autostartEnabled() {
		t.Fatal("đã tắt nhưng autostartEnabled() vẫn trả true")
	}
}
