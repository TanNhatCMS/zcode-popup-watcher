//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
	"github.com/getlantern/systray"
)

// Popup "Thông tin" từ menu tray: thông tin app + tác giả + phiên bản,
// nút mở nhật ký và kiểm tra cập nhật (tải + tự cài + khởi động lại
// tray khi người dùng xác nhận).

const (
	idBtnLog    = 4001
	idBtnUpdate = 4002
	idBtnClose  = 4003

	mbYesNo = 0x00000004
)

type infoState struct {
	done      chan struct{}
	dpi       int
	navyBrush uintptr
	lines     []drawLine
}

// showInfoWindow mở popup thông tin, BLOCK đến khi đóng.
func showInfoWindow() {
	if !popupTryAcquire() {
		alertBox("ZCode Deal", "Có popup khác đang mở — đóng nó rồi thử lại.", true)
		return
	}
	defer popupRelease()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	done := make(chan struct{}, 1)
	if err := runInfoWindow(done); err != nil {
		alertBox("ZCode Deal", fmt.Sprintf("Không mở được cửa sổ thông tin: %v", err), true)
	}
	<-done
}

func runInfoWindow(done chan struct{}) error {
	if err := procRegisterClassExW.Find(); err != nil {
		return err
	}
	oldCtx, _, _ := procSetThreadDpiCtx.Call(dpiAwareV2)
	defer procSetThreadDpiCtx.Call(oldCtx)

	dpi := sysDPI()
	state := &infoState{done: done, dpi: dpi}
	state.navyBrush, _, _ = procCreateSolidBrush.Call(uintptr(colBgNavy))
	defer procDeleteObject.Call(state.navyBrush)

	const winW = 470
	const winH = 330
	W := scaleValue(winW, dpi)
	H := scaleValue(winH, dpi)
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))

	fontTitle := newUIFont(dpi, 700, -20)
	fontBody := newUIFont(dpi, 400, -14)
	fontSmall := newUIFont(dpi, 400, -12)
	fontButton := newUIFont(dpi, 400, -14)
	fonts := []uiFont{fontTitle, fontBody, fontSmall, fontButton}
	defer func() {
		for i := range fonts {
			fonts[i].release()
		}
	}()

	margins := scaleValue(28, dpi)
	add := func(text string, font uiFont, color uint32, top, height int, centered bool) {
		flags := uintptr(dtWordBreak)
		if centered {
			flags = uintptr(dtSingle | dtCenter)
		}
		state.lines = append(state.lines, drawLine{
			text: text, font: font.handle, color: color,
			rect:  rectI{Left: int32(margins), Top: int32(top), Right: int32(W - margins), Bottom: int32(top + height)},
			flags: flags,
		})
	}
	add("zcode-deal", fontTitle, colTextWhite, 24, 36, true)
	add(fmt.Sprintf("Phiên bản %s", version), fontBody, colAccent, 64, 22, true)
	add("Theo dõi deal ZCode — popup nhận deal khi bạn bấm.", fontBody, colTextGrey, 94, 22, true)
	add("Tác giả: TanNhatCMS · MIT © 2026", fontSmall, colTextGrey, 120, 20, true)

	className := utf16Ptr("ZCodeDealInfoWnd")
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	appIcon, _, _ := procLoadIconW.Call(hInstance, 1)

	wndClass := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   syscall.NewCallback(infoWndProc(state)),
		HInstance:     syscall.Handle(hInstance),
		HIcon:         syscall.Handle(appIcon),
		HIconSm:       syscall.Handle(appIcon),
		HCursor:       syscall.Handle(hCursor),
		HbrBackground: 0,
		LpszClassName: className,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass))); ret == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}

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
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("ZCode Deal - Thông tin"))),
		style,
		uintptr(int32(x)), uintptr(int32(yWin)), uintptr(uint32(W)), uintptr(uint32(H)),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}
	popupSetHwnd(hwnd)

	btnW := scaleValue(130, dpi)
	btnH := scaleValue(34, dpi)
	gap := scaleValue(12, dpi)
	btnY := H - scaleValue(84, dpi)
	total := 3*btnW + 2*gap
	btnX := (W - total) / 2
	hLog := createControl("BUTTON", "Nhật ký", bsPushButton, hInstance, btnX, btnY, btnW, btnH, hwnd, idBtnLog)
	hUpdate := createControl("BUTTON", "Kiểm tra cập nhật", bsPushButton, hInstance, btnX+btnW+gap, btnY, btnW, btnH, hwnd, idBtnUpdate)
	hClose := createControl("BUTTON", "Đóng", bsDefPushButton|bsPushButton, hInstance, btnX+2*(btnW+gap), btnY, btnW, btnH, hwnd, idBtnClose)
	for _, h := range []uintptr{hLog, hUpdate, hClose} {
		procSendMessageW.Call(h, uintptr(wmSetFont), fontButton.handle, 1)
	}

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

