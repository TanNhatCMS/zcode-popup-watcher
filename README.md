# zcode-deal — Theo dõi deal ZCode, popup nhận deal khi bạn bấm

Tool Go duy nhất, 1 binary: theo dõi deal ZCode rồi **hiện popup có nút
"Nhận deal"**. Deal chỉ được nhận khi **bạn bấm nút** (hoặc gõ `y` ở
lệnh `claim`) — tool không tự claim ngầm.

Vì sao vẫn giữ người dùng quyết định? Endpoint nhận deal
(`POST /api/v1/zcode-plan/billing/claim`) là API dành cho app ZCode
khi người dùng bấm nút. Tool gọi đúng API đó nhưng **chỉ theo lệnh bấm
của bạn**, không có auto-claim, không retry ngầm — tự động claim
rất dễ bị 429, thu hồi quota hoặc khóa tài khoản.

## Cách hoạt động

1. Đọc `~/.zcode/v2/telemetry-state.json` → `deviceMid` (UUID, plaintext).
2. Đọc `~/.zcode/v2/credentials.json` → giải mã key `zcodejwttoken`
   (AES-256-GCM, key = SHA256(secret), secret = `$ZCODE_CREDENTIAL_SECRET`
   nếu có, nếu không là `zcode-credential-fallback:<platform>:<homedir>:<username>`
   — đúng scheme của ZCode). Giải mã chỉ chạy được trên cùng máy/cùng user
   với ZCode; sai secret thì fail-closed, tool dừng lại, không đoán token.
3. Poll `GET https://zcode.z.ai/api/v1/marketing/touch?seq=...` với header
   `X-Device-Mid`, `X-Client-Language`, `X-ZCode-App-Version` và
   `Authorization: Bearer <jwt>` nếu có token. Mặc định 600 giây/lần,
   đúng nhịp poll của app gốc. Dưới 300 giây dễ bị 429.
4. Có delivery mới (banner/popup, mỗi vị trí chỉ lấy 1 cái đầu tiên,
   giống app gốc) thì báo + hiện popup nếu deal có nút nhận:
   - Nút `claim_zcode_plan` → popup "Nhận deal" → claim thật qua
     `POST /api/v1/zcode-plan/billing/claim` với body `{plan_id}` và
     header `X-ZCode-App-Version`, `X-Platform` (`win32-x64`,…) —
     đúng hợp đồng app gốc (`claimManualPlan`).
   - Nút `open_url` (chỉ http/https) → tool mở link thay vì gọi API.
5. Sau khi claim thành công, report `confirm` về
   `POST /api/v1/marketing/touch/action` với body
   `{campaign_id, action_type}` — giống app gốc report sau khi người
   dùng bấm nút. Bỏ qua deal thì report `cancel`.
6. Ghi deal đã báo vào seen file để không báo lại lần sau.

Hợp đồng API đối chiếu trực tiếp từ source ZCode:
`marketingTouchService.ts` (touch/action), `bigmodelCodingPlanSubscriptionProvider.ts`
(billing/preview + billing/claim), `marketingTouch.ts` (schema delivery).

Token **không bao giờ** in ra, ghi log hay ghi file — chỉ nằm trong RAM
và gửi làm header tới `zcode.z.ai`. `--dry-run` chỉ in "có/không".

## Cài đặt

Tải `zcode-deal.exe` (Windows) hoặc `zcode-deal` (Linux) từ tab Releases,
hoặc tự build từ source (cần Go 1.23+):

```sh
go build -o dist/zcode-deal .
# Windows (cross-build từ Linux/macOS, không cần CGO):
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H windowsgui -s -w" -o dist/zcode-deal.exe .
```

## Cách dùng

```sh
zcode-deal                    # không tham số: tự mở tray
zcode-deal check --once       # kiểm tra 1 lần rồi thoát
zcode-deal check              # poll liên tục, 10 phút/lần
zcode-deal tray               # chế độ chạy nền (xem dưới)
zcode-deal claim              # nhận deal đang chờ (xác nhận y/N)
zcode-deal claim --list       # xem các plan nhận được (billing/preview)
zcode-deal status             # daemon / app tray có đang chạy không
zcode-deal stop               # dừng daemon
zcode-deal reminder           # nhắc theo khung giờ deal đêm, 1 lần/ngày
zcode-deal reminder --test    # báo ngay để xem thử
zcode-deal self-test          # test parse snapshot mẫu, không gọi API
zcode-deal --version          # in version
```

