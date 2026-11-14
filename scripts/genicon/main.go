// Command genicon vẽ icon của app zcode-deal và ghi ra
// assets/app-icon.ico (đa kích thước: 256/64/48/32/16, 32bpp + alpha)
// cùng assets/app-icon.png (bản 256 xem trước).
//
// Thiết kế khớp UI của tool: nền navy bo góc (giữ chiều popup/vé),
// chuông trắng (thông báo deal) + chấm cam (có deal mới) — giống
// banner "300,000,000 tokens" của app ZCode.
//
// Chạy: go run ./scripts/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

type rgba = color.RGBA

var (
	colNavyTop = rgba{R: 14, G: 21, B: 46, A: 255}
	colNavyBot = rgba{R: 7, G: 10, B: 24, A: 255}
	colBell    = rgba{R: 245, G: 248, B: 255, A: 255}
	colDot     = rgba{R: 245, G: 158, B: 11, A: 255} // cam #F59E0B
)

// icoImage là 1 ảnh con trong file .ico (PNG cho 256, DIB cho nhỏ).
type icoImage struct {
	w, h int
	data []byte
}

func main() {
	// Vẽ ở 512 rồi thu nhỏ để có viền mượt (AA miễn phí).
	outDir := "assets"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	// 1) Icon app (exe resource + preview PNG): chuông + chấm cam.
	// 256 dùng bản chi tiết; các size nhỏ (48/32/16) dùng bản compact
	// để title bar/tray vẫn đọc ra là chuông.
	writeICO(filepath.Join(outDir, "app-icon.ico"), true, true)
	writePNG(filepath.Join(outDir, "app-icon.png"), true)

	// 2) Icon tray: thường (chuông trơn) + alert (chuông + chấm cam)
	// — khớp thiết kế app, thay bộ icon cũ của tray.
	writeICO(filepath.Join(outDir, "icon.ico"), false, false)
	writeICO(filepath.Join(outDir, "icon-alert.ico"), true, false)

	// Tự kiểm tra: 4 góc icon phải trong suốt (alpha 0) — nếu resize
	// làm mất alpha, icon trong tray sẽ hiện thành ô vuông đen.
	chk := resize(drawIcon(512, true, false), 256, 256)
	for _, pt := range [][2]int{{0, 0}, {255, 0}, {0, 255}, {255, 255}, {6, 6}} {
		if a := chk.RGBAAt(pt[0], pt[1]).A; a != 0 {
			log.Fatalf("góc icon (%d,%d) không trong suốt: alpha = %d", pt[0], pt[1], a)
		}
	}
	log.Print("Đã tạo assets/app-icon.ico, app-icon.png, icon.ico, icon-alert.ico (góc trong suốt OK)")
}

// writePNG ghi bản xem trước 256 (bản chi tiết).
func writePNG(path string, withDot bool) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, resize(drawIcon(512, withDot, false), 256, 256)); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}

// writeICO ghi file .ico; withPNG256 nhúng thêm entry 256 dạng PNG
// (chuẩn cho kích thước lớn nhất). 64px dùng bản chi tiết, ≤48px dùng
// bản compact.
func writeICO(path string, withDot, withPNG256 bool) {
	var entries []icoImage
	if withPNG256 {
		entries = append(entries, icoImage{w: 256, h: 256, data: pngBytes(drawIcon(512, withDot, false))})
	}
	entries = append(entries, icoImage{w: 64, h: 64, data: dibFrom(resize(drawIcon(512, withDot, false), 64, 64))})
	for _, size := range []int{48, 32, 16} {
		entries = append(entries, icoImage{w: size, h: size, data: dibFrom(resize(drawIcon(512, withDot, true), size, size))})
	}
	if err := os.WriteFile(path, buildICO(entries), 0o644); err != nil {
		log.Fatal(err)
	}
}

