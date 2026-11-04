package checker

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
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
