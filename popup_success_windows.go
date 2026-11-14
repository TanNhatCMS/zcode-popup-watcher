//go:build windows

package main

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// Popup "vé trắng" sau khi nhận deal thành công — vẽ theo dialog kết
// quả của app ZCode (ảnh 2): nền navy có sao mờ, tấm vé trắng bo góc
// (số token lớn, dòng model, đường perforation có notch, "Hạn dùng
// đến"), heading "Bạn đã nhận được…" và 2 nút Đã hiểu / Chia sẻ.
// Dữ liệu: plan name + entitlements từ deal (args.zcode_plan) hoặc
// billing/preview, ends_at từ response billing/claim.

type ticketState struct {
	resultCh chan string
	dpi      int

	planName string
	quota    *planQuotaView
	endsAt   int64
	heading  string
	subline  string

	cardX, cardY, cardW, cardH int32
	sepY                       int32 // Y đường perforation (tọa độ client)
}

// planQuotaView là view nội bộ của popup vé (không phụ thuộc package
// checker để tránh leak kiểu).
type planQuotaView struct {
	Amount string // "300,000,000 Tokens"
	Model  string // "GLM-5.3-Flash"
	Benefit string // "GLM-5.3-Flash · 300M Tokens"
}

// showClaimSuccessPopup mở vé nhận deal thành công, BLOCK tới khi
// người dùng bấm: "ok" (Đã hiểu) / "share" (Chia sẻ) / "none".
func showClaimSuccessPopup(info ticketInfo) string {
	if !popupAcquire() {
		return "busy"
	}
	defer popupOpenMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	resultCh := make(chan string, 1)
	if err := runTicketWindow(info, resultCh); err != nil {
		resultCh <- "none"
	}
	return <-resultCh
}

func runTicketWindow(info ticketInfo, resultCh chan string) error {
	if err := procRegisterClassExW.Find(); err != nil {
		return err
	}
	oldCtx, _, _ := procSetThreadDpiCtx.Call(dpiAwareV2)
	defer procSetThreadDpiCtx.Call(oldCtx)

	dpi := sysDPI()
	state := &ticketState{resultCh: resultCh, dpi: dpi}

	state.planName = info.PlanName
	state.endsAt = info.EndsAt
	if info.Quota != nil {
		state.quota = &planQuotaView{
			Amount:  info.Quota.FormatAmount(),
			Model:   info.Quota.Model,
			Benefit: info.Quota.FormatBenefit(),
		}
	}
	model := ""
	if state.quota != nil {
		model = state.quota.Model
	}
	if state.planName != "" {
		state.heading = "Bạn đã nhận được: " + state.planName
	} else {
		state.heading = "Nhận deal thành công!"
	}
	if model != "" {
		state.subline = model + " sẵn sàng sử dụng."
	}

	const winW = 440
	const winH = 470
	W := scaleValue(winW, dpi)
	H := scaleValue(winH, dpi)
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))

	// Tấm vé trắng: card ở 1/3 trên, chiếm ~62% chiều cao.
	cardW := int32(W - scaleValue(64, dpi))
	cardH := int32(scaleValue(210, dpi))
	state.cardX = int32(scaleValue(32, dpi))
	state.cardY = int32(scaleValue(52, dpi))
	state.cardW, state.cardH = cardW, cardH
	state.sepY = state.cardY + int32(scaleValue(138, dpi))

	x := (int(screenW) - W) / 2
	yWin := (int(screenH) - H) / 4
	if x < 0 {
		x = 0
	}
	if yWin < 0 {
		yWin = 0
	}

	className := utf16Ptr("ZCodeDealTicketWnd")
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wndClass := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   syscall.NewCallback(ticketWndProc(state)),
		HInstance:     syscall.Handle(hInstance),
		HCursor:       syscall.Handle(hCursor),
		HbrBackground: 0,
		LpszClassName: className,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass))); ret == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}

	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(wsExTopmost),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("ZCode Deal - Nhận thành công"))),
		uintptr(wsCaption|wsSysmenu),
		uintptr(int32(x)), uintptr(int32(yWin)), uintptr(uint32(W)), uintptr(uint32(H)),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}

	fontButton := newUIFont(dpi, 400, -14)
	defer fontButton.release()

	btnW := scaleValue(120, dpi)
	btnH := scaleValue(36, dpi)
	gap := scaleValue(16, dpi)
	btnY := H - scaleValue(74, dpi)
	btnTotal := 2*btnW + gap
	btnX := (W - btnTotal) / 2
	hOk := createControl("BUTTON", "Đã hiểu", bsDefPushButton|bsPushButton, hInstance, btnX, btnY, btnW, btnH, hwnd, idBtnGotIt)
	procSendMessageW.Call(hOk, uintptr(wmSetFont), fontButton.handle, 1)
	hShare := createControl("BUTTON", "Chia sẻ", bsPushButton, hInstance, btnX+btnW+gap, btnY, btnW, btnH, hwnd, idBtnShare)
	procSendMessageW.Call(hShare, uintptr(wmSetFont), fontButton.handle, 1)

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

