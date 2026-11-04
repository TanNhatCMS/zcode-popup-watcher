#!/usr/bin/env bun
// cli.ts -- Diem vao duy nhat cua CLI zcode-deal.
//
// Chay bang Bun:
//   bun run cli.ts check --once
//   bun run cli.ts tray
// Build thanh file chay doc lap (khong can cai Bun tren may dich):
//   bun run build:exe        # Windows x64 -> dist/zcode-deal.exe
//   bun run build:exe:linux  # Linux x64 -> dist/zcode-deal
// File exe tren Windows can co run.ps1 nam cung thu muc moi hien
// duoc icon tray (run.ps1 la script rieng, khong nhung trong exe).
//
// CLI nay chi BAO deal, khong tu nhan thay ban. Xem README.md.

import { runDealChecker } from "./deal-checker.ts";
import { runPopupWatcher } from "./popup-watcher.ts";

const VERSION = "0.1.0";

const HELP = `zcode-deal ${VERSION} - Theo doi deal ZCode, chi bao, khong tu nhan.

Cach dung:
  zcode-deal <lenh> [tuy chon]

Lenh:
  check [--once] [--interval GIAY] [--config-dir DIR] [--open]
      Kiem tra deal qua API marketing/touch. Mac dinh poll lien tuc
      moi 600 giay; --once chi kiem tra 1 lan roi thoat.
  watch [tuy chon nhu check]
      Giong check (poll lien tuc).
  tray [--interval GIAY] [--config-dir DIR] [--open]
      Chay daemon tach roi + icon tray, lenh thoat ngay, tat duoc
      terminal. Tren Windows can run.ps1 cung thu muc voi exe.
  status
      Daemon tray co dang chay khong? Xem vai dong log cuoi.
  stop
      Dung daemon tray (tuong duong nut Thoat trong menu tray).
  reminder [--from HH:MM] [--to HH:MM] [--open]
      Nhac theo khung gio deal dem (mac dinh 17:55-22:10 gio VN),
      1 lan/ngay, khong goi API.
  reminder --test
      Bao ngay de xem thu, khong doi khung gio.
  self-test
      Test parse snapshot mau, khong goi API, khong can token.
  help
      Hien bang nay.
  --version, -v
      In version.

Vi du:
  zcode-deal check --once
  zcode-deal tray --open
  zcode-deal status
  zcode-deal stop
  zcode-deal reminder --test

State nam o ~/.zcode/mod/. Token chi doc tu ~/.zcode/v2 tren cung
may/cung user voi ZCode, khong bao gio in ra hay ghi ra file.
`;

async function main(): Promise<number> {
  const argv = process.argv.slice(2);
  const command = argv[0];
  const rest = argv.slice(1);

  switch (command) {
    case undefined:
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
      // Lenh noi bo: daemon con do --tray tao ra (ban compile).
      // Ban bun dung --tray-child truc tiep thay vi lenh nay.
      return await runDealChecker(["--tray-child", ...rest]);
    default:
      console.error(`[cli] Lenh khong biet: ${command}\n`);
      console.log(HELP);
      return 2;
  }
}

process.exitCode = await main();
