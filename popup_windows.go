//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Popup Win32 thuần (không thêm dependency): cửa sổ nhỏ luôn nổi trên
// cùng, hiện deal + nút "Nhận deal". Khi bấm nút, kết quả trả về qua
// channel và window tự đóng. Người dùng phải chủ động bấm — tool không
// tự claim ngầm (xem README).

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	procSetThreadDpiCtx     = user32.NewProc("SetThreadDpiAwarenessContext")
	procCreateFontW         = gdi32.NewProc("CreateFontW")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsCaption     = 0x00C00000
	wsSysmenu     = 0x00080000
	wsMinimizebox = 0x00020000
	wsExTopmost   = 0x00000008

	bsPushButton    = 0x00000000
	bsDefPushButton = 0x00000001

	wmCommand = 0x0111
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmSetFont = 0x0030

	swShow = 1

	smCXScreen = 0
	smCYScreen = 1

	idcArrow = 32512

	mbOK            = 0x00000000
	mbIconInfo      = 0x00000040
	mbIconError     = 0x00000010
	mbSetForeground = 0x00010000
	mbTopmost       = 0x00040000

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4.
	dpiAwareV2 = ^uintptr(3)

	// Control IDs của WM_COMMAND (LOWORD wParam).
	idBtnClaim = 1001
	idBtnLater = 1002
	idBtnOpen  = 1003
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

type msgW struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// popupOpenCh chỉ cho 1 popup tại 1 thời điểm: deal mới đến khi popup
// đang mở sẽ được ghi vào pending-claim.json (đã làm ở caller) và bỏ
// qua popup để không chồng cửa sổ.
var popupOpenMu sync.Mutex

func utf16Ptr(s string) *uint16 {
	ptr, err := windows.UTF16PtrFromString(s)
	if err != nil {
		// Chuỗi có ký tự NUL là lỗi lập trình; rơi về chuỗi trống
		// thay vì panic trong luồng UI.
		empty, _ := windows.UTF16PtrFromString("")
		return empty
	}
	return ptr
}

func sysDPI() int {
	if err := procGetDpiForSystem.Find(); err == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
			return int(dpi)
		}
	}
	return 96
}

func scaleValue(value, dpi int) int {
	return value * dpi / 96
}

// showDealPopup mở popup nhận deal và BLOCK cho đến khi người dùng
// chọn: "claim" (Nhận deal) / "later" (Để sau hoặc đóng X) / "open"
// (Mở ZCode) / "none" (popup không hiện được). Chỉ chạy trên Windows;
// goroutine gọi phải sẵn sàng bị chặn vài phút.
func showDealPopup(title, detail, buttonLabel string) string {
	// Nếu đã có popup mở thì không mở thêm (caller đã ghi pending file).
	if !popupOpenMu.TryLock() {
		return "busy"
	}
	defer popupOpenMu.Unlock()

	// Win32 window gắn chặt với thread tạo nó — khóa goroutine vào
	// một OS thread riêng cho suốt đời của cửa sổ.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	resultCh := make(chan string, 1)
	if err := runPopupWindow(title, detail, buttonLabel, resultCh); err != nil {
		resultCh <- "none"
	}
	return <-resultCh
}

// alertBox hiện MessageBox OK đơn giản (kết quả claim thành công/lỗi).
func alertBox(title, text string, isError bool) {
	flags := uintptr(mbOK | mbSetForeground | mbTopmost | mbIconInfo)
	if isError {
		flags = uintptr(mbOK | mbSetForeground | mbTopmost | mbIconError)
	}
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		flags,
	)
}

