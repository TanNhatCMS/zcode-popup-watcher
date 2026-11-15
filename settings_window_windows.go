//go:build windows

package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

// Cửa sổ "Cài đặt" hợp nhất của tray: khởi động cùng hệ thống +
// tài khoản check deal + khung giờ + tần suất — một chỗ duy nhất.
//
// Checkbox tự VẼ (hình vuông + dấu tick trong WM_PAINT, click hit-test
// trong WM_LBUTTONUP) thay vì control BUTTON — không phụ thuộc theme
// Windows nên nền luôn navy, chữ luôn trắng, không bị tô trắng đục.
// Lưu: settings → ~/.zcode/mod/deal-settings.json; autostart →
// HKCU Run. Poll loop nạp lại settings mỗi vòng = hiệu lực tức thì.

const (
	idCboAccount = 3002
	idEdFrom     = 3004
	idEdTo       = 3005
	idCboFreq    = 3006
	idBtnSave    = 3007
)

// freqPresets là các lựa chọn tần suất poll trên popup cài đặt.
var freqPresets = []struct {
	label string
	sec   int
}{
	{"5 phút", 300},
	{"10 phút (khuyến nghị)", 600},
	{"15 phút", 900},
	{"30 phút", 1800},
	{"60 phút", 3600},
}

// checkRow là 1 dòng checkbox tự vẽ (vuông tick + nhãn).
type checkRow struct {
	Rect rectI
	On   bool
	Text string
}

type settingsState struct {
	done       chan struct{}
	dpi        int
	accKeys    []string // "default" + email từng tài khoản cockpit
	edFrom     uintptr
	edTo       uintptr
	cboAccount uintptr
	cboFreq    uintptr
	lines      []drawLine
	rows       []checkRow
	navyBrush  uintptr
	whitePen   uintptr
}

// showCheckSettingsWindow mở cửa sổ cài đặt hợp nhất và BLOCK đến khi
// đóng. Bấm "Lưu cài đặt" ghi settings + registry autostart ngay —
// poll loop nạp lại mỗi vòng nên có hiệu lực tức thì.
func showCheckSettingsWindow() {
	if !popupTryAcquire(popupKindSettings) {
		alertBox("ZCode Deal", "Có popup khác đang mở — đóng nó rồi thử lại.", true)
		return
	}
	defer popupRelease(popupKindSettings)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	done := make(chan struct{}, 1)
	if err := runSettingsWindow(done); err != nil {
		alertBox("ZCode Deal - Lỗi cài đặt", fmt.Sprintf("Không mở được cửa sổ cài đặt: %v", err), true)
	}
	<-done
}

