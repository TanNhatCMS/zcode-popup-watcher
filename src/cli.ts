#!/usr/bin/env bun
// cli.ts -- Diem vào duy nhất của CLI zcode-deal.
//
// Chạy bằng Bun:
//   bun run src/cli.ts check --once
//   bun run src/cli.ts tray
// Build thanh file chạy đọc lap (không cần cái Bun trên máy đích):
//   bun run build:exe        # Windows x64 -> dist/zcode-deal.exe
//   bun run build:exe:linux  # Linux x64 -> dist/zcode-deal
// File exe trên Windows cần có run.ps1 nằm cùng thư mục mới hiện
// được icon tray (run.ps1 là script riêng, không nhúng trong exe).
//
// CLI này chỉ BÁO deal, không tự nhận thay bạn. Xem README.md.

import { runDealChecker } from "./deal-checker.ts";
import { runPopupWatcher } from "./popup-watcher.ts";

const VERSION = "1.0.0";

const HELP = `zcode-deal ${VERSION} - Theo dõi deal ZCode, chỉ báo, không tự nhận.

Cách dùng:
  zcode-deal <lệnh> [tùy chọn]
  zcode-deal
      Không tham số: tự mở tray (tương đương lệnh tray).
      Double-click zcode-deal.exe trên Windows cũng mở tray luôn.

Lệnh:
  check [--once] [--interval GIÂY] [--config-dir DIR] [--open]
      Kiểm tra deal qua API marketing/touch. Mặc định poll liên tục
      mỗi 600 giây; --once chỉ kiểm tra 1 lần rồi thoát.
  watch [tùy chọn như check]
      Giống check (poll liên tục).
  tray [--interval GIÂY] [--config-dir DIR] [--open]
      Chạy daemon tách rời + icon tray, lệnh thoát ngay, tắt được
      terminal. Trên Windows cần run.ps1 cùng thư mục với exe.
  status
      Daemon tray có đang chạy không? Xem vài dòng log cuối.
  stop
      Dừng daemon tray (tương đương nút Thoát trong menu tray).
  reminder [--from HH:MM] [--to HH:MM] [--open]
      Nhắc theo khung giờ deal đêm (mặc định 17:55-22:10 giờ VN),
      1 lần/ngày, không gọi API.
  reminder --test
      Báo ngay để xem thử, không đợi khung giờ.
  self-test
      Test parse snapshot mẫu, không gọi API, không cần token.
  help
      Hiện bảng này.
  --version, -v
      In version.

Vì du:
  zcode-deal check --once
  zcode-deal tray --open
  zcode-deal status
  zcode-deal stop
  zcode-deal reminder --test

State nằm ở ~/.zcode/mod/. Token chỉ đọc từ ~/.zcode/v2 trên cùng
máy/cùng user với ZCode, không báo giờ in ra hay ghi ra file.
`;

async function main(): Promise<number> {
  const argv = process.argv.slice(2);
  const command = argv[0];
  const rest = argv.slice(1);

  switch (command) {
    case undefined:
      // Double-click exe / chạy không tham số: mở tray luôn, không
      // bắt user gõ thêm lệnh. Daemon đã chạy thì spawnTray chỉ dam
      // báo icon tray hiện lên, không tạo daemon trung.
      return await runDealChecker(["--tray"]);
    case "help":
    case "--help":
    case "-h":
      console.log(HELP);
      return 0;
    case "--version":
    case "-v":
    case "version":
      console.log(`zcode-deal ${VERSION}`);
      return 0;
    case "check":
    case "watch":
      return await runDealChecker(rest);
    case "tray":
      return await runDealChecker(["--tray", ...rest]);
    case "status":
      return await runDealChecker(["--status"]);
    case "stop":
      return await runDealChecker(["--stop"]);
    case "reminder":
      await runPopupWatcher(rest);
      return 0;
    case "self-test":
      return await runDealChecker(["--self-test"]);
    case "__tray-child":
    case "--tray-child":
      // Lệnh nội bộ: daemon con do --tray tạo ra.
      // Bản compile dùng __tray-child; bản bun qua cli.ts dùng
      // --tray-child (vì Bun.main lúc đó là cli.ts, không phải
      // deal-checker.ts, nên spawnTray gọi lại chính cli).
      return await runDealChecker(["--tray-child", ...rest]);
    default:
      console.error(`[cli] Lệnh không biết: ${command}\n`);
      console.log(HELP);
      return 2;
  }
}

process.exitCode = await main();