func pngBytes(src *image.RGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, resize(src, 256, 256)); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// drawIcon vẽ icon ở kích thước bất kỳ (tọa độ scale theo size/512).
// withDot: chấm cam "có deal mới". compact: bố cục rút gọn cho kích
// thước nhỏ (≤48px — title bar/tray/Explorer) — khối to, tương phản,
// ít chi tiết mịn để ở 16px vẫn đọc ra là chuông; co thiết kế chi
// tiết xuống 16px sẽ thành cục trắng mờ.
func drawIcon(size int, withDot, compact bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size) / 512.0
	px := func(v int) float64 { return float64(v) * s }

	// Nền navy bo góc, gradient dọc nhẹ cho có chiều sâu.
	roundRectGradient(img, px(8), px(8), px(504), px(504), px(104), colNavyTop, colNavyBot)

	if compact {
		// Chuông to bản: núm → vòm → thân → loa dày → thanh đáy → lục lạc.
		fillCircle(img, px(256), px(90), px(24), colBell)                   // núm
		fillCircle(img, px(256), px(238), px(128), colBell)                 // vòm
		fillRect(img, px(128), px(238), px(384), px(292), colBell)          // thân
		fillEllipse(img, px(256), px(304), px(152), px(44), colBell)        // loa
		roundRect(img, px(110), px(350), px(402), px(382), px(14), colBell) // thanh đáy
		fillCircle(img, px(256), px(416), px(28), colBell)                  // lục lạc
	} else {
		// Bản chi tiết cho kích thước lớn (≥64px).
		fillCircle(img, px(256), px(96), px(18), colBell)                   // núm trên
		fillCircle(img, px(256), px(224), px(114), colBell)                 // vòm
		fillRect(img, px(142), px(224), px(370), px(288), colBell)          // thân
		fillEllipse(img, px(256), px(296), px(140), px(38), colBell)        // loa
		roundRect(img, px(116), px(316), px(396), px(340), px(10), colBell) // thanh đáy
		fillCircle(img, px(256), px(362), px(24), colBell)                  // lục lạc
	}
	if withDot {
		// Chấm cam "có deal mới" — viền navy tách khỏi chuông.
		ringCol := colNavyTop
		if compact {
			fillCircle(img, px(402), px(124), px(66), ringCol)
			fillCircle(img, px(402), px(124), px(52), colDot)
		} else {
			fillCircle(img, px(396), px(150), px(56), ringCol)
			fillCircle(img, px(396), px(150), px(46), colDot)
		}
	}
	return img
}

// ---- primitives vẽ cơ bản (không anti-alias: vẽ 2x rồi thu nhỏ) ----