func runSettingsWindow(done chan struct{}) error {
	if err := procRegisterClassExW.Find(); err != nil {
		return err
	}
	oldCtx, _, _ := procSetThreadDpiCtx.Call(dpiAwareV2)
	defer procSetThreadDpiCtx.Call(oldCtx)

	dpi := sysDPI()
	s := checker.LoadDealSettings()
	state := &settingsState{done: done, dpi: dpi}
	state.navyBrush, _, _ = procCreateSolidBrush.Call(uintptr(colBgNavy))
	state.whitePen, _, _ = procCreatePen.Call(uintptr(psSolid), 2, uintptr(colTextWhite))
	defer func() {
		procDeleteObject.Call(state.navyBrush)
		procDeleteObject.Call(state.whitePen)
	}()

	const winW = 480
	const winH = 392
	W := scaleValue(winW, dpi)
	H := scaleValue(winH, dpi)
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))

	fontHeader := newUIFont(dpi, 600, -12)
	fontBody := newUIFont(dpi, 400, -14)
	fontSmall := newUIFont(dpi, 400, -12)
	fontButton := newUIFont(dpi, 400, -14)
	fonts := []uiFont{fontHeader, fontBody, fontSmall, fontButton}
	defer func() {
		for i := range fonts {
			fonts[i].release()
		}
	}()

	// Các nhãn tĩnh vẽ trong WM_PAINT (nền navy). Rect phải scale DPI
	// giống control/font — không scale thì nhãn render lệch lên trên
	// và bị control đè lên (bug "chồng chéo").
	margins := scaleValue(24, dpi)
	add := func(text string, font uiFont, color uint32, top, height int) {
		state.lines = append(state.lines, drawLine{
			text: text, font: font.handle, color: color,
			rect: rectI{
				Left: int32(margins), Top: int32(scaleValue(top, dpi)),
				Right: int32(W - margins), Bottom: int32(scaleValue(top+height, dpi)),
			},
		})
	}
	add("KHỞI ĐỘNG CÙNG HỆ THỐNG", fontHeader, colTextWhite, 16, 18)
	add("CHECK DEAL", fontHeader, colTextWhite, 84, 18)
	add("Từ / Đến (HH:MM giờ VN):", fontBody, colTextGrey, 146, 20)
	add("Tài khoản check deal:", fontBody, colTextGrey, 214, 20)
	add("Tần suất check:", fontBody, colTextGrey, 278, 20)

	className := "ZCodeDealSettingsWnd"
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	appIcon, _, _ := procLoadIconW.Call(hInstance, 1)

	// Class đăng ký MỘT LẦN mỗi tiến trình; handler gắn theo hwnd.
	if err := registerPopupClass(className, hInstance, appIcon, hCursor); err != nil {
		return err
	}

	// Client size mong muốn → outer size cho CreateWindowExW.
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

	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(wsExTopmost),
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr("ZCode Deal - Cài đặt"))),
		style,
		uintptr(int32(x)), uintptr(int32(yWin)), uintptr(uint32(W)), uintptr(uint32(H)),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}
	popupSetHwnd(popupKindSettings, hwnd)
	popupSetHandler(hwnd, settingsWndHandler(state))
	defer popupDeleteHandler(hwnd)

	cw := W - 2*margins

	// Dòng checkbox tự vẽ: vuông tick 16px + nhãn trắng.
	addCheckRow := func(top int, on bool, text string) {
		state.rows = append(state.rows, checkRow{
			Rect: rectI{
				Left: int32(margins), Top: int32(top),
				Right: int32(W - margins), Bottom: int32(top + scaleValue(22, dpi)),
			},
			On:   on,
			Text: text,
		})
	}
	addCheckRow(scaleValue(44, dpi), autostartEnabled(), "Tự mở zcode-deal khi đăng nhập Windows")
	addCheckRow(scaleValue(110, dpi), s.CheckWindowEnabled, "Chỉ check trong khung giờ (ngoài khung KHÔNG gọi API)")

	// Ô giờ from/to (EDIT trắng — control nhập liệu giữ nền sáng).
	edW := scaleValue(110, dpi)
	gapS := scaleValue(8, dpi)
	state.edFrom = createControl("EDIT", s.CheckFrom, wsBorder, hInstance, margins+scaleValue(56, dpi), scaleValue(170, dpi), edW, scaleValue(24, dpi), hwnd, idEdFrom)
	procSendMessageW.Call(state.edFrom, uintptr(wmSetFont), fontBody.handle, 1)
	state.edTo = createControl("EDIT", s.CheckTo, wsBorder, hInstance, margins+scaleValue(56, dpi)+edW+gapS, scaleValue(170, dpi), edW, scaleValue(24, dpi), hwnd, idEdTo)
	procSendMessageW.Call(state.edTo, uintptr(wmSetFont), fontBody.handle, 1)

	// Combobox tài khoản: "default" + các tài khoản cockpit có JWT.
	state.cboAccount, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("COMBOBOX"))),
		uintptr(0),
		uintptr(wsChild|wsVisible|cbsDropdownList),
		uintptr(int32(margins)), uintptr(int32(scaleValue(238, dpi))), uintptr(uint32(cw)), uintptr(uint32(scaleValue(170, dpi))),
		hwnd, uintptr(idCboAccount), hInstance, 0,
	)
	procSendMessageW.Call(state.cboAccount, uintptr(wmSetFont), fontBody.handle, 1)
	choices := buildAccountChoices(trayCtx)
	selIdx := 0
	for i, choice := range choices {
		procSendMessageW.Call(state.cboAccount, uintptr(cbAddString), 0, uintptr(unsafe.Pointer(utf16Ptr(choice.Label))))
		state.accKeys = append(state.accKeys, choice.Key)
		if strings.EqualFold(s.CheckAccount, choice.Key) {
			selIdx = i
		}
	}
	procSendMessageW.Call(state.cboAccount, uintptr(cbSetCurSel), uintptr(selIdx), 0)

	// Tần suất: chọn preset khớp interval hiện tại.
	curInterval := s.CheckInterval
	if curInterval <= 0 {
		curInterval = int(trayOpts.IntervalSec)
	}
	state.cboFreq, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("COMBOBOX"))),
		uintptr(0),
		uintptr(wsChild|wsVisible|cbsDropdownList),
		uintptr(int32(margins)), uintptr(int32(scaleValue(302, dpi))), uintptr(uint32(cw)), uintptr(uint32(scaleValue(170, dpi))),
		hwnd, uintptr(idCboFreq), hInstance, 0,
	)
	procSendMessageW.Call(state.cboFreq, uintptr(wmSetFont), fontBody.handle, 1)
	for i, preset := range freqPresets {
		procSendMessageW.Call(state.cboFreq, uintptr(cbAddString), 0, uintptr(unsafe.Pointer(utf16Ptr(preset.label))))
		if preset.sec == curInterval {
			procSendMessageW.Call(state.cboFreq, uintptr(cbSetCurSel), uintptr(i), 0)
		}
	}

	saveW := scaleValue(150, dpi)
	btnY := scaleValue(340, dpi)
	hSave := createControl("BUTTON", "Lưu cài đặt", bsDefPushButton|bsPushButton, hInstance,
		(W-saveW)/2, btnY, saveW, scaleValue(34, dpi), hwnd, idBtnSave)
	procSendMessageW.Call(hSave, uintptr(wmSetFont), fontButton.handle, 1)

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