func runPopupWindow(title, detail, buttonLabel string, resultCh chan string) error {
	if err := procRegisterClassExW.Find(); err != nil {
		return err
	}
	// Per-Monitor v2 để không bị blur trên màn hình DPI cao; hệ cũ
	// không có hàm này thì bỏ qua (Call trên proc chưa Find chỉ trả err).
	oldCtx, _, _ := procSetThreadDpiCtx.Call(dpiAwareV2)
	defer procSetThreadDpiCtx.Call(oldCtx)

	dpi := sysDPI()
	className := utf16Ptr("ZCodeDealPopupWnd")

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wndClass := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   syscall.NewCallback(popupWndProc(resultCh)),
		HInstance:     syscall.Handle(hInstance),
		HCursor:       syscall.Handle(hCursor),
		HbrBackground: syscall.Handle(6), // COLOR_WINDOW + 1
		LpszClassName: className,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass))); ret == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}

	const (
		winW = 460
		winH = 240
	)
	W := scaleValue(winW, dpi)
	H := scaleValue(winH, dpi)
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
	x := (int(screenW) - W) / 2
	y := (int(screenH) - H) / 3
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	style := uintptr(wsCaption | wsSysmenu | wsMinimizebox)
	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(wsExTopmost),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("ZCode Deal"))),
		style,
		uintptr(int32(x)), uintptr(int32(y)), uintptr(uint32(W)), uintptr(uint32(H)),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}

	fontTitle := createUIFont(dpi, 700)
	fontBody := createUIFont(dpi, 400)
	defer procDeleteObject.Call(fontTitle)
	defer procDeleteObject.Call(fontBody)

	claimLabel := buttonLabel
	if claimLabel == "" {
		claimLabel = "Nhận deal"
	}

	margins := scaleValue(24, dpi)
	titleH := scaleValue(30, dpi)
	descY := scaleValue(64, dpi)
	descH := scaleValue(96, dpi)
	btnY := scaleValue(178, dpi)
	btnH := scaleValue(36, dpi)

	hTitle := createControl("STATIC", title, 0, hInstance, margins, scaleValue(20, dpi), W-2*margins, titleH, hwnd)
	procSendMessageW.Call(hTitle, uintptr(wmSetFont), fontTitle, 1)
	hDesc := createControl("STATIC", detail, 0, hInstance, margins, descY, W-2*margins, descH, hwnd)
	procSendMessageW.Call(hDesc, uintptr(wmSetFont), fontBody, 1)

	btnW := scaleValue(130, dpi)
	gap := scaleValue(12, dpi)
	hClaim := createControl("BUTTON", claimLabel, bsDefPushButton|bsPushButton, hInstance, margins, btnY, btnW, btnH, hwnd, idBtnClaim)
	procSendMessageW.Call(hClaim, uintptr(wmSetFont), fontBody, 1)
	hOpen := createControl("BUTTON", "Mở ZCode", bsPushButton, hInstance, margins+btnW+gap, btnY, btnW, btnH, hwnd, idBtnOpen)
	procSendMessageW.Call(hOpen, uintptr(wmSetFont), fontBody, 1)
	hLater := createControl("BUTTON", "Để sau", bsPushButton, hInstance, margins+2*(btnW+gap), btnY, btnW, btnH, hwnd, idBtnLater)
	procSendMessageW.Call(hLater, uintptr(wmSetFont), fontBody, 1)

	_ = hTitle
	_ = hDesc
	_ = hClaim
	_ = hOpen
	_ = hLater

	procShowWindow.Call(hwnd, uintptr(swShow))
	procSetForegroundWindow.Call(hwnd)

	// Bơm message loop cho đến khi WM_QUIT (wndproc gửi khi đóng).
	var m msgW
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		// GetMessage trả -1 lỗi, 0 khi WM_QUIT.
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func popupWndProc(resultCh chan string) func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	return func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case wmCommand:
			switch wParam & 0xFFFF {
			case idBtnClaim:
				select {
				case resultCh <- "claim":
				default:
				}
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			case idBtnOpen:
				select {
				case resultCh <- "open":
				default:
				}
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			case idBtnLater:
				select {
				case resultCh <- "later":
				default:
				}
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			}
		case wmClose:
			// Đóng bằng X = "Để sau" (app gốc report cancel khi đóng dialog).
			select {
			case resultCh <- "later":
			default:
			}
			procDestroyWindow.Call(uintptr(hwnd))
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
		ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
		return ret
	}
}

func createControl(class, text string, style uintptr, hInstance uintptr, x, y, w, h int, parent uintptr, id ...uintptr) uintptr {
	ctrlID := uintptr(0)
	if len(id) > 0 {
		ctrlID = id[0]
	}
	className := utf16Ptr(class)
	windowText := utf16Ptr(text)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowText)),
		uintptr(wsChild|wsVisible|style),
		uintptr(int32(x)), uintptr(int32(y)), uintptr(uint32(w)), uintptr(uint32(h)),
		parent, ctrlID, hInstance, 0,
	)
	return hwnd
}

func createUIFont(dpi, weight int) uintptr {
	height := -scaleValue(15, dpi) // chiều cao ký tự âm = em height
	faceName := utf16Ptr("Segoe UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(int32(height)), 0, 0, 0,
		uintptr(uint32(weight)), 0, 0, 0,
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(faceName)),
	)
	return font
}
