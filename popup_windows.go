//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Popup Win32 thuần (không thêm dependency): vẽ deal giống UI app ZCode
// — popup khi CÓ deal theo banner tối màu (ảnh 1: header plan, số token
// lớn, nút Claim), popup SAU KHI nhận theo tấm vé trắng (ảnh 2, xem
// popup_success_windows.go). Người dùng phải chủ động bấm — tool không
// tự claim ngầm (xem README).

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdiplus  = windows.NewLazySystemDLL("gdiplus.dll")

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
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procSetBkMode           = user32.NewProc("SetBkMode") // gdi32 nhưng lib không quan trọng
	procSetTextColor        = user32.NewProc("SetTextColor")
	procSelectObject        = user32.NewProc("SelectObject")
	procDrawTextW           = user32.NewProc("DrawTextW")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	procFillRect            = user32.NewProc("FillRect")
	procCreatePen           = gdi32.NewProc("CreatePen")
	procMoveToEx            = gdi32.NewProc("MoveToEx")
	procLineTo              = gdi32.NewProc("LineTo")
	procRoundRect           = gdi32.NewProc("RoundRect")
	procEllipse             = gdi32.NewProc("Ellipse")
	procSetPixel            = gdi32.NewProc("SetPixel")
	procGetStockObject      = gdi32.NewProc("GetStockObject")
	procCreateFontW         = gdi32.NewProc("CreateFontW")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procGdiplusStartup      = gdiplus.NewProc("GdiplusStartup")
	procGdiplusShutdown     = gdiplus.NewProc("GdiplusShutdown")
	procGdipCreateBitmap    = gdiplus.NewProc("GdipCreateBitmapFromFile")
	procGdipGetImgWidth     = gdiplus.NewProc("GdipGetImageWidth")
	procGdipGetImgHeight    = gdiplus.NewProc("GdipGetImageHeight")
	procGdipCreateFromHDC   = gdiplus.NewProc("GdipCreateFromHDC")
	procGdipDrawImageRectI  = gdiplus.NewProc("GdipDrawImageRectI")
	procGdipDeleteGraphics  = gdiplus.NewProc("GdipDeleteGraphics")
	procGdipDisposeImage    = gdiplus.NewProc("GdipDisposeImage")
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

	wmCommand     = 0x0111
	wmClose       = 0x0010
	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmEraseBkgnd  = 0x0014
	wmSetFont    = 0x0030
	wmCtlColorStc = 0x0138

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
	dtLeft      = 0x00000000
	dtCenter    = 0x00000001
	dtVCenter   = 0x00000004
	dtSingle    = 0x00000020
	dtWordBreak = 0x00000010
	dtNoPrefix  = 0x00000800

	bkTransparent = 1

	psSolid    = 0
	psDash     = 1
	whiteBrushStockConst = 0 // GetStockObject(WHITE_BRUSH)
	nullPenStockConst    = 5 // GetStockObject(NULL_PEN)

	// Control IDs của WM_COMMAND (LOWORD wParam).
	idBtnClaim = 1001
	idBtnLater = 1002
	idBtnOpen  = 1003

	idBtnGotIt = 2001
	idBtnShare = 2002
)

