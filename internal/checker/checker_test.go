package checker

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func TestPlainTextHTMLStripsScript(t *testing.T) {
	// marketingLabel của app: bỏ cả cây script/style, giữ text còn lại.
	got := plainText("html", "<p>Deal <b>đêm</b> 300M</p><script>alert(1)</script><style>.x{color:red}</style>")
	want := "Deal đêm 300M"
	if got != want {
		t.Fatalf("plainText(html) = %q, want %q", got, want)
	}
}

func TestPlainTextMarkdown(t *testing.T) {
	got := plainText("markdown", "## Deal đêm\nNhận **300M token** [tại đây](https://x.y) ngay `sau` 20h")
	want := "Deal đêm\nNhận 300M token tại đây ngay sau 20h"
	if got != want {
		t.Fatalf("plainText(markdown) = %q, want %q", got, want)
	}
}

func TestPlainTextPlaintextPassthrough(t *testing.T) {
	// Đường đi thật: formattedText đọc {format, content} rồi trim,
	// plainText giữ nguyên content plaintext.
	format, content := formattedText(map[string]any{"format": "plaintext", "content": "  thô  "})
	if format != "plaintext" || content != "thô" {
		t.Fatalf("formattedText = (%q, %q)", format, content)
	}
	if got := plainText(format, content); got != "thô" {
		t.Fatalf("plainText = %q", got)
	}
}

func TestParseSnapshotHeroAndFormattedDesc(t *testing.T) {
	payload := []byte(`{"code":0,"data":{"deliveries":[
		{"campaign_id":"pop-hero","priority":90,"resource_position":"popup","popup":{
			"title":{"format":"html","content":"<b>Deal đêm</b> <script>alert(1)</script>"},
			"description":{"format":"markdown","content":"Nhận **300M token**"},
			"hero":{"type":"image","image":{"default":{"src":"https://cdn.z.ai/hero.png","sha256":"aabbccdd"}}},
			"buttons":[{"text":{"content":"Nhận"},"action":{"type":"claim_zcode_plan","args":{"plan_id":"plan-h1"}}}]}},
		{"campaign_id":"ban-hero","priority":80,"resource_position":"banner","banner":{
			"background":{"type":"image","image":{"default":{"src":"https://cdn.z.ai/banner.png","sha256":"11223344"}}},
			"buttons":[{"text":{"content":"Nhận ngay"},"action":{"type":"claim_zcode_plan","args":{"plan_id":"plan-b1"}}}]}}
	]}}`)
	deals, err := ParseSnapshot(payload)
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	if len(deals) != 2 {
		t.Fatalf("got %d deals, want 2", len(deals))
	}
	popup := deals[0]
	if popup.Title != "Deal đêm" {
		t.Fatalf("Title html strip sai: %q", popup.Title)
	}
	if popup.Desc != "Nhận 300M token" {
		t.Fatalf("Desc markdown strip sai: %q", popup.Desc)
	}
	if popup.HeroSrc != "https://cdn.z.ai/hero.png" || popup.HeroSHA256 != "aabbccdd" {
		t.Fatalf("hero popup sai: %q / %q", popup.HeroSrc, popup.HeroSHA256)
	}
	banner := deals[1]
	if banner.HeroSrc != "https://cdn.z.ai/banner.png" || banner.HeroSHA256 != "11223344" {
		t.Fatalf("hero banner (background) sai: %q / %q", banner.HeroSrc, banner.HeroSHA256)
	}
	if banner.Title != "" {
		// Banner không có title riêng (chỉ success_popup); payload này
		// không khai báo success_popup nên title rỗng là đúng.
		t.Fatalf("banner title phải rỗng, got %q", banner.Title)
	}
}

