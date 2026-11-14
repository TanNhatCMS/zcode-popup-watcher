//go:build windows

package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Popup Win32 thuần (không thêm dependency): vẽ deal theo UI app ZCode
// — popup khi CÓ deal theo banner tối màu (ảnh 1: header plan, số token
// lớn, nút Claim), popup SAU KHI nhận là tấm vé trắng (ảnh 2, xem
// popup_success_windows.go). Người dùng phải chủ động bấm — tool không
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
	procLoadIconW           = user32.NewProc("LoadIconW")
	procGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	procSetThreadDpiCtx     = user32.NewProc("SetThreadDpiAwarenessContext")
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procDrawTextW           = user32.NewProc("DrawTextW")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procFillRect            = user32.NewProc("FillRect")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procAdjustWindowRect    = user32.NewProc("AdjustWindowRect")
	procMoveToEx            = user32.NewProc("MoveToEx")
	procLineTo              = user32.NewProc("LineTo")
	procInvalidateRect      = user32.NewProc("InvalidateRect")
	procIsWindow            = user32.NewProc("IsWindow")
	procSetBkMode           = gdi32.NewProc("SetBkMode")
	procSetTextColor        = gdi32.NewProc("SetTextColor")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	procCreatePen           = gdi32.NewProc("CreatePen")
	procGetStockObject      = gdi32.NewProc("GetStockObject")
	procRoundRect           = gdi32.NewProc("RoundRect")
	procEllipse             = gdi32.NewProc("Ellipse")
	procSetPixel            = gdi32.NewProc("SetPixel")
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

	wmCommand    = 0x0111
	wmClose      = 0x0010
	wmDestroy    = 0x0002
	wmPaint      = 0x000F
	wmEraseBkgnd = 0x0014
	wmSetFont    = 0x0030
	wmLButtonUp  = 0x0202

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

	// DrawTextW flags.
	dtCalcrect  = 0x00000400 // đo chiều cao text, không vẽ
	dtLeft      = 0x00000000
	dtCenter    = 0x00000001
	dtRight     = 0x00000002
	dtVCenter   = 0x00000004
	dtSingle    = 0x00000020
	dtWordBreak = 0x00000010
	dtNoPrefix  = 0x00000800

	// ComboBox: dropdownlist chọn tài khoản nhận deal.
	cbsDropdownList = 0x00000002
	cbAddString     = 0x00000143
	cbSetCurSel     = 0x0000014E
	cbGetCurSel     = 0x00000147

	// Button checkbox + EDIT.
	wsBorder   = 0x00800000
	bstChecked = 1

	bkTransparent = 1

	psSolid = 0 // CreatePen style nét liền

	whiteBrushStockConst = 0 // GetStockObject(WHITE_BRUSH)
	nullPenStockConst    = 8 // GetStockObject(NULL_PEN) — chú ý: 5 là NULL_BRUSH, dùng nhầm làm RoundRect fail im lặng!

	// Control IDs của WM_COMMAND (LOWORD wParam).
	idBtnClaim = 1001
	idBtnLater = 1002
	idBtnOpen  = 1003

	idBtnGotIt = 2001
)

// Màu lấy từ UI app ZCode (ảnh 1/2): nền navy đậm, accent xanh dương,
// vé trắng. COLORREF = 0x00BBGGRR. (colorRef là hàm nên khai báo var,
// không thể const.)
var (
	colBgNavy    = colorRef(11, 16, 32)
	colTextWhite = colorRef(245, 248, 255)
	colTextGrey  = colorRef(163, 176, 200)
	colAccent    = colorRef(88, 148, 255)
	colTextDark  = colorRef(16, 24, 40)
	colTextMid   = colorRef(102, 112, 133)
	colSepGrey   = colorRef(226, 232, 240)
	colStarDim   = colorRef(64, 76, 108)
)

func colorRef(r, g, b uint8) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}

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

type rectI struct {
	Left, Top, Right, Bottom int32
}

