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
//   - GET  /api/v1/marketing/touch?seq=N   → 1 popup deal có nút claim
//   - POST /api/v1/marketing/touch/action  → report confirm/cancel
//   - GET  /api/v1/zcode-plan/billing/preview → danh sách plan
//   - POST /api/v1/zcode-plan/billing/claim   → nhận plan thành công
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8931", "địa chỉ lắng nghe")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/marketing/touch", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GET touch seq=%s deviceMid=%s lang=%s ver=%s auth=%v",
			r.URL.Query().Get("seq"), r.Header.Get("X-Device-Mid"),
			r.Header.Get("X-Client-Language"), r.Header.Get("X-ZCode-App-Version"),
			r.Header.Get("Authorization") != "")
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
							"title":       text("Deal đêm mock"),
							"description": text("300M token GLM-5.3-Flash (server mẫu cục bộ)"),
							"buttons": []any{
								map[string]any{
									"text":   text("Nhận ngay"),
									"action": map[string]any{"type": "claim_zcode_plan", "args": map[string]any{"plan_id": "mock-plan-1"}},
								},
								map[string]any{
									"text":   text("Đóng"),
									"action": map[string]any{"type": "close"},
								},
							},
						},
					},
				},
			},
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
						"name":        "Deal đêm mock",
						"description": "Plan mẫu từ server cục bộ",
						"priority":    90,
						"entitlements": []any{
							map[string]any{
								"entitlement_id": "ent-1",
								"show_name":      "300M token GLM-5.3-Flash",
								"meter":          "tokens",
								"unit_type":      "M",
								"period":         "day",
								"grant_units":    300,
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
					"name":        "Deal đêm mock",
					"description": "Plan mẫu từ server cục bộ",
					"priority":    90,
					"entitlements": []any{
						map[string]any{
							"entitlement_id": "ent-1",
							"show_name":      "300M token GLM-5.3-Flash",
							"meter":          "tokens",
							"unit_type":      "M",
							"period":         "day",
							"grant_units":    300,
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

func text(content string) map[string]any {
	return map[string]any{"format": "plaintext", "content": content}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(value)
	_, _ = w.Write(data)
	fmt.Println()
}
