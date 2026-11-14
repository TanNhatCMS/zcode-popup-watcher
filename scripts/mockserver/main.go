// Command mockserver là server mẫu cục bộ cho việc test zcode-deal
// không cần chạm vào API thật. Chạy:
//
//	go run ./scripts/mockserver -addr 127.0.0.1:8931
//
// rồi để tool gọi qua biến môi trường:
//
//	ZCODE_MARKETING_URL=http://127.0.0.1:8931 zcode-deal check --once
//
// Endpoint mô phỏng đúng app gốc:
//   - GET  /api/v1/marketing/touch?seq=N   → popup deal có ảnh hero +
//     description dạng HTML (có <script> để chứng minh tool strip đúng
//     như marketingLabel của app) + nút claim
//   - GET  /mock-assets/hero.png           → ảnh banner (PNG tự vẽ)
//   - POST /api/v1/marketing/touch/action  → report confirm/cancel
//   - GET  /api/v1/zcode-plan/billing/preview → danh sách plan
//   - POST /api/v1/zcode-plan/billing/claim   → nhận plan thành công
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
)

// heroPNG + heroSHA256 precompute lúc start để payload trả src/sha256
// khớp nhau (giống asset gate của app gốc).
var (
	heroPNG []byte
	heroSHA string
)

func buildHeroPNG() {
	const w, h = 480, 120
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Gradient xanh → cam cho giống banner khuyến mãi.
			t := float64(x) / float64(w-1)
			r := uint8(30 + t*220)
			g := uint8(90 + t*60)
			b := uint8(200 - t*140)
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatalf("không vẽ được ảnh hero: %v", err)
	}
	heroPNG = buf.Bytes()
	sum := sha256.Sum256(heroPNG)
	heroSHA = hex.EncodeToString(sum[:])
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8931", "địa chỉ lắng nghe")
	flag.Parse()
	buildHeroPNG()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/marketing/touch", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GET touch seq=%s deviceMid=%s lang=%s ver=%s auth=%v",
			r.URL.Query().Get("seq"), r.Header.Get("X-Device-Mid"),
			r.Header.Get("X-Client-Language"), r.Header.Get("X-ZCode-App-Version"),
			r.Header.Get("Authorization") != "")
		heroSrc := "http://" + r.Host + "/mock-assets/hero.png"
		writeJSON(w, map[string]any{
			"code": 0,
			"data": map[string]any{
				"server_time": 1793000000,
				"language":    "en-US",
				"deliveries": []any{
					map[string]any{
						"campaign_id":       "mock-deal-1",
						"priority":          90,
						"resource_position": "popup",
						"popup": map[string]any{
							"title": map[string]any{
								"format":  "html",
								"content": "<b>Deal đêm mock</b><script>alert('phải bị strip')</script>",
							},
							"description": map[string]any{
								"format":  "html",
								"content": "<p>Nhận <b>300M token GLM-5.3-Flash</b> dùng đêm nay.</p><ul><li>Hết hạn 08:00 sáng mai</li><li>Dùng cho model GLM-5.3-Flash</li></ul><script>alert('phải bị strip')</script>",
							},
							"hero": map[string]any{
								"type": "image",
								"image": map[string]any{
									"default": map[string]any{"src": heroSrc, "sha256": heroSHA},
								},
								// Dữ liệu tấm vé sau khi nhận — giống
								// heroData() của app gốc đọc từ đây.
								"args": map[string]any{
									"zcode_plan": map[string]any{
										"name":    "ZCODE GLOBAL BUILD",
										"ends_at": 1793280000,
										"entitlements": []any{
											map[string]any{
												"meter":       "model_usage",
												"show_name":   "GLM-5.3-Flash",
												"grant_units": 300000000.0,
												"unit_type":   "Tokens",
											},
										},
									},
								},
							},
							"buttons": []any{
								map[string]any{
									"text":   map[string]any{"format": "plaintext", "content": "Nhận ngay"},
									"action": map[string]any{"type": "claim_zcode_plan", "args": map[string]any{"plan_id": "mock-plan-1"}},
								},
								map[string]any{
									"text":   map[string]any{"format": "plaintext", "content": "Đóng"},
									"action": map[string]any{"type": "close"},
								},
							},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/mock-assets/hero.png", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GET %s (sha256 khớp payload: %v)", r.URL.Path, sha256Hex(heroPNG) == heroSHA)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(heroPNG)
	})
	mux.HandleFunc("/api/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GET oauth/userinfo auth=%v", r.Header.Get("Authorization") != "")
		// Giống response OIDC userinfo của app gốc: user ở root hoặc
		// trong data — tool chấp nhận cả hai.
		writeJSON(w, map[string]any{
			"sub":                "mock-user-1",
			"name":               "TanNhat (mock)",
			"preferred_username": "tannhat",
			"email":              "tannhat@mock.local",
		})
	})
	mux.HandleFunc("/api/v1/marketing/touch/action", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log.Printf("POST touch/action body=%s auth=%v", body, r.Header.Get("Authorization") != "")
		writeJSON(w, map[string]any{"code": 0})
	})
	mux.HandleFunc("/api/v1/zcode-plan/billing/preview", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GET billing/preview platform=%s ver=%s auth=%v",
			r.URL.Query().Get("platform"), r.URL.Query().Get("app_version"), r.Header.Get("Authorization") != "")
		writeJSON(w, map[string]any{
			"code": 0,
			"data": map[string]any{
				"server_time": 1793000000,
				"plans": []any{
					map[string]any{
						"plan_id":     "mock-plan-1",
						"name":        "ZCODE GLOBAL BUILD",
						"description": "Plan mẫu từ server cục bộ",
						"priority":    90,
						"entitlements": []any{
							map[string]any{
								"entitlement_id": "ent-1",
								"show_name":      "GLM-5.3-Flash",
								"meter":          "model_usage",
								"unit_type":      "Tokens",
								"period":         "day",
								"grant_units":    300000000.0,
							},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/zcode-plan/billing/claim", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log.Printf("POST billing/claim body=%s platform=%s auth=%v", body, r.Header.Get("X-Platform"), r.Header.Get("Authorization") != "")
		var req struct {
			PlanID string `json:"plan_id"`
		}
		_ = json.Unmarshal(body, &req)
		if req.PlanID != "mock-plan-1" {
			writeJSON(w, map[string]any{"code": 1001, "data": map[string]any{"message": "plan không tồn tại (mock)"}})
			return
		}
		writeJSON(w, map[string]any{
			"code": 0,
			"data": map[string]any{
				"server_time": 1793000000,
				"plan": map[string]any{
					"plan_id":     "mock-plan-1",
					"name":        "ZCODE GLOBAL BUILD",
					"description": "Plan mẫu từ server cục bộ",
					"priority":    90,
					"ends_at":     1793280000,
					"entitlements": []any{
						map[string]any{
							"entitlement_id": "ent-1",
							"show_name":      "GLM-5.3-Flash",
							"meter":          "model_usage",
							"unit_type":      "Tokens",
							"period":         "day",
							"grant_units":    300000000.0,
						},
					},
				},
			},
		})
	})

	log.Printf("Mock ZCode marketing server đang chạy tại http://%s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(value)
	_, _ = w.Write(data)
	fmt.Println()
}
