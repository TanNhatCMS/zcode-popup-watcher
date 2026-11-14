//go:build !windows

package main

// Trên Linux/macOS không có popup GUI gốc (cần GTK/AppIndicator riêng,
// giống hạn chế của tray). Deal vẫn được ghi vào pending-claim.json và
// người dùng nhận bằng lệnh `zcode-deal claim`; báo bằng toast hệ thống.

// showDealPopup trả "none" — caller hiểu là không có popup, không chặn.
func showDealPopup(info popupDealInfo) string {
	return "none"
}

// alertBox không có GUI tương đương trên non-Windows; bỏ qua (đã có
// toast + log + console).
func alertBox(title, text string, isError bool) {}