// Màu lấy từ UI app ZCode (ảnh 1/2): nền navy đậm, accent xanh dương,
// vé trắng. COLORREF = 0x00BBGGRR. (colorRef là hàm nên khai báo var,
// không thể const.)
var (
	colBgNavy    = colorRef(11, 16, 32)
	colCardWhite = colorRef(255, 255, 255)
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

type gdiplusStartupInput struct {
	Version                  uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

// popupOpenMu chỉ cho 1 popup tại 1 thời điểm (deal + vé dùng chung):
// deal mới đến khi popup đang mở được ghi vào pending-claim.json và bỏ
// qua để không chồng cửa sổ.
var popupOpenMu sync.Mutex

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

// popupAcquire giữ popupOpenMu; trả false nếu đã có popup khác mở.
func popupAcquire() bool { return popupOpenMu.TryLock() }

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
	hight  int // chiều cao em (âm), trước khi scale
	weight int
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
	return uiFont{handle: font, hight: height, weight: weight}
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
	textLen := len([]rune(text))
	procDrawTextW.Call(
		hdc,
		uintptr(unsafe.Pointer(textUTF16)), uintptr(textLen),
		uintptr(unsafe.Pointer(&rect)),
		uintptr(dtNoPrefix)|flags,
	)
}

// fillRectColor tô 1 vùng bằng màu đặc (tạo brush rồi xoá ngay).
func fillRectColor(hdc uintptr, rect rectI, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	if brush != 0 {
		procFillRect.Call(hdc, uintptr(brush), uintptr(unsafe.Pointer(&rect)))
		procDeleteObject.Call(brush)
	}
}

// ---- Popup khi CÓ deal (ảnh 1: banner tối màu + nút Claim) ----

// showDealPopup mở popup nhận deal và BLOCK cho đến khi người dùng
// chọn: "claim" / "later" / "open" / "none" / "busy".
func showDealPopup(info popupDealInfo) string {
	if !popupAcquire() {
		return "busy"
	}
	defer popupOpenMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	resultCh := make(chan string, 1)
	if err := runPopupWindow(info, resultCh); err != nil {
		resultCh <- "none"
	}
	return <-resultCh
}

// drawLine là 1 khối text trong bố cục popup, vẽ trong WM_PAINT.
type drawLine struct {
	text  string
	font  uintptr
	color uint32
	rect  rectI
	flags uintptr
}

type popupState struct {
	resultCh chan string
	bitmap   uintptr
	heroX    int32
	heroW    int32
	heroH    int32
	lines    []drawLine
	dpi      int
}

func runPopupWindow(info popupDealInfo, resultCh chan string) error {
	if err := procRegisterClassExW.Find(); err != nil {
		return err
	}
	oldCtx, _, _ := procSetThreadDpiCtx.Call(dpiAwareV2)
	defer procSetThreadDpiCtx.Call(oldCtx)

	dpi := sysDPI()

	state := &popupState{resultCh: resultCh, dpi: dpi}
	if len(info.HeroPNG) > 0 && procGdiplusStartup.Find() == nil {
		var token uintptr
		startInput := gdiplusStartupInput{Version: 1}
		if ret, _, _ := procGdiplusStartup.Call(
			uintptr(unsafe.Pointer(&token)),
			uintptr(unsafe.Pointer(&startInput)), 0,
		); ret == 0 {
			state.bitmap, state.heroW, state.heroH = loadHeroBitmap(info.HeroPNG)
			defer procGdiplusShutdown.Call(token)
		}
	}

	const winW = 470
	W := scaleValue(winW, dpi)
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))

	// Ảnh hero: fit theo tỉ lệ gốc, cap chiều cao, full-bleed.
	const heroCapPx = 180
	if state.bitmap != 0 {
		heroCap := int32(scaleValue(heroCapPx, dpi))
		imgW, imgH := state.heroW, state.heroH
		if imgW <= 0 || imgH <= 0 {
			state.bitmap = 0
		} else {
			fitW := int32(W)
			fitH := int32(W) * imgH / imgW
			if fitH > heroCap {
				fitH = heroCap
				fitW = imgW * heroCap / imgH
			}
			state.heroX = (int32(W) - fitW) / 2
			state.heroW, state.heroH = fitW, fitH
		}
	}

	fontHeader := newUIFont(dpi, 600, -12)
	fontTitle := newUIFont(dpi, 700, -17)
	fontQuota := newUIFont(dpi, 700, -24)
	fontBody := newUIFont(dpi, 400, -14)
	// Font phải sống đến khi WM_PAINT chạy xong (trong message loop)
	// nên chỉ giải phóng sau khi loop kết thúc.
	fonts := []uiFont{fontHeader, fontTitle, fontQuota, fontBody}
	defer func() {
		for i := range fonts {
			fonts[i].release()
		}
	}()

	margins := scaleValue(24, dpi)
	lineH := scaleValue(17, dpi)
	charsPerLine := maxInt(20, (W-2*margins)/scaleValue(7, dpi))
	btnH := scaleValue(36, dpi)
	gap := scaleValue(12, dpi)

	y := int(margins) + scaleValue(10, dpi)
	if state.bitmap != 0 {
		y = int(state.heroH) + scaleValue(16, dpi)
	}

	addLine := func(text string, font uiFont, color uint32, height int) {
		state.lines = append(state.lines, drawLine{
			text:  text,
			font:  font.handle,
			color: color,
			rect:  rectI{Left: int32(margins), Top: int32(y), Right: int32(W - margins), Bottom: int32(y + height)},
		})
		y += height
	}

	// Header: tên plan (kiểu nhãn "ZCODE GLOBAL BUILD" trên banner).
	if info.PlanName != "" {
		addLine(info.PlanName, fontHeader, colAccent, scaleValue(20, dpi))
	}
	// Title bold trắng.
	if info.Title != "" {
		addLine(info.Title, fontTitle, colTextWhite, scaleValue(34, dpi))
	}
	// Số token lớn (kiểu "300,000,000 tokens" trên banner app).
	if info.Quota != nil {
		addLine(info.Quota.FormatAmount(), fontQuota, colTextWhite, scaleValue(42, dpi))
		if info.Quota.Model != "" {
			addLine(info.Quota.Model+" quota", fontBody, colTextGrey, scaleValue(22, dpi))
		}
	}
	// Mô tả đầy đủ.
	if info.Desc != "" {
		h := estimateLines(info.Desc, charsPerLine)*lineH + scaleValue(6, dpi)
		addLine(info.Desc, fontBody, colTextGrey, h)
	}
	// Quyền lợi thật từ billing/preview (nếu có).
	if len(info.Entitlements) > 0 {
		h := (len(info.Entitlements)+1)*lineH + scaleValue(6, dpi)
		addLine("Bạn nhận được:\n"+joinLines(info.Entitlements), fontBody, colTextGrey, h)
	}

	buttonY := y + scaleValue(8, dpi)
	contentH := buttonY + btnH + margins
	H := minInt(maxInt(contentH, scaleValue(240, dpi)), int(screenH)-scaleValue(80, dpi))

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

	wndClass := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   syscall.NewCallback(popupWndProc(state)),
		HInstance:     syscall.Handle(hInstance),
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

	fontButton := newUIFont(dpi, 400, -14)
	defer fontButton.release()

	claimLabel := info.ButtonLabel
	if claimLabel == "" {
		claimLabel = "Nhận deal"
	}

	btnW := scaleValue(130, dpi)
	hClaim := createControl("BUTTON", claimLabel, bsDefPushButton|bsPushButton, hInstance, margins, buttonY, btnW, btnH, hwnd, idBtnClaim)
	procSendMessageW.Call(hClaim, uintptr(wmSetFont), fontButton.handle, 1)
	hOpen := createControl("BUTTON", "Mở ZCode", bsPushButton, hInstance, margins+btnW+gap, buttonY, btnW, btnH, hwnd, idBtnOpen)
	procSendMessageW.Call(hOpen, uintptr(wmSetFont), fontButton.handle, 1)
	hLater := createControl("BUTTON", "Để sau", bsPushButton, hInstance, margins+2*(btnW+gap), buttonY, btnW, btnH, hwnd, idBtnLater)
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
				sendPopupResult(state, "claim")
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			case idBtnOpen:
				sendPopupResult(state, "open")
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			case idBtnLater:
				sendPopupResult(state, "later")
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			}
		case wmClose:
			sendPopupResult(state, "later")
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
	// trong WM_PAINT cho chắc chắn sau khi hero thay đổi kích thước).
	fillRectColor(hdc, rc, colBgNavy)
	if state.bitmap != 0 {
		var graphics uintptr
		if ret, _, _ := procGdipCreateFromHDC.Call(hdc, uintptr(unsafe.Pointer(&graphics))); ret == 0 && graphics != 0 {
			procGdipDrawImageRectI.Call(
				graphics, state.bitmap,
				uintptr(uint32(state.heroX)), 0,
				uintptr(uint32(state.heroW)), uintptr(uint32(state.heroH)),
			)
			procGdipDeleteGraphics.Call(graphics)
		}
	}
	for _, line := range state.lines {
		drawTextBlock(hdc, line.font, line.text, line.rect, line.color, line.flags|dtWordBreak)
	}
}