type paintStructW struct {
	HDC         syscall.Handle
	FErase      int32
	RcPaint     rectI
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

// Popup holder TỰ PHỤC HỒI, tách riêng theo LOẠI popup: mỗi loại
// (deal / vé / cài đặt / thông tin) có holder riêng → mở được ĐỒNG
// THỜI nhiều popup khác nhau; cùng loại thì vẫn chỉ 1 instance.
// Holder theo dõi HWND: cửa sổ đã chết mà chưa nhả (lộ trình thoát
// lỗi) thì lần acquire kế tiếp chiếm quyền lại thay vì chặn oan mãi.
type popupKind int

const (
	popupKindDeal popupKind = iota
	popupKindTicket
	popupKindSettings
	popupKindInfo
)

type popupHolder struct {
	hwnd     uintptr
	creating bool
}

var (
	popupMu        sync.Mutex
	popupHolders   = map[popupKind]*popupHolder{}
)

func isWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	res, _, _ := procIsWindow.Call(hwnd)
	return res != 0
}

// popupTryAcquire chiếm quyền mở 1 loại popup; false = loại đó đang
// có cửa sổ sống (không mở 2 cửa sổ cùng loại).
func popupTryAcquire(kind popupKind) bool {
	popupMu.Lock()
	defer popupMu.Unlock()
	h := popupHolders[kind]
	if h != nil {
		if h.creating {
			return false
		}
		if isWindow(h.hwnd) {
			return false
		}
		logLine(fmt.Sprintf("[popup] Popup cũ (hwnd %x) đã chết nhưng chưa nhả lock — chiếm quyền lại.", h.hwnd))
	}
	popupHolders[kind] = &popupHolder{creating: true}
	return true
}

// popupSetHwnd báo HWND của popup vừa tạo xong.
func popupSetHwnd(kind popupKind, hwnd uintptr) {
	popupMu.Lock()
	defer popupMu.Unlock()
	if h := popupHolders[kind]; h != nil {
		h.hwnd = hwnd
		h.creating = false
	}
}

// popupRelease nhả quyền của 1 loại popup khi popup đóng/bị hủy.
func popupRelease(kind popupKind) {
	popupMu.Lock()
	defer popupMu.Unlock()
	delete(popupHolders, kind)
}

func utf16Ptr(s string) *uint16 {
	ptr, err := windows.UTF16PtrFromString(s)
	if err != nil {
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

// alertBox hiện MessageBox OK đơn giản (lỗi claim).
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

// ---- helpers vẽ text/shape dùng chung cho deal popup + vé ----

type uiFont struct {
	handle uintptr
}

func newUIFont(dpi int, weight, height int) uiFont {
	emHeight := -scaleValue(-height, dpi)
	faceName := utf16Ptr("Segoe UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(int32(emHeight)), 0, 0, 0,
		uintptr(uint32(weight)), 0, 0, 0,
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(faceName)),
	)
	return uiFont{handle: font}
}

func (f uiFont) release() {
	if f.handle != 0 {
		procDeleteObject.Call(f.handle)
	}
}

// drawTextBlock vẽ 1 khối text bằng DrawTextW (transparent, word-break).
func drawTextBlock(hdc, font uintptr, text string, rect rectI, color uint32, flags uintptr) {
	procSetBkMode.Call(hdc, uintptr(bkTransparent))
	procSetTextColor.Call(hdc, uintptr(color))
	old, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, old)
	textUTF16 := utf16Ptr(text)
	procDrawTextW.Call(
		hdc,
		uintptr(unsafe.Pointer(textUTF16)), uintptr(len([]rune(text))+1),
		uintptr(unsafe.Pointer(&rect)),
		uintptr(dtNoPrefix)|flags,
	)
}

// fillRectColor tô 1 vùng bằng màu đặc (tạo brush rồi xoá ngay).
// Thứ tự tham số FillRect: (hDC, *RECT, hBRUSH).
func fillRectColor(hdc uintptr, rect rectI, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	if brush != 0 {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), brush)
		procDeleteObject.Call(brush)
	}
}

// ---- Popup khi CÓ deal (ảnh 1: banner tối màu + nút Claim) ----