func ticketWndProc(state *ticketState) func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	return func(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case wmCommand:
			switch wParam & 0xFFFF {
			case idBtnGotIt:
				sendTicketResult(state, "ok")
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			case idBtnShare:
				sendTicketResult(state, "share")
				procDestroyWindow.Call(uintptr(hwnd))
				return 0
			}
		case wmClose:
			sendTicketResult(state, "ok")
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
				paintTicket(state, hdc, ps.RcPaint)
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

func sendTicketResult(state *ticketState, choice string) {
	select {
	case state.resultCh <- choice:
	default:
	}
}

func paintTicket(state *ticketState, hdc uintptr, rc rectI) {
	dpi := state.dpi
	// Nền navy + vài chấm sao mờ (giữ vị trí cố định cho không nhấp nháy).
	fillRectColor(hdc, rc, colBgNavy)
	drawStars(hdc, rc)

	fontLabel := newUIFont(dpi, 600, -12)
	fontBig := newUIFont(dpi, 700, -22)
	fontBody := newUIFont(dpi, 500, -14)
	fontSmall := newUIFont(dpi, 400, -12)
	fontHeading := newUIFont(dpi, 700, -16)
	fontSub := newUIFont(dpi, 400, -13)
	defer fontLabel.release()
	defer fontBig.release()
	defer fontBody.release()
	defer fontSmall.release()
	defer fontHeading.release()
	defer fontSub.release()

	// Tấm vé trắng bo góc: brush trắng + pen null (không viền).
	whiteBrush, _, _ := procGetStockObject.Call(uintptr(whiteBrushStockConst))
	nullPen, _, _ := procGetStockObject.Call(uintptr(nullPenStockConst))
	oldBrush, _, _ := procSelectObject.Call(hdc, whiteBrush)
	oldPen, _, _ := procSelectObject.Call(hdc, nullPen)
	procRoundRect.Call(
		hdc,
		uintptr(state.cardX), uintptr(state.cardY),
		uintptr(state.cardX+state.cardW), uintptr(state.cardY+state.cardH),
		uintptr(scaleValue(18, dpi)), uintptr(scaleValue(18, dpi)),
	)
	// Notches 2 mép (mô phỏng đường perforation vé): 2 hình tròn nền navy.
	notchR := int32(scaleValue(9, dpi))
	notchBrush, _, _ := procCreateSolidBrush.Call(uintptr(colBgNavy))
	oldNotchBrush, _, _ := procSelectObject.Call(hdc, notchBrush)
	procEllipse.Call(hdc,
		uintptr(state.cardX-notchR), uintptr(state.sepY-notchR),
		uintptr(state.cardX+notchR), uintptr(state.sepY+notchR),
	)
	procEllipse.Call(hdc,
		uintptr(state.cardX+state.cardW-notchR), uintptr(state.sepY-notchR),
		uintptr(state.cardX+state.cardW+notchR), uintptr(state.sepY+notchR),
	)
	procSelectObject.Call(hdc, oldNotchBrush)
	procDeleteObject.Call(notchBrush)
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)

	pad := scaleValue(20, dpi)
	cardTextR := func(top, bottom int32) rectI {
		return rectI{
			Left:   state.cardX + int32(pad),
			Top:    top,
			Right:  state.cardX + state.cardW - int32(pad),
			Bottom: bottom,
		}
	}

	// Dòng 1: "ZCODE" (đậm trái) + tên plan (xanh phải).
	headerH := int32(scaleValue(20, dpi))
	headerY := state.cardY + int32(scaleValue(18, dpi))
	halfW := (state.cardW - int32(2*pad)) / 2
	drawTextBlock(hdc, fontLabel.handle, "ZCODE", rectI{
		Left: state.cardX + int32(pad), Top: headerY,
		Right: state.cardX + int32(pad) + halfW, Bottom: headerY + headerH,
	}, colTextDark, dtSingle)
	if state.planName != "" {
		drawTextBlock(hdc, fontLabel.handle, state.planName, rectI{
			Left: state.cardX + int32(pad) + halfW, Top: headerY,
			Right: state.cardX + state.cardW - int32(pad), Bottom: headerY + headerH,
		}, colAccent, dtSingle|dtCenter)
	}

	// Số token lớn (đen đậm) + dòng benefit (xám đậm).
	contentY := headerY + headerH + int32(scaleValue(10, dpi))
	bigH := int32(scaleValue(34, dpi))
	if state.quota != nil {
		drawTextBlock(hdc, fontBig.handle, state.quota.Amount, cardTextR(contentY, contentY+bigH), colTextDark, dtSingle)
		contentY += bigH + int32(scaleValue(6, dpi))
		drawTextBlock(hdc, fontBody.handle, state.quota.Benefit, cardTextR(contentY, contentY+int32(scaleValue(20, dpi))), colTextMid, dtSingle)
	} else if state.planName != "" {
		drawTextBlock(hdc, fontBig.handle, state.planName, cardTextR(contentY, contentY+bigH), colTextDark, dtSingle)
	}

	// Đường perforation: các nét đứt ngắn giữa 2 notch.
	dashY := state.sepY
	dashFrom := state.cardX + notchR + int32(scaleValue(6, dpi))
	dashTo := state.cardX + state.cardW - notchR - int32(scaleValue(6, dpi))
	dashBrush, _, _ := procCreateSolidBrush.Call(uintptr(colSepGrey))
	for dx := dashFrom; dx < dashTo; dx += int32(scaleValue(10, dpi)) {
		dashR := rectI{Left: dx, Top: dashY, Right: dx + int32(scaleValue(5, dpi)), Bottom: dashY + 1}
		procFillRect.Call(hdc, dashBrush, uintptr(unsafe.Pointer(&dashR)))
	}
	procDeleteObject.Call(dashBrush)

	// "Hạn dùng đến" + thời gian kết thúc (ends_at epoch giây →
	// toLocaleString kiểu en-US như app gốc).
	belowSep := state.sepY + int32(scaleValue(12, dpi))
	drawTextBlock(hdc, fontSmall.handle, "Hạn dùng đến", cardTextR(belowSep, belowSep+int32(scaleValue(18, dpi))), colTextMid, dtSingle)
	if state.endsAt > 0 {
		drawTextBlock(hdc, fontBody.handle,
			time.Unix(state.endsAt, 0).Format("01/02/2006, 3:04:05 PM"),
			cardTextR(belowSep+int32(scaleValue(18, dpi)), belowSep+int32(scaleValue(40, dpi))),
			colTextDark, dtSingle)
	}

	// Heading + subline dưới card (trên nền tối).
	headingY := state.cardY + state.cardH + int32(scaleValue(24, dpi))
	drawTextBlock(hdc, fontHeading.handle, state.heading, rectI{
		Left: rc.Left + int32(scaleValue(20, dpi)), Top: headingY,
		Right: rc.Right - int32(scaleValue(20, dpi)), Bottom: headingY + int32(scaleValue(26, dpi)),
	}, colTextWhite, dtSingle|dtCenter)
	if state.subline != "" {
		drawTextBlock(hdc, fontSub.handle, state.subline, rectI{
			Left: rc.Left + int32(scaleValue(20, dpi)), Top: headingY + int32(scaleValue(30, dpi)),
			Right: rc.Right - int32(scaleValue(20, dpi)), Bottom: headingY + int32(scaleValue(52, dpi)),
		}, colTextGrey, dtSingle|dtCenter)
	}
}

// drawStars vẽ các chấm sao mờ cố định (LCG seed cố định để không
// nhấp nháy khi repaint) — gợi nền trời đêm của dialog app.
func drawStars(hdc uintptr, rc rectI) {
	seed := uint32(0x9E3779B9)
	next := func(max int32) int32 {
		seed = seed*1664525 + 1013904223
		return int32(seed>>16) % max
	}
	for i := 0; i < 70; i++ {
		x := rc.Left + next(rc.Right-rc.Left)
		y := rc.Top + next(rc.Bottom-rc.Top)
		if y > rc.Bottom/2 {
			// Sao tập trung nửa trên như ảnh app.
			y = y / 2
		}
		color := uint32(colStarDim)
		if i%7 == 0 {
			color = uint32(colTextGrey)
		}
		procSetPixel.Call(hdc, uintptr(uint32(x)), uintptr(uint32(y)), uintptr(color))
	}
}
