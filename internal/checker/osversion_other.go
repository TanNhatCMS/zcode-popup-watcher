//go:build !windows

package checker

// osVersionString trên non-Windows: không đọc được phiên bản OS bằng
// stdlib mà không thêm dependency — header X-Os-Version là tùy chọn
// (app gốc cũng chỉ log), trả rỗng để header bị bỏ.
func osVersionString() string {
	return ""
}