Tùy chọn chung cho `check`/`watch`/`tray`:
`--interval GIÂY` (mặc định 600), `--config-dir DIR` (mặc định
`~/.zcode/v2`), `--open` (có deal thì thử mở ZCode luôn),
`--no-popup` (không hiện popup; deal nhận được vẫn lưu để
`zcode-deal claim` dùng được).
`check` thêm: `--once`, `--dry-run`, `--self-test`.
`reminder` thêm: `--from HH:MM` (mặc định 17:55), `--to HH:MM`
(mặc định 22:10, giờ VN), `--open`, `--test`.

Biến môi trường tùy chọn: `ZCODE_DATA_BASE_DIR` (đổi thư mục gốc thay
cho `$HOME`), `ZCODE_JWT_TOKEN` (ghi đè token, ưu tiên hơn
`credentials.json`), `ZCODE_CREDENTIAL_SECRET`, `ZCODE_LOCALE`
(mặc định `en-US`), `ZCODE_APP_VERSION` (mặc định `3.17.2`),
`ZCODE_MARKETING_URL` (đổi origin API khi test với server mẫu cục bộ —
áp dụng cho cả endpoint touch lẫn billing).

## Chế độ tray / chạy nền

- **Windows:** `zcode-deal tray` mở app tray thật (icon chuông vàng +
  menu native): Mở ZCode / Kiểm tra ngay / Mở log / Thoát. Có deal mới
  có thể nhận: **popup nổi trên cùng hiện ra** với nút
  "Nhận deal" / "Mở ZCode" / "Để sau" — bấm "Nhận deal" tool mới claim
  và hiện kết quả; toast + icon đổi chấm đỏ vẫn như trước. Chạy
  `zcode-deal.exe` không tham số cũng mở tray luôn.
- **Linux/macOS:** chưa có menu tray gốc (cần GTK/AppIndicator riêng).
  `zcode-deal tray` tách daemon chạy nền, lệnh thoát ngay, tắt được
  terminal; daemon báo bằng thông báo hệ thống, thoát bằng
  `zcode-deal stop`. Không có popup GUI: deal nhận được nằm trong
  `pending-claim.json`, nhận bằng `zcode-deal claim`.

State chỉ nằm ở `~/.zcode/mod/`:

```text
~/.zcode/mod/
  deal-checker.pid     # PID daemon (non-Windows)
  gotray.pid           # PID app tray Go (Windows)
  deal-checker.log     # log, giữ ~200 dòng cuối
  deal-notify.json     # thông báo deal mới nhất (chỉ key/title/message)
  pending-claim.json   # deal có thể nhận đang chờ (KHÔNG chứa token)
  check-now.trigger    # file trigger "kiểm tra ngay" cho daemon
  seen_deals.json      # các deal đã báo rồi
  popup-watcher-state.json  # ngày đã nhắc reminder
```

Chạy song song daemon và app tray Go có thể báo trùng (mỗi bên chỉ
load seen file 1 lần lúc khởi động), nên dừng daemon trước khi mở tray
và ngược lại.

## Test với server mẫu cục bộ

Không cần chạm API thật khi test luồng claim:

```sh
go run ./scripts/mockserver -addr 127.0.0.1:8931 &
ZCODE_MARKETING_URL=http://127.0.0.1:8931 \
ZCODE_JWT_TOKEN=mock-token \
zcode-deal check --once          # popup hiện ra với deal mock
zcode-deal claim                 # nhận deal mock, xem kết quả
```

## Lưu ý an ninh

- Nếu không đặt `ZCODE_CREDENTIAL_SECRET`, khóa giải mã chỉ suy ra từ
  platform + homedir + username, nên bất cứ ai chạy được với cùng user
  trên cùng máy này cũng giải mã được. Đây là thiết kế của ZCode, không
  phải do tool này làm yếu đi.
- Tool chỉ đọc trên chính máy này, chỉ dùng cho tài khoản của chính bạn.
  Đừng copy `credentials.json` sang máy khác rồi mong giải mã được —
  và cũng đừng chia sẻ file đó cho ai.
- Claim chỉ chạy khi bạn chủ động bấm nút / gõ y. Tool không có chế độ
  auto-claim và không nên thêm vào — rủi ro 429/thu hồi quota là thật.

## Lịch sử

- **2.1.0:** popup nhận deal trên Windows (Win32 thuần), claim qua
  `POST /api/v1/zcode-plan/billing/claim` khi bạn bấm nút, lệnh
  `claim` + `claim --list`, pending-claim.json, dọn toàn bộ code cũ
  (bản Bun 1.0.1 và app tray Go riêng) — 1 binary Go duy nhất.
- 2.0.0: viết lại toàn bộ bằng Go (thay bản Bun 1.0.1 + app tray Go
  riêng trước đó). Lệnh và state dir giữ nguyên.
- 1.0.1 trở về trước: bản Bun (đã bỏ).

## Giấy phép

MIT © 2026 TanNhatCMS. Xem file `LICENSE`.
