# zcode-popup-watcher

[![CI](https://github.com/TanNhatCMS/zcode-popup-watcher/actions/workflows/ci.yml/badge.svg)](https://github.com/TanNhatCMS/zcode-popup-watcher/actions/workflows/ci.yml)

Các tool theo dõi deal của ZCode và thông báo lên màn hình, để bạn
tự nhận deal trong app ZCode chính thức.

**Các tool này không bao giờ tự nhận deal thay bạn.** Chúng không gọi
endpoint action của marketing, không bấm gì trong ZCode, và không gửi
token của bạn đi đâu khác ngoài header `Authorization` tới
`zcode.z.ai` khi kiểm tra snapshot marketing.

## Theo dõi cái gì

Hệ thống banner/popup trong app ZCode ("marketing-touch") hoạt động
bằng cách polling, không phải push:

```
GET https://zcode.z.ai/api/v1/marketing/touch?seq=...
```

kèm các header `X-Device-Mid`, `X-Client-Language`,
`X-ZCode-App-Version` và, khi đã đăng nhập,
`Authorization: Bearer <ZCode JWT>`. Server trả về một snapshot chứa
`deliveries` (một `banner` và/hoặc một `popup`). Client chỉ hiển thị
những gì server trả về, nên khung giờ chiến dịch, điều kiện tham gia
và số lượng còn lại đều do server quyết định.

## Các tool

Chỉ dùng TypeScript + Bun. Không có dependency, không cần bước cài đặt.
Cần có [Bun](https://bun.sh).

| File | Chức năng |
| --- | --- |
| `cli.ts` | Điểm vào duy nhất của CLI `zcode-deal` (xem mục CLI bên dưới). Các file dưới vẫn chạy trực tiếp được bằng Bun. |
| `deal-checker.ts` | Poll `marketing/touch` mỗi 600 giây (đúng chu kỳ của client production), hiện thông báo hệ thống khi có delivery mới. |
| `popup-watcher.ts` | Nhắc theo khung giờ: thông báo một lần mỗi ngày trong khung 17:55–22:10 giờ VN cho deal đêm. Không gọi API. |
| `zcode-config.ts` | Đọc config ở `~/.zcode/v2` (xem bên dưới). |
| `notify.ts` | Thông báo hệ thống: macOS dùng `osascript`, Linux dùng `notify-send`, Windows dùng MessageBox, không có công cụ nào thì in ra console. |
| `run.ps1` | Icon system tray trên Windows cho daemon chạy nền (xem bên dưới). Không dùng trên hệ điều hành khác. |

```sh
bun run deal-checker.ts --once
bun run deal-checker.ts            # poll chạy nền, 10 phút kiểm tra một lần
bun run deal-checker.ts --self-test
bun run deal-checker.ts --dry-run  # chỉ in cấu hình, không gọi API
bun run popup-watcher.ts --test
```

## CLI và file .exe (Bun compile)

Không cần cài Bun trên máy đích: build bằng `bun build --compile`
thành file chạy độc lập (runtime Bun đã nhúng trong file). File này
vẫn đọc config `~/.zcode/v2` trên cùng máy/cùng user với ZCode như
bản script, và state vẫn ở `~/.zcode/mod/`.

```sh
# Chạy CLI bằng Bun (không cần build)
bun run cli.ts check --once
bun run cli.ts tray --open
bun run cli.ts status
bun run cli.ts stop
bun run cli.ts reminder --test
bun run cli.ts self-test
bun run cli.ts --help

# Build file chạy độc lập
bun run build:exe        # Windows x64 -> dist/zcode-deal.exe
bun run build:exe:linux  # Linux x64 -> dist/zcode-deal
./dist/zcode-deal check --once
./dist/zcode-deal tray --open
```

Lưu ý cho bản `.exe` trên Windows: `run.ps1` là script riêng, không
nhúng trong exe, nên muốn hiện icon tray thì copy `run.ps1` vào cùng
thư mục với `zcode-deal.exe` rồi mới chạy `zcode-deal.exe tray`.
Các lệnh còn lại (`check`, `status`, `stop`, `reminder`) chỉ cần
mỗi file exe.

Chạy không tham số cũng mở tray luôn: double-click
`zcode-deal.exe` trên Windows (hoặc chạy `zcode-deal` không tham số)
sẽ tự khởi động daemon và hiện icon tray, tương đương lệnh `tray`.
Chạy lại lần nữa khi daemon đã chạy thì chỉ đảm bảo icon tray hiện
lên, không tạo daemon trùng (PID của icon được theo dõi ở
`~/.zcode/mod/tray.pid`, `run.ps1` tự xoá file này khi thoát).
Muốn xem trợ giúp thì chạy `zcode-deal help` thay vì chạy trần.

CI cũng build sẵn hai file này mỗi lần push lên `main` hoặc mở pull
request (job `build-exe`): vào tab Actions của repo, mở run tương
ứng và tải artifact `zcode-deal-windows-x64` hoặc
`zcode-deal-linux-x64` về dùng, không cần build ở local.

## Chế độ tray / chạy nền (một lệnh, tắt được terminal)

```sh
bun run deal-checker.ts --tray      # khởi động daemon tách rời + icon tray, lệnh thoát ngay
bun run deal-checker.ts --tray --open   # như trên, nhưng tự mở ZCode khi có deal
bun run deal-checker.ts --status    # daemon có đang chạy không? xem vài dòng log cuối
bun run deal-checker.ts --stop      # dừng daemon (tương đương nút Thoát trong menu tray)
```

Cách hoạt động: `--tray` tạo một tiến trình con Bun tách rời
(`--tray-child`) rồi thoát ngay, nên bạn có thể tắt cửa sổ terminal.
Tiến trình con tiếp tục poll, ghi log vào
`~/.zcode/mod/deal-checker.log`, ghi PID vào
`~/.zcode/mod/deal-checker.pid`, và ghi mỗi deal mới vào
`~/.zcode/mod/deal-notify.json` (chỉ có key campaign, tiêu
đề và nội dung — không bao giờ có token). Nếu đã có một daemon đang
chạy mà gọi `--tray` thêm lần nữa thì lệnh sẽ từ chối, không tạo
daemon trùng. State chỉ nằm ở `~/.zcode/mod`, code không đọc các
thư mục cũ nữa — nếu còn daemon bản cũ đang chạy, hãy dừng nó trước
rồi xoá hẳn các thư mục cũ cho gọn.

Trên **Windows**, `--tray` còn chạy `run.ps1`, hiện icon system tray
với menu sau:

- **Mở ZCode** — mở app ZCode (chỉ mở khi thật sự tìm thấy file thực
  thi; các đường dẫn cài đặt thường gặp được kiểm tra trước).
- **Kiểm tra ngay** — ghi một file trigger; daemon sẽ kiểm tra lại
  trong khoảng 2 giây thay vì chờ đủ chu kỳ 600 giây.
- **Mở log** — mở log của daemon bằng Notepad.
- **Thoát** — giết daemon rồi tự tắt icon tray.

Lưu ý: menu tray trên Windows hiển thị không dấu, vì `run.ps1` được
viết không dấu để tránh lỗi encoding của Windows PowerShell 5.1.

Khi có deal, tray hiện thông báo dạng bong bóng từ file notify;
**bấm vào bong bóng sẽ mở ZCode**, để bạn nhận deal trong app chính
thức. Với `--open`, daemon tự mở ZCode ngay khi phát hiện deal, không
cần chờ bạn bấm.

Trên **macOS/Linux** hiện chưa có menu tray gốc (muốn có cần viết
riêng một app menu bar bằng Swift hoặc một helper AppIndicator cho
Linux): daemon vẫn chạy tách rời kèm thông báo hệ thống, và `--stop`
là cách để thoát. Bản thân icon tray, bao gồm `run.ps1`, chưa chạy
thật được trên máy test Linux headless, nên hãy xác nhận trên Windows
rằng icon và menu có hiện ra không — phía daemon (tách tiến trình,
PID, log, file notify, trigger, stop) thì đã được kiểm chứng.

## Đọc config từ `~/.zcode/v2`

Checker đọc, trên cùng máy và cùng user với ZCode:

- `telemetry-state.json` → `deviceMid` (một UUID dạng plaintext,
  không phải bí mật).
- `credentials.json` → key `zcodejwttoken`, giá trị của nó đã được mã
  hoá dạng `enc:v1:...` và được giải mã bằng AES-256-GCM. Khoá là
  `SHA256(secret)`, trong đó secret là `$ZCODE_CREDENTIAL_SECRET` nếu
  có, nếu không thì là
  `zcode-credential-fallback:<platform>:<homedir>:<username>`
  — đúng scheme của chính ZCode. Hệ quả:
  - Tool chỉ chạy được trên cùng máy và cùng user với ZCode. Chạy khác
    máy, khác user hoặc khác secret thì tool dừng lại (fail-closed)
    thay vì đoán token.
  - Nếu không đặt secret riêng, secret fallback này không phải là bảo
    mật mạnh: bất cứ ai chạy được với cùng user trên cùng máy cũng
    giải mã được, giống như chính ZCode làm được.
  - Token không bao giờ được in ra, không bao giờ được ghi vào file
    hay log, và chỉ tồn tại trong RAM trong lúc chạy. `--dry-run` chỉ
    in ra token có hay không và `deviceMid` đã che bớt.

Token truyền qua `ZCODE_JWT_TOKEN` sẽ ghi đè lên `credentials.json`.

## Kiểm tra tự động (CI)

Mỗi lần push lên `main` hoặc mở pull request, GitHub Actions chạy
workflow `.github/workflows/ci.yml` trên Ubuntu với Bun 1.4.4:

- `bun run build` — build `cli.ts`, `deal-checker.ts` và
  `popup-watcher.ts` ra `dist/` bằng Bun, để bắt lỗi cú pháp/import hỏng.
- `bun run test` — self-test của deal-checker (chỉ parse snapshot
  mẫu, không gọi API thật, không cần token).
- `bun run test:watcher` — popup-watcher ở chế độ `--test`
  (báo ngay, không chờ khung giờ).
- `bun run cli.ts self-test` — kiểm tra CLI dispatch đúng lệnh.
- Parse cú pháp `run.ps1` bằng PowerShell (chỉ parse, không chạy
  code WinForms nên an toàn trên Linux).
- Job `build-exe` — build `zcode-deal.exe` (Windows x64, cross-compile
  từ Ubuntu) và `zcode-deal` (Linux x64) rồi upload làm artifact.

Ngoài ra còn có workflow `.github/workflows/release.yml` riêng cho
phát hành: push một tag dạng `v*` (ví dụ `git tag v1.0.0 &&
git push origin v1.0.0`) hoặc chạy thủ công từ tab Actions với input
tag, workflow sẽ build hai file trên và tạo GitHub Release mang tên
tag đó, đính kèm `zcode-deal.exe`, `zcode-deal` và `run.ps1` (đủ bộ
để chạy tray trên Windows). Chạy lại cùng tag thì file cũ được ghi đè,
không tạo release trùng.

Chạy đúng các bước đó ở local bằng một lệnh: `bun run check`.

## Kỷ luật polling

- Chu kỳ mặc định là 600 giây, khớp với poller production của ZCode.
  Đừng đặt dưới 300 giây: bạn dễ bị HTTP 429, nhất là khi app ZCode
  cũng đang poll song song.
- Khi gặp lỗi, thời gian chờ tăng theo cấp số nhân lên tới tối đa 10
  phút, giống poller của app.
- Các delivery đã thấy được ghi vào
  `~/.zcode/mod/seen_deals.json` (chỉ key campaign, không
  bao giờ có token) để mỗi deal chỉ được thông báo một lần.

## Ghi chú bảo mật

- Đừng bao giờ dán ZCode JWT của bạn vào chat, issue hay commit.
- Các tool này chỉ thông báo, ở chế độ chỉ đọc. Muốn nhận deal, hãy
  mở ZCode và bấm banner/popup trong app chính thức.
- Tự động nhận quota miễn phí vi phạm điều khoản của Z.ai và có nguy
  cơ bị giới hạn tần suất, thu hồi quota hoặc khoá tài khoản. Tính năng
  đó cố tình không được làm ở đây.
