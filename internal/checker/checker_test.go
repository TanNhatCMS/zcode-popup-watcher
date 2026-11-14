package checker

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestParseSnapshotDedupesByPosition(t *testing.T) {
	payload := []byte(`{"code":0,"data":{"deliveries":[
		{"campaign_id":"night-1","resource_position":"banner","priority":90,"banner":{"buttons":[{"text":{"content":"Nhận ngay"}}],"success_popup":{"title":{"content":"Deal đêm"}}}},
		{"campaign_id":"night-2","resource_position":"banner","priority":80,"banner":{"buttons":[]}},
		{"campaign_id":"pop-1","resource_position":"popup","priority":70,"popup":{"title":{"content":"Deal popup"},"description":{"content":"300M token"},"buttons":[{"text":{"content":"Nhận"}}]}}
	]}}`)
	deals, err := ParseSnapshot(payload)
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	if len(deals) != 2 {
		t.Fatalf("got %d deals, want 2 (banner trùng vị trí bị loại)", len(deals))
	}
	if deals[0].Key != "banner:night-1" || deals[1].Key != "popup:pop-1" {
		t.Fatalf("unexpected keys: %v", deals)
	}
	if deals[0].Title != "Deal đêm" || deals[1].Detail == "" {
		t.Fatalf("unexpected title/detail: %+v", deals)
	}
}

func TestParseSnapshotRejectsBadCode(t *testing.T) {
	if _, err := ParseSnapshot([]byte(`{"code":1,"data":{}}`)); err == nil {
		t.Fatal("expected error for code != 0")
	}
}

func TestParseSnapshotExtractsClaimAction(t *testing.T) {
	payload := []byte(`{"code":0,"data":{"deliveries":[
		{"campaign_id":"banner-claim","priority":80,"resource_position":"banner","banner":{
			"buttons":[
				{"text":{"content":"Nhận ngay"},"action":{"type":"claim_zcode_plan","args":{"plan_id":"plan-abc-123"}}}
			],
			"success_popup":{"title":{"content":"Nhận thành công"}}}},
		{"campaign_id":"pop-claim","priority":90,"resource_position":"popup","popup":{
			"title":{"content":"Deal đêm"},
			"description":{"content":"300M token"},
			"buttons":[
				{"text":{"content":"Đóng"},"action":{"type":"close"}},
				{"text":{"content":"Nhận ngay"},"action":{"type":"claim_zcode_plan","args":{"plan_id":"plan-xyz-9"}}},
				{"text":{"content":"Mở"},"action":{"type":"open_url","args":{"url":"https://zcode.z.ai/deal"}}},
				{"text":{"content":"Mở lạ"},"action":{"type":"open_url","args":{"url":"javascript:alert(1)"}}}
			]}},
		{"campaign_id":"pop-duplicate","priority":10,"resource_position":"popup","popup":{"title":{"content":"Bị loại trùng vị trí"},"buttons":[]}}
	]}}`)
	deals, err := ParseSnapshot(payload)
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	if len(deals) != 2 {
		t.Fatalf("got %d deals, want 2 (popup trùng vị trí bị loại)", len(deals))
	}
	banner := deals[0]
	if banner.Key != "banner:banner-claim" {
		t.Fatalf("key = %q", banner.Key)
	}
	if banner.PlanID != "plan-abc-123" || banner.ButtonLabel != "Nhận ngay" {
		t.Fatalf("banner claim parsed sai: %+v", banner)
	}
	claim := deals[1]
	if claim.PlanID != "plan-xyz-9" {
		t.Fatalf("PlanID = %q, want plan-xyz-9", claim.PlanID)
	}
	if claim.ButtonLabel != "Nhận ngay" {
		t.Fatalf("ButtonLabel = %q, want \"Nhận ngay\"", claim.ButtonLabel)
	}
	if claim.ClaimURL != "" {
		// Có nút claim thì link open_url bị bỏ qua (popup của tool chỉ
		// có nút claim / Mở ZCode / Để sau).
		t.Fatalf("ClaimURL phải rỗng khi có claim, got %q", claim.ClaimURL)
	}
	if !claim.Claimable() {
		t.Fatal("deal claim phải Claimable()")
	}

	// Deal thuần link (open_url, không có claim): link http/https được
	// giữ để tool mở, scheme lạ bị chặn như schema app gốc.
	linkPayload := []byte(`{"code":0,"data":{"deliveries":[
		{"campaign_id":"pop-link","priority":70,"resource_position":"popup","popup":{
			"title":{"content":"Deal link"},
			"buttons":[{"text":{"content":"Mở"},"action":{"type":"open_url","args":{"url":"https://zcode.z.ai/deal"}}}]}}
	]}}`)
	linkDeals, err := ParseSnapshot(linkPayload)
	if err != nil {
		t.Fatalf("ParseSnapshot(link): %v", err)
	}
	if len(linkDeals) != 1 || linkDeals[0].ClaimURL != "https://zcode.z.ai/deal" || linkDeals[0].Claimable() != true {
		t.Fatalf("link deal parsed sai: %+v", linkDeals)
	}
	badPayload := []byte(`{"code":0,"data":{"deliveries":[
		{"campaign_id":"pop-bad-link","priority":60,"resource_position":"popup","popup":{
			"title":{"content":"Link lạ"},
			"buttons":[{"text":{"content":"Mở"},"action":{"type":"open_url","args":{"url":"javascript:alert(1)"}}}]}}
	]}}`)
	badDeals, err := ParseSnapshot(badPayload)
	if err != nil {
		t.Fatalf("ParseSnapshot(bad): %v", err)
	}
	if len(badDeals) != 1 || badDeals[0].Claimable() {
		t.Fatalf("link non-http phải bị bỏ qua: %+v", badDeals)
	}
}