func fillRect(img *image.RGBA, x0, y0, x1, y1 float64, col rgba) {
	for y := int(y0); y < int(y1); y++ {
		for x := int(x0); x < int(x1); x++ {
			set(img, x, y, col)
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r float64, col rgba) {
	fillEllipse(img, cx, cy, r, r, col)
}

func fillEllipse(img *image.RGBA, cx, cy, rx, ry float64, col rgba) {
	for y := int(cy - ry); y <= int(cy+ry); y++ {
		for x := int(cx - rx); x <= int(cx+rx); x++ {
			dx := (float64(x) + 0.5 - cx) / rx
			dy := (float64(y) + 0.5 - cy) / ry
			if dx*dx+dy*dy <= 1 {
				set(img, x, y, col)
			}
		}
	}
}

func roundRect(img *image.RGBA, x0, y0, x1, y1, r float64, col rgba) {
	fillRect(img, x0+r, y0, x1-r, y1, col)
	fillRect(img, x0, y0+r, x1, y1-r, col)
	fillCircle(img, x0+r, y0+r, r, col)
	fillCircle(img, x1-r, y0+r, r, col)
	fillCircle(img, x0+r, y1-r, r, col)
	fillCircle(img, x1-r, y1-r, r, col)
}

func roundRectGradient(img *image.RGBA, x0, y0, x1, y1, r float64, top, bot rgba) {
	for y := int(y0); y < int(y1); y++ {
		t := (float64(y) - y0) / (y1 - y0)
		row := lerp(top, bot, t)
		for x := int(x0); x < int(x1); x++ {
			// Chỉ tô điểm nằm trong hình bo góc: với mỗi góc kiểm tra
			// khoảng cách tới tâm tròn bo.
			if !inRoundRect(float64(x)+0.5, float64(y)+0.5, x0, y0, x1, y1, r) {
				continue
			}
			set(img, x, y, row)
		}
	}
}

func inRoundRect(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	cx := clamp(px, x0+r, x1-r)
	cy := clamp(py, y0+r, y1-r)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func lerp(a, b rgba, t float64) rgba {
	return rgba{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

func set(img *image.RGBA, x, y int, col rgba) {
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return
	}
	img.SetRGBA(x, y, col)
}

// resize thu nhỏ bilinear với PREMULTIPLIED alpha: trộn RGBA thẳng
// sẽ làm pixel trong suốt (đen, a=0) loang vào viền tạo fringe đen và
// mất trong suốt ở góc bo. Quy trình: premultiply → blend →
// unpremultiply; alpha nội suy theo để góc ngoài icon thật sự trong suốt.
func resize(src *image.RGBA, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	// sample trả về [R, G, B, A] dạng premultiplied (RGB đã nhân alpha).
	sample := func(x, y int) [4]float64 {
		c := src.RGBAAt(x, y)
		a := float64(c.A)
		return [4]float64{
			float64(c.R) / 255 * a,
			float64(c.G) / 255 * a,
			float64(c.B) / 255 * a,
			a,
		}
	}
	for y := 0; y < h; y++ {
		fy := (float64(y) + 0.5) * float64(sh) / float64(h)
		y0 := int(fy)
		y1 := minInt(y0+1, sh-1)
		ty := fy - float64(y0)
		for x := 0; x < w; x++ {
			fx := (float64(x) + 0.5) * float64(sw) / float64(w)
			x0 := int(fx)
			x1 := minInt(x0+1, sw-1)
			tx := fx - float64(x0)
			s00 := sample(x0, y0)
			s10 := sample(x1, y0)
			s01 := sample(x0, y1)
			s11 := sample(x1, y1)
			var out [4]float64
			for i := range out {
				out[i] = lerpF(lerpF(s00[i], s10[i], tx), lerpF(s01[i], s11[i], tx), ty)
			}
			a := clampF(out[3], 0, 255)
			var r, g, b uint8
			if a > 0 {
				r = uint8(clampF(out[0]/a*255, 0, 255))
				g = uint8(clampF(out[1]/a*255, 0, 255))
				b = uint8(clampF(out[2]/a*255, 0, 255))
			}
			dst.SetRGBA(x, y, rgba{R: r, G: g, B: b, A: uint8(a)})
		}
	}
	return dst
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func lerpF(a, b, t float64) float64 { return a + (b-a)*t }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- ICO writer: ICONDIR + ICONDIRENTRY[] + dữ liệu ----

func buildICO(entries []icoImage) []byte {
	var buf bytes.Buffer
	header := make([]byte, 6)
	binary.LittleEndian.PutUint16(header[0:], 0) // reserved
	binary.LittleEndian.PutUint16(header[2:], 1) // type icon
	binary.LittleEndian.PutUint16(header[4:], uint16(len(entries)))
	buf.Write(header)

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		w8, h8 := uint8(e.w%256), uint8(e.h%256) // 256 → 0
		dir := make([]byte, 16)
		dir[0], dir[1] = w8, h8
		binary.LittleEndian.PutUint16(dir[4:], 1)  // planes
		binary.LittleEndian.PutUint16(dir[6:], 32) // bpp
		binary.LittleEndian.PutUint32(dir[8:], uint32(len(e.data)))
		binary.LittleEndian.PutUint32(dir[12:], uint32(offset))
		buf.Write(dir)
		offset += len(e.data)
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return buf.Bytes()
}

// dibFrom chuyển RGBA sang BITMAPINFOHEADER(40) + XOR BGRA bottom-up +
// AND mask 1bpp — định dạng entry ICO cho kích thước nhỏ (không dùng
// PNG cho size nhỏ để tương thích tối đa).
func dibFrom(img *image.RGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	andStride := ((w + 31) / 32) * 4
	total := 40 + w*h*4 + andStride*h
	out := make([]byte, total)

	// BITMAPINFOHEADER; height ghi ×2 (XOR + AND).
	binary.LittleEndian.PutUint32(out[0:], 40)
	binary.LittleEndian.PutUint32(out[4:], uint32(w))
	binary.LittleEndian.PutUint32(out[8:], uint32(h*2))
	binary.LittleEndian.PutUint16(out[12:], 1)  // planes
	binary.LittleEndian.PutUint16(out[14:], 32) // bpp

	// XOR: BGRA, hàng từ DƯỚI lên.
	p := 40
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			out[p] = c.B
			out[p+1] = c.G
			out[p+2] = c.R
			out[p+3] = c.A
			p += 4
		}
	}
	// AND mask: 1bpp, hàng từ dưới lên, tất cả 0 (alpha 32bpp quyết định).
	return out
}