// paintCheckRows vẽ các dòng checkbox tự vẽ: vuông viền trắng, tick
// trắng 2 nét chéo khi bật, nhãn trắng căn giữa dọc theo vuông.
func paintCheckRows(state *settingsState, hdc uintptr) {
	dpi := state.dpi
	box := scaleValue(16, dpi)
	whitePen, _, _ := procCreatePen.Call(uintptr(psSolid), 1, uintptr(colTextWhite))
	oldPen, _, _ := procSelectObject.Call(hdc, whitePen)
	defer func() {
		procSelectObject.Call(hdc, oldPen)
		procDeleteObject.Call(whitePen)
	}()
	fontBody := newUIFont(dpi, 400, -14)
	defer fontBody.release()
	for _, row := range state.rows {
		x0 := int(row.Rect.Left)
		y0 := int(row.Rect.Top) + scaleValue(3, dpi)
		x1 := x0 + box
		y1 := y0 + box
		// Viền vuông trắng 1px.
		drawBoxOutline(hdc, x0, y0, x1, y1)
		if row.On {
			// Tick trắng: 2 đoạn chéo (✓).
			strokeLine(hdc, x0+scaleValue(3, dpi), y0+box/2, x0+box/2, y1-scaleValue(3, dpi))
			strokeLine(hdc, x0+box/2, y1-scaleValue(3, dpi), x1-scaleValue(3, dpi), y0+scaleValue(3, dpi))
		}
		textRect := rectI{
			Left: int32(x1 + scaleValue(10, dpi)), Top: row.Rect.Top,
			Right: row.Rect.Right, Bottom: row.Rect.Bottom,
		}
		drawTextBlock(hdc, fontBody.handle, row.Text, textRect, colTextWhite, dtSingle|dtVCenter)
	}
}

func drawBoxOutline(hdc uintptr, x0, y0, x1, y1 int) {
	fillRectColor(hdc, rectI{int32(x0), int32(y0), int32(x1), int32(y0 + 1)}, colTextWhite)
	fillRectColor(hdc, rectI{int32(x0), int32(y1 - 1), int32(x1), int32(y1)}, colTextWhite)
	fillRectColor(hdc, rectI{int32(x0), int32(y0), int32(x0 + 1), int32(y1)}, colTextWhite)
	fillRectColor(hdc, rectI{int32(x1 - 1), int32(y0), int32(x1), int32(y1)}, colTextWhite)
}

// strokeLine vẽ 1 đoạn thẳng bằng pen đã chọn sẵn trong hdc.
func strokeLine(hdc uintptr, x0, y0, x1, y1 int) {
	procMoveToEx.Call(hdc, uintptr(int32(x0)), uintptr(int32(y0)), 0)
	procLineTo.Call(hdc, uintptr(int32(x1)), uintptr(int32(y1)))
}