// showDealPopup mở popup nhận deal và BLOCK cho đến khi người dùng
// chọn. Kết quả kèm chỉ số tài khoản đã chọn trong combobox (0 = mặc
// định): "claim" / "later" / "open" / "none" / "busy".
func showDealPopup(info popupDealInfo) popupChoice {
	if !popupTryAcquire() {
		return popupChoice{Choice: "busy"}
	}
	defer popupRelease()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	resultCh := make(chan popupChoice, 1)
	if err := runPopupWindow(info, resultCh); err != nil {
		resultCh <- popupChoice{Choice: "none"}
	}
	return <-resultCh
}

// drawLine là 1 khối text trong bố cục popup, vẽ trong WM_PAINT.
type drawLine struct {
	text  string
	font  uintptr
	color uint32
	rect  rectI
	flags uintptr // DrawTextW flags bổ sung (DT_CENTER cho dòng giữa, …)
}

type popupState struct {
	resultCh  chan popupChoice
	lines     []drawLine
	accounts  []accountChoice
	comboHwnd uintptr // combobox chọn tài khoản (0 = không có, 1 lựa chọn)
}

func runPopupWindow(info popupDealInfo, resultCh chan popupChoice) error {
	if err := procRegisterClassExW.Find(); err != nil {
		return err
	}
	oldCtx, _, _ := procSetThreadDpiCtx.Call(dpiAwareV2)
	defer procSetThreadDpiCtx.Call(oldCtx)

	dpi := sysDPI()
	state := &popupState{resultCh: resultCh, accounts: info.Accounts}

	const winW = 470
	W := scaleValue(winW, dpi)
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))

	fontHeader := newUIFont(dpi, 600, -12)
	fontTitle := newUIFont(dpi, 700, -17)
	fontQuota := newUIFont(dpi, 700, -26)
	fontBody := newUIFont(dpi, 400, -14)
	fontSmall := newUIFont(dpi, 400, -12)
	// Font phải sống đến khi WM_PAINT chạy xong (trong message loop)
	// nên chỉ giải phóng sau khi loop kết thúc.
	fonts := []uiFont{fontHeader, fontTitle, fontQuota, fontBody, fontSmall}
	defer func() {
		for i := range fonts {
			fonts[i].release()
		}
	}()

	margins := scaleValue(28, dpi)
	btnH := scaleValue(36, dpi)
	gap := scaleValue(12, dpi)
	textW := W - 2*margins

	// Đo chiều cao text CHUẨN bằng DrawTextW DT_CALCRECT trên screen
	// DC. Bắt buộc phải SELECT FONT vào DC trước khi đo — không thì
	// DrawTextW đo bằng font mặc định của Windows, rect bị thiếu chiều
	// cao và chữ lớn bị cắt ngang khi vẽ.
	hdcTmp, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, hdcTmp)
	measure := func(text string, font uiFont) int32 {
		if text == "" {
			return 0
		}
		r := rectI{Left: 0, Top: 0, Right: int32(textW), Bottom: 0}
		oldFont, _, _ := procSelectObject.Call(hdcTmp, font.handle)
		procDrawTextW.Call(
			hdcTmp, uintptr(unsafe.Pointer(utf16Ptr(text))),
			uintptr(len([]rune(text))+1),
			uintptr(unsafe.Pointer(&r)),
			uintptr(dtCalcrect|dtWordBreak|dtNoPrefix),
		)
		procSelectObject.Call(hdcTmp, oldFont)
		return r.Bottom
	}

	y := int32(margins)
	addLine := func(text string, font uiFont, color uint32, padBottom int) {
		if text == "" {
			return
		}
		h := measure(text, font) + int32(scaleValue(padBottom, dpi))
		state.lines = append(state.lines, drawLine{
			text:  text,
			font:  font.handle,
			color: color,
			rect:  rectI{Left: int32(margins), Top: y, Right: int32(W - margins), Bottom: y + h},
		})
		y += h
	}

	// Bố cục (tối giản, không trùng lặp): header plan → title → số
	// token lớn + dòng model → mô tả → (quyền lợi nếu chưa có quota).
	titleText := info.Title
	if info.PlanName != "" {
		addLine(info.PlanName, fontHeader, colAccent, 6)
		if strings.EqualFold(titleText, info.PlanName) {
			titleText = "" // tránh lặp tên plan 2 dòng
		}
	}
	if titleText != "" {
		addLine(titleText, fontTitle, colTextWhite, 12)
	}
	if info.Quota != nil {
		addLine(info.Quota.FormatAmount(), fontQuota, colTextWhite, 8)
		if info.Quota.Model != "" {
			addLine(info.Quota.Model+" quota", fontBody, colTextGrey, 14)
		}
	}
	if info.Desc != "" {
		addLine(info.Desc, fontBody, colTextGrey, 14)
	}
	// Khối "Bạn nhận được" chỉ hiện khi KHÔNG có quota (tránh lặp số
	// token 2 lần); quota đã thể hiện đủ ở dòng lớn phía trên.
	if info.Quota == nil && len(info.Entitlements) > 0 {
		addLine("Bạn nhận được:\n"+joinLines(info.Entitlements), fontBody, colTextGrey, 0)
	}
	// Tài khoản ZCode đang đăng nhập — deal sẽ nhận về đúng tài khoản
	// này (JWT từ ~/.zcode/v2, hiển thị tên/email, không hiện token).
	if info.Account != "" {
		addLine("Tài khoản: "+info.Account, fontSmall, colTextGrey, 0)
	}

	buttonY := y + int32(scaleValue(16, dpi))
	// Hàng combobox chọn tài khoản (chỉ khi có ≥2 lựa chọn): nhãn nhỏ
	// + dropdownlist đặt ngay trên hàng nút.
	comboY := int32(0)
	state.comboHwnd = 0
	if len(info.Accounts) > 1 {
		addLine("Nhận về tài khoản:", fontSmall, colTextGrey, 4)
		comboH := scaleValue(26, dpi)
		comboY = y
		y += int32(comboH) + int32(scaleValue(10, dpi))
		buttonY = y + int32(scaleValue(6, dpi))
	}
	contentH := int(buttonY) + btnH + margins
	H := maxInt(contentH, scaleValue(210, dpi))
	if H > int(screenH)-scaleValue(80, dpi) {
		H = int(screenH) - scaleValue(80, dpi)
	}

	// Kích thước truyền vào CreateWindowExW là size NGOÀI cửa sổ
	// (gồm caption + viền) — nếu bỏ qua, caption ăn mất ~40px client
	// ở đáy và hàng nút bị che. Dùng AdjustWindowRect quy đổi từ
	// client size mong muốn (W×H là vùng vẽ) sang outer size.
	style := uintptr(wsCaption | wsSysmenu | wsMinimizebox)
	adj := rectI{Left: 0, Top: 0, Right: int32(W), Bottom: int32(H)}
	if r1, _, _ := procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&adj)), style, 0); r1 != 0 {
		W = int(adj.Right - adj.Left)
		H = int(adj.Bottom - adj.Top)
	}

	x := (int(screenW) - W) / 2
	yWin := (int(screenH) - H) / 3
	if x < 0 {
		x = 0
	}
	if yWin < 0 {
		yWin = 0
	}

	className := utf16Ptr("ZCodeDealPopupWnd")
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	// Icon app nhúng qua rsrc_windows_amd64.syso (resource ID 1) —
	// hiện trên title bar của popup; thiếu resource thì về icon mặc định.
	appIcon, _, _ := procLoadIconW.Call(hInstance, 1)

	wndClass := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   syscall.NewCallback(popupWndProc(state)),
		HInstance:     syscall.Handle(hInstance),
		HIcon:         syscall.Handle(appIcon),
		HIconSm:       syscall.Handle(appIcon),
		HCursor:       syscall.Handle(hCursor),
		HbrBackground: 0, // tự vẽ nền tối trong WM_ERASEBKGND/WM_PAINT
		LpszClassName: className,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass))); ret == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}

	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(wsExTopmost),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("ZCode Deal"))),
		uintptr(wsCaption|wsSysmenu|wsMinimizebox),
		uintptr(int32(x)), uintptr(int32(yWin)), uintptr(uint32(W)), uintptr(uint32(H)),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}
	popupSetHwnd(hwnd)

	fontButton := newUIFont(dpi, 400, -14)
	defer fontButton.release()

	claimLabel := info.ButtonLabel
	if claimLabel == "" {
		claimLabel = "Nhận deal"
	}

	// Combobox chọn tài khoản nhận deal: mục 0 là tài khoản mặc định
	// đang đăng nhập trong ZCode, tiếp theo là các tài khoản cockpit.
	if len(info.Accounts) > 1 {
		comboW := W - 2*margins
		state.comboHwnd, _, _ = procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(utf16Ptr("COMBOBOX"))),
			uintptr(0),
			uintptr(wsChild|wsVisible|cbsDropdownList),
			uintptr(int32(margins)), uintptr(int32(comboY)), uintptr(uint32(comboW)), uintptr(uint32(scaleValue(160, dpi))),
			hwnd, 0, hInstance, 0,
		)
		if state.comboHwnd != 0 {
			procSendMessageW.Call(state.comboHwnd, uintptr(wmSetFont), fontButton.handle, 1)
			for _, choice := range info.Accounts {
				textPtr := uintptr(unsafe.Pointer(utf16Ptr(choice.Label)))
				procSendMessageW.Call(state.comboHwnd, uintptr(cbAddString), 0, textPtr)
			}
			procSendMessageW.Call(state.comboHwnd, uintptr(cbSetCurSel), 0, 0)
		}
	}

	btnW := scaleValue(130, dpi)
	hClaim := createControl("BUTTON", claimLabel, bsDefPushButton|bsPushButton, hInstance, margins, int(buttonY), btnW, btnH, hwnd, idBtnClaim)
	procSendMessageW.Call(hClaim, uintptr(wmSetFont), fontButton.handle, 1)
	hOpen := createControl("BUTTON", "Mở ZCode", bsPushButton, hInstance, margins+btnW+gap, int(buttonY), btnW, btnH, hwnd, idBtnOpen)
	procSendMessageW.Call(hOpen, uintptr(wmSetFont), fontButton.handle, 1)
	hLater := createControl("BUTTON", "Để sau", bsPushButton, hInstance, margins+2*(btnW+gap), int(buttonY), btnW, btnH, hwnd, idBtnLater)
	procSendMessageW.Call(hLater, uintptr(wmSetFont), fontButton.handle, 1)

	procShowWindow.Call(hwnd, uintptr(swShow))
	procSetForegroundWindow.Call(hwnd)

	var m msgW
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func popupWndProc(state *popupState) func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	return func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case wmCommand:
			switch wParam & 0xFFFF {
			case idBtnClaim:
				sendPopupResult(state, "claim", selectedAccountIdx(state))
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			case idBtnOpen:
				sendPopupResult(state, "open", 0)
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			case idBtnLater:
				sendPopupResult(state, "later", 0)
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			}
		case wmClose:
			sendPopupResult(state, "later", 0)
			procDestroyWindow.Call(uintptr(hwnd))
			return 0
		case wmEraseBkgnd:
			// Tô nền navy ngay, không để Windows tô trắng gây chớp.
			var rc rectI
			if r1, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc))); r1 != 0 {
				fillRectColor(uintptr(wParam), rc, colBgNavy)
			}
			return 1
		case wmPaint:
			var ps paintStructW
			hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
			if hdc != 0 {
				paintDealPopup(state, hdc, ps.RcPaint)
				procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
			}
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
		ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
		return ret
	}
}

func paintDealPopup(state *popupState, hdc uintptr, rc rectI) {
	// Nền navy toàn vùng paint (WM_ERASEBKGND đã tô trước đó; tô lại
	// trong WM_PAINT cho chắc chắn).
	fillRectColor(hdc, rc, colBgNavy)
	for _, line := range state.lines {
		drawTextBlock(hdc, line.font, line.text, line.rect, line.color, dtWordBreak)
	}
}

// selectedAccountIdx đọc lựa chọn hiện tại của combobox tài khoản
// (mục 0 = mặc định); không có combobox → 0.
func selectedAccountIdx(state *popupState) int {
	if state.comboHwnd == 0 {
		return 0
	}
	idx, _, _ := procSendMessageW.Call(state.comboHwnd, uintptr(cbGetCurSel), 0, 0)
	if int32(idx) < 0 {
		return 0
	}
	return int(idx)
}

func sendPopupResult(state *popupState, choice string, accountIdx int) {
	select {
	case state.resultCh <- popupChoice{Choice: choice, AccountIdx: accountIdx}:
	default:
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

func joinLines(lines []string) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