func infoWndProc(state *infoState) func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	return func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case wmCommand:
			switch wParam & 0xFFFF {
			case idBtnLog:
				if err := openFile(checker.LogFile()); err != nil {
					alertBox("ZCode Deal", fmt.Sprintf("Không mở được nhật ký: %v", err), true)
				}
				return 0
			case idBtnUpdate:
				// Check + tải + cài chạy trong goroutine riêng để popup
				// không đơ khi mạng chậm.
				go runUpdateFlow(hwnd)
				return 0
			case idBtnClose:
				closeOnce(state.done)
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			}
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
					drawTextBlock(hdc, line.font, line.text, line.rect, line.color, line.flags)
				}
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

// runUpdateFlow kiểm tra bản mới từ GitHub Releases → xác nhận → tải
// exe mới → đổi chỗ (giữ .old làm backup) → mời khởi động lại tray.
func runUpdateFlow(hwnd syscall.Handle) {
	info, err := checker.CheckUpdate(version)
	if err != nil {
		alertBox("ZCode Deal - Kiểm tra cập nhật", fmt.Sprintf("Không kiểm tra được bản mới:\n%v", err), true)
		return
	}
	if !info.HasUpdate {
		alertBox("ZCode Deal - Kiểm tra cập nhật",
			fmt.Sprintf("Bạn đang dùng bản mới nhất (%s).\nBản mới nhất trên GitHub: %s", version, info.LatestTag), false)
		return
	}
	if info.AssetURL == "" {
		alertBox("ZCode Deal - Kiểm tra cập nhật",
			fmt.Sprintf("Có bản mới %s nhưng chưa thấy file %s trong release — tải thủ công tại:\nhttps://github.com/%s/releases",
				info.LatestTag, info.AssetName, checker.UpdateRepo), true)
		return
	}
	yes := alertBoxYesNo("ZCode Deal - Có bản mới!",
		fmt.Sprintf("Bản mới: %s (hiện tại: %s)\nKích thước: %.1f MB\n\nTải về và cài đặt ngay?",
			info.LatestTag, version, float64(info.AssetSize)/(1024*1024)))
	if !yes {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		alertBox("ZCode Deal - Lỗi cập nhật", fmt.Sprintf("Không xác định được đường dẫn exe: %v", err), true)
		return
	}
	newPath := exe + ".new"
	alertBox("ZCode Deal - Đang tải", fmt.Sprintf("Đang tải %s...\nCửa sổ này sẽ báo khi xong.", info.LatestTag), false)
	if err := checker.DownloadFile(info.AssetURL, newPath); err != nil {
		_ = os.Remove(newPath)
		alertBox("ZCode Deal - Lỗi cập nhật", fmt.Sprintf("Tải bản cập nhật thất bại:\n%v", err), true)
		return
	}
	// Đổi chỗ: exe đang chạy cho phép RENAME (không cho ghi đè) — giữ
	// bản cũ thành .old làm backup, đặt bản mới vào chỗ cũ.
	oldPath := exe + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		_ = os.Remove(newPath)
		alertBox("ZCode Deal - Lỗi cập nhật", fmt.Sprintf("Không đổi được tên exe (file đang mở?): %v", err), true)
		return
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe) // rollback
		alertBox("ZCode Deal - Lỗi cập nhật", fmt.Sprintf("Không đặt được bản mới vào chỗ cũ: %v", err), true)
		return
	}
	restart := alertBoxYesNo("ZCode Deal - Cập nhật xong!",
		fmt.Sprintf("Đã cài bản %s.\n\nKhởi động lại tray ngay bây giờ?", info.LatestTag))
	if restart {
		// Mở tray mới (bản đã cài) rồi thoát tray hiện tại.
		if err := execStart(exe, "tray"); err != nil {
			alertBox("ZCode Deal", fmt.Sprintf("Không tự khởi động lại được — chạy thủ công:\n%s tray", exe), true)
		}
		systray.Quit()
	}
}

// execStart khởi động tiến trình mới detached (không chờ, không
// console — exe là windowsgui).
func execStart(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	return cmd.Start()
}

// alertBoxYesNo MessageBox Yes/No — true nếu người dùng bấm Yes.
func alertBoxYesNo(title, text string) bool {
	res, _, _ := procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		uintptr(mbYesNo|mbSetForeground|mbTopmost|mbIconInfo),
	)
	return res == uintptr(idYes)
}

const idYes = 6
