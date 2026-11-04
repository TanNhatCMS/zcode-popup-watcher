# zcode-popup-watcher

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
`~/.zcode/zcode-popup-watcher/deal-checker.log`, ghi PID vào
`~/.zcode/zcode-popup-watcher/deal-checker.pid`, và ghi mỗi deal mới vào
`~/.zcode/zcode-popup-watcher/deal-notify.json` (chỉ có key campaign, tiêu
đề và nội dung — không bao giờ có token). Nếu đã có một daemon đang
chạy mà gọi `--tray` thêm lần nữa thì lệnh sẽ từ chối, không tạo
daemon trùng. Các bản cũ để state ở `~/.zcode_popup_watcher`: PID và
seen cũ vẫn được đọc để không chạy trùng daemon cũ và không báo lại
deal đã thấy, nhưng mọi file mới chỉ ghi vào `~/.zcode/zcode-popup-watcher`.
Sau khi đã dừng daemon cũ, bạn có thể xoá hẳn thư mục cũ cho gọn.

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

## Kỷ luật polling

- Chu kỳ mặc định là 600 giây, khớp với poller production của ZCode.
  Đừng đặt dưới 300 giây: bạn dễ bị HTTP 429, nhất là khi app ZCode
  cũng đang poll song song.
- Khi gặp lỗi, thời gian chờ tăng theo cấp số nhân lên tới tối đa 10
  phút, giống poller của app.
- Các delivery đã thấy được ghi vào
  `~/.zcode/zcode-popup-watcher/seen_deals.json` (chỉ key campaign, không
  bao giờ có token) để mỗi deal chỉ được thông báo một lần.

## Ghi chú bảo mật

- Đừng bao giờ dán ZCode JWT của bạn vào chat, issue hay commit.
- Các tool này chỉ thông báo, ở chế độ chỉ đọc. Muốn nhận deal, hãy
  mở ZCode và bấm banner/popup trong app chính thức.
- Tự động nhận quota miễn phí vi phạm điều khoản của Z.ai và có nguy
  cơ bị giới hạn tần suất, thu hồi quota hoặc khoá tài khoản. Tính năng
  đó cố tình không được làm ở đây.
