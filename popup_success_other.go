//go:build !windows

package main

// Non-Windows không có popup GUI: kết quả claim in ra console/log ở
// caller (claimDeal đã log), stub trả "none" để không chặn.

// showClaimSuccessPopup trả "none" — caller hiểu là không có GUI.
func showClaimSuccessPopup(info ticketInfo) string {
	return "none"
}