func TestParseAccountUser(t *testing.T) {
	// Dạng 1: user ở root (OIDC userinfo của Z.ai global).
	info, err := parseAccountUser(map[string]any{
		"sub":                "u1",
		"name":               "TanNhat",
		"preferred_username": "tannhat",
		"email":              "tannhat@example.com",
	})
	if err != nil {
		t.Fatalf("parse root user: %v", err)
	}
	if info.Username != "TanNhat" || info.Email != "tannhat@example.com" || info.ID != "u1" {
		t.Fatalf("root user sai: %+v", info)
	}
	// Dạng 2: user trong data (profile của Z.ai CN, id thay bằng user_id).
	info2, err := parseAccountUser(map[string]any{
		"code": 0,
		"data": map[string]any{"user_id": "x9", "name": "CN user"},
	})
	if err != nil {
		t.Fatalf("parse data user: %v", err)
	}
	if info2.ID != "x9" || info2.Username != "CN user" {
		t.Fatalf("data user sai: %+v", info2)
	}
	// Không có thông tin user → lỗi.
	if _, err := parseAccountUser(map[string]any{"code": 0}); err == nil {
		t.Fatal("payload không có user phải trả lỗi")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.2.0", "2.1.0", 1},
		{"2.1.0", "2.1.0", 0},
		{"2.0.9", "2.1.0", -1},
		{"v2.1.1", "2.1.0", 1},
		{"2.10.0", "2.9.0", 1},      // số so sánh theo giá trị, không theo chuỗi
		{"v2.1.0-beta", "2.1.0", 0}, // hậu tố bỏ qua
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDealSettingsWindow(t *testing.T) {
	s := DealSettings{CheckFrom: "17:55", CheckTo: "22:10"}
	if !s.InCheckWindow(18 * 60) {
		t.Fatal("18:00 phải trong khung 17:55-22:10")
	}
	if s.InCheckWindow(10 * 60) {
		t.Fatal("10:00 phải ngoài khung")
	}
	if s.InCheckWindow(17*60 + 54) {
		t.Fatal("17:54 ngoài khung")
	}
	// Khung qua nửa đêm: 22:00-02:00 → 23:00 và 01:00 trong, 10:00 ngoài.
	night := DealSettings{CheckFrom: "22:00", CheckTo: "02:00"}
	if !night.InCheckWindow(23*60) || !night.InCheckWindow(60) {
		t.Fatal("khung qua nửa đêm sai")
	}
	if night.InCheckWindow(10 * 60) {
		t.Fatal("10:00 ngoài khung đêm")
	}
	// Cấu hình sai → fail-open (luôn trong khung).
	broken := DealSettings{CheckFrom: "abc", CheckTo: "22:10"}
	if !broken.InCheckWindow(10 * 60) {
		t.Fatal("cấu hình sai phải fail-open")
	}
}

func TestDealSettingsLoadSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_BASE_DIR", dir)
	// StateDir dùng UserHomeDir, ZCODE_DATA_BASE_DIR chỉ ảnh hưởng
	// ConfigDir — nên ghi file trực tiếp vào StateDir thật là không ổn
	// cho unit test. Thay vào đó kiểm tra default + round-trip JSON.
	saved := []byte(`{"check_account":"zcode-27-10@tannhatcms.io.vn","check_window_enabled":false,"check_from":"18:00","check_to":"23:00"}`)
	var loaded DealSettings
	if err := json.Unmarshal(saved, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.CheckAccount != "zcode-27-10@tannhatcms.io.vn" || loaded.CheckWindowEnabled || loaded.CheckFrom != "18:00" {
		t.Fatalf("parse settings sai: %+v", loaded)
	}
	def := DefaultDealSettings()
	if def.CheckAccount != "default" || !def.CheckWindowEnabled || def.CheckFrom != "17:55" {
		t.Fatalf("default sai: %+v", def)
	}
}

func TestCockpitParse(t *testing.T) {
	dir := t.TempDir()
	index := []byte(`{"version":"1","current_account_id":"acc-1","accounts":[
		{"id":"acc-1","email":"a@example.com","plan_type":"GLM Start"},
		{"id":"acc-2","email":"b@example.com"}
	]}`)
	if err := os.MkdirAll(filepath.Join(dir, "zcode_accounts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zcode_accounts.json"), index, 0o644); err != nil {
		t.Fatal(err)
	}
	writeAcc := func(id, jwt string) {
		t.Helper()
		body := []byte(`{"id":"` + id + `","email":"` + id + `@example.com","display_name":"Người ` + id + `","plan_type":"GLM Start","zcode_jwt_token":"` + jwt + `"}`)
		if err := os.WriteFile(filepath.Join(dir, "zcode_accounts", id+".json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeAcc("acc-1", "jwt-a")
	writeAcc("acc-2", "") // không có JWT — phải bị bỏ qua

	t.Setenv("COCKPIT_TOOLS_DATA_DIR", dir)
	t.Setenv("COCKPIT_TOOLS_PROFILE", "")
	accounts := ListCockpitZcodeAccounts()
	if len(accounts) != 1 {
		t.Fatalf("got %d accounts, want 1 (acc-2 không JWT bị bỏ qua)", len(accounts))
	}
	acc := accounts[0]
	if acc.ID != "acc-1" || acc.Email != "a@example.com" || acc.JWT != "jwt-a" || acc.DisplayName != "Người acc-1" {
		t.Fatalf("account parse sai: %+v", acc)
	}
}

func TestFindPlanPreview(t *testing.T) {
	plans := []ClaimPlanPreview{{PlanID: "p1"}, {PlanID: "p2", Name: "Deal đêm"}}
	if got := FindPlanPreview(plans, "p2"); got == nil || got.Name != "Deal đêm" {
		t.Fatalf("FindPlanPreview sai: %+v", got)
	}
	if FindPlanPreview(plans, "không-có") != nil {
		t.Fatal("plan lạ phải trả nil")
	}
}

func TestQuotaFromEntitlements(t *testing.T) {
	// meter model_usage được ưu tiên; unit "Tokens" giữ nguyên số.
	quota := QuotaFromEntitlements([]ClaimEntitlement{
		{ShowName: "GLM-5.3-Flash", Meter: "model_usage", GrantUnits: 300000000, UnitType: "Tokens"},
	})
	if quota == nil {
		t.Fatal("quota nil")
	}
	if got := quota.FormatAmount(); got != "300,000,000 Tokens" {
		t.Fatalf("FormatAmount = %q, want \"300,000,000 Tokens\"", got)
	}
	if got := quota.FormatBenefit(); got != "GLM-5.3-Flash · 300M Tokens" {
		t.Fatalf("FormatBenefit = %q, want \"GLM-5.3-Flash · 300M Tokens\"", got)
	}
	// unit "M" nhân lên 1 triệu: 300 M = 300,000,000.
	quotaM := QuotaFromEntitlements([]ClaimEntitlement{
		{ShowName: "GLM-5.3-Flash", Meter: "model_usage", GrantUnits: 300, UnitType: "M"},
	})
	if got := quotaM.FormatAmount(); got != "300,000,000 Tokens" {
		t.Fatalf("FormatAmount(M) = %q", got)
	}
	if quotaM.AmountShort != "300M" {
		t.Fatalf("AmountShort(M) = %q", quotaM.AmountShort)
	}
	// Không có entitlement model_usage và không có số → nil.
	if QuotaFromEntitlements([]ClaimEntitlement{{ShowName: "rỗng số"}}) != nil {
		t.Fatal("phải nil khi không đọc được số")
	}
}

func TestParseSnapshotZcodePlanArgs(t *testing.T) {
	payload := []byte(`{"code":0,"data":{"deliveries":[
		{"campaign_id":"pop-ticket","priority":90,"resource_position":"popup","popup":{
			"title":{"content":"Deal đêm"},
			"hero":{"type":"image","image":{"default":{"src":"https://cdn/hero.png","sha256":"aa"}},"args":{"zcode_plan":{
				"name":"ZCODE GLOBAL BUILD",
				"ends_at":1793280000,
				"entitlements":[{"meter":"model_usage","show_name":"GLM-5.3-Flash","grant_units":300000000,"unit_type":"Tokens"}]
			}}},
			"buttons":[{"text":{"content":"Nhận"},"action":{"type":"claim_zcode_plan","args":{"plan_id":"plan-t1"}}}]}
		}]}}`)
	deals, err := ParseSnapshot(payload)
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	if len(deals) != 1 || deals[0].ZcodePlan == nil {
		t.Fatalf("zcode_plan không parse được: %+v", deals)
	}
	plan := deals[0].ZcodePlan
	if plan.Name != "ZCODE GLOBAL BUILD" || plan.EndsAt != 1793280000 {
		t.Fatalf("zcode_plan name/ends_at sai: %+v", plan)
	}
	quota := QuotaFromEntitlements(plan.Entitlements)
	if quota == nil || quota.FormatAmount() != "300,000,000 Tokens" {
		t.Fatalf("quota từ zcode_plan sai: %+v", quota)
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