func sendPopupResult(state *popupState, choice string) {
	select {
	case state.resultCh <- choice:
	default:
	}
}

// loadHeroBitmap decode bytes ảnh (PNG/JPEG…) thành GDI+ bitmap; trả
// (bitmap, width, height). Ảnh ghi ra file tạm rồi decode bằng
// GdipCreateBitmapFromFile (tránh chuyển uintptr↔Pointer thủ công).
// Bất kỳ bước nào lỗi → (0, 0, 0) → text-only.
func loadHeroBitmap(data []byte) (uintptr, int32, int32) {
	if err := procGdipCreateBitmap.Find(); err != nil {
		return 0, 0, 0
	}
	tmp, err := os.CreateTemp("", "zdeal-hero-*.img")
	if err != nil {
		return 0, 0, 0
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return 0, 0, 0
	}
	if err := tmp.Close(); err != nil {
		return 0, 0, 0
	}
	pathPtr, err := windows.UTF16PtrFromString(tmpName)
	if err != nil {
		return 0, 0, 0
	}
	var bitmap uintptr
	if ret, _, _ := procGdipCreateBitmap.Call(
		uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&bitmap)),
	); ret != 0 || bitmap == 0 {
		return 0, 0, 0
	}
	var w, h int32
	procGdipGetImgWidth.Call(bitmap, uintptr(unsafe.Pointer(&w)))
	procGdipGetImgHeight.Call(bitmap, uintptr(unsafe.Pointer(&h)))
	return bitmap, w, h
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

// estimateLines đếm số dòng hiển thị sau khi word-wrap: mỗi segment
// (ngắt bằng \n) chiếm ceil(len/charsPerLine) dòng.
func estimateLines(text string, charsPerLine int) int {
	lines := 0
	for _, segment := range splitLines(text) {
		n := (len([]rune(segment)) + charsPerLine - 1) / charsPerLine
		if n < 1 {
			n = 1
		}
		lines += n
	}
	return maxInt(lines, 1)
}

func splitLines(text string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	parts = append(parts, text[start:])
	return parts
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