func TestClientPlatformKey(t *testing.T) {
	// X-Platform phải là chuỗi "process.platform-arch" của Node, không
	// phải của Go: windows/amd64 → win32-x64 (app gốc gửi như vậy).
	want := nodePlatform(runtime.GOOS) + "-"
	switch runtime.GOARCH {
	case "amd64":
		want += "x64"
	case "386":
		want += "ia32"
	default:
		want += runtime.GOARCH
	}
	if got := clientPlatformKey(); got != want {
		t.Fatalf("clientPlatformKey() = %q, want %q", got, want)
	}
}

func TestNodePlatform(t *testing.T) {
	if got := nodePlatform("windows"); got != "win32" {
		t.Fatalf("nodePlatform(windows) = %q, want win32 (ZCode dùng win32 trong fallback secret)", got)
	}
	if got := nodePlatform("darwin"); got != "darwin" {
		t.Fatalf("nodePlatform(darwin) = %q, want darwin", got)
	}
	if got := nodePlatform("linux"); got != "linux" {
		t.Fatalf("nodePlatform(linux) = %q, want linux", got)
	}
}

func TestDecryptCredentialRoundTrip(t *testing.T) {
	// Mã hóa bằng dùng scheme của tool: AES-256-GCM, key SHA256
	// của fallback secret hiện tại (không đặt ZCODE_CREDENTIAL_SECRET).
	home, _ := os.UserHomeDir()
	secret := fmt.Sprintf("zcode-credential-fallback:%s:%s:%s", runtimeGOOS(), home, currentUsername())
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}
	sealed := gcm.Seal(nil, iv, []byte("test-jwt-123"), nil)
	cipherText, authTag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	enc := "enc:v1:" + base64.RawURLEncoding.EncodeToString(iv) + "." +
		base64.RawURLEncoding.EncodeToString(authTag) + "." +
		base64.RawURLEncoding.EncodeToString(cipherText)
	plain, err := DecryptCredential(enc)
	if err != nil {
		t.Fatalf("DecryptCredential: %v", err)
	}
	if plain != "test-jwt-123" {
		t.Fatalf("got %q, want test-jwt-123", plain)
	}
	if plain2, err := DecryptCredential("already-plain"); err != nil || plain2 != "already-plain" {
		t.Fatalf("plaintext passthrough broken: %q %v", plain2, err)
	}
}