func settingsWndHandler(state *settingsState) func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	return func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case wmCommand:
			switch wParam & 0xFFFF {
			case idBtnSave:
				saveSettingsFromControls(state, hwnd)
				return 0
			}
		case wmLButtonUp:
			// Hit-test các dòng checkbox tự vẽ → toggle + vẽ lại.
			// Tọa độ chuột nằm trong lParam (client px): low word = x,
			// high word = y — wParam là flags phím, KHÔNG phải tọa độ
			// (đọc wParam là bug "không tick được").
			x := int(int16(lParam & 0xFFFF))
			y := int(int16((lParam >> 16) & 0xFFFF))
			toggled := false
			for i := range state.rows {
				r := state.rows[i].Rect
				if int32(x) >= r.Left && int32(x) <= r.Right && int32(y) >= r.Top && int32(y) <= r.Bottom {
					state.rows[i].On = !state.rows[i].On
					toggled = true
					break
				}
			}
			if toggled {
				procInvalidateRect.Call(uintptr(hwnd), 0, 0)
			}
			return 0
		case wmClose:
			closeOnce(state.done)
			procDestroyWindow.Call(uintptr(hwnd))
			return 0
		case wmEraseBkgnd:
			var rc rectI
			if r1, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc))); r1 != 0 {
				fillRectColor(uintptr(wParam), rc, colBgNavy)
			}
			return 1
		case wmPaint:
			var ps paintStructW
			hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
			if hdc != 0 {
				fillRectColor(hdc, ps.RcPaint, colBgNavy)
				for _, line := range state.lines {
					drawTextBlock(hdc, line.font, line.text, line.rect, line.color, dtSingle)
				}
				paintCheckRows(state, hdc)
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

// saveSettingsFromControls đọc toàn bộ controls, validate rồi ghi
// settings + registry autostart. Lỗi validate → alert + giữ cửa sổ.
func saveSettingsFromControls(state *settingsState, hwnd syscall.Handle) {
	s := checker.LoadDealSettings()

	// Autostart (HKCU Run) — trạng thái từ dòng checkbox tự vẽ.
	if err := setAutostart(state.rows[0].On); err != nil {
		alertBox("ZCode Deal - Lỗi cài đặt", fmt.Sprintf("Không lưu được khởi động cùng hệ thống: %v", err), true)
		return
	}

	// Tài khoản check deal từ combobox.
	idx := ctrlCurSel(state.cboAccount)
	if idx >= 0 && idx < len(state.accKeys) {
		s.CheckAccount = state.accKeys[idx]
	}

	// Khung giờ: validate HH:MM trước khi lưu; bật/tắt từ checkbox row.
	from := ctrlText(state.edFrom)
	to := ctrlText(state.edTo)
	if _, ok := checker.ParseHHMM(from); !ok {
		alertBox("ZCode Deal - Lỗi cài đặt", "Giờ bắt đầu không hợp lệ — nhập dạng HH:MM (ví dụ 17:55).", true)
		return
	}
	if _, ok := checker.ParseHHMM(to); !ok {
		alertBox("ZCode Deal - Lỗi cài đặt", "Giờ kết thúc không hợp lệ — nhập dạng HH:MM (ví dụ 22:10).", true)
		return
	}
	s.CheckFrom, s.CheckTo = from, to
	s.CheckWindowEnabled = state.rows[1].On

	// Tần suất.
	fi := ctrlCurSel(state.cboFreq)
	if fi >= 0 && fi < len(freqPresets) {
		s.CheckInterval = freqPresets[fi].sec
	}

	if err := checker.SaveDealSettings(s); err != nil {
		alertBox("ZCode Deal - Lỗi cài đặt", fmt.Sprintf("Không ghi được settings: %v", err), true)
		return
	}
	logLine(fmt.Sprintf("[gotray] Đã lưu cài đặt: autostart=%v · check account=%s · khung %s-%s giờ VN (bật=%v) · tần suất %ds",
		state.rows[0].On, s.CheckAccount, s.CheckFrom, s.CheckTo, s.CheckWindowEnabled, s.CheckInterval))
	closeOnce(state.done)
	procDestroyWindow.Call(uintptr(hwnd))
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// ctrlCurSel đọc lựa chọn hiện tại của combobox (-1 → 0).
func ctrlCurSel(hwnd uintptr) int {
	if hwnd == 0 {
		return 0
	}
	idx, _, _ := procSendMessageW.Call(hwnd, uintptr(cbGetCurSel), 0, 0)
	if int32(idx) < 0 {
		return 0
	}
	return int(idx)
}

// ctrlText đọc text của control (EDIT).
func ctrlText(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	buf := make([]uint16, 64)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}
