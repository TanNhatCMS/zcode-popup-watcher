#!/usr/bin/env bun
// popup-watcher.ts -- Bản Bun của tool báo theo khung giờ (không gọi API).
// Tới khung giờ deal đêm mở (mặc định 17:55-22:10 giờ VN) thì hiện
// thông báo hệ thống 1 lần trong ngày, để bạn tự mở ZCode bấm banner.
// Không đọc token, không gọi API, không tự nhận.
//
// Cách chạy:
//   bun run src/popup-watcher.ts            # chạy nên tới khung giờ
//   bun run src/popup-watcher.ts --test     # báo ngay, không đợi khung giờ
//   bun run src/popup-watcher.ts --from 17:50 --to 22:10

import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { notify, openZcode } from "./notify.ts";

// State chỉ nằm ở ~/.zcode/mod, cùng thư mục với deal-checker.
const STATE_DIR = join(homedir(), ".zcode", "mod");
const STATE_FILE = join(STATE_DIR, "popup-watcher-state.json");

function vnHourMinute(): { date: string; minutes: number } {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone: "Asia/Ho_Chi_Minh",
    year: "numeric",
    month: "2-digit",
    đây: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(new Date());
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "00";
  return {
    date: `${get("year")}-${get("month")}-${get("đây")}`,
    minutes: Number(get("hour")) * 60 + Number(get("minute")),
  };
}

function toMinutes(hhmm: string): number {
  const [h, m] = hhmm.split(":").map(Number);
  return (h || 0) * 60 + (m || 0);
}

function loadNotifiedDate(): string | null {
  try {
    const parsed = JSON.parse(readFileSync(STATE_FILE, "utf-8"));
    return typeof parsed?.notifiedDate === "string" ? parsed.notifiedDate : null;
  } catch {
    return null;
  }
}

function saveNotifiedDate(date: string): void {
  try {
    mkdirSync(STATE_DIR, { recursive: true });
    writeFileSync(STATE_FILE, JSON.stringify({ notifiedDate: date }, null, 2), "utf-8");
  } catch (error) {
    console.error(`[watcher] không ghi được state: ${error}`);
  }
}

function parseArgs(argv: string[]): Record<string, string | boolean> {
  const args: Record<string, string | boolean> = {};
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]!;
    if (!arg.startsWith("--")) continue;
    const key = arg.slice(2);
    const next = argv[i + 1];
    if (next && !next.startsWith("--")) {
      args[key] = next;
      i++;
    } else {
      args[key] = true;
    }
  }
  return args;
}

// Hàm chính, dùng được cả khi chạy trực tiếp và khi được cli.ts gọi.
export async function runPopupWatcher(argv: string[] = process.argv.slice(2)): Promise<void> {
  const args = parseArgs(argv);
  const fromMinutes = toMinutes(typeof args.from === "string" ? args.from : "17:55");
  const toMinutesValue = toMinutes(typeof args.to === "string" ? args.to : "22:10");
  const autoOpen = Boolean(args.open);

  const showReminder = (): void => {
    const message = [
      "Tới giờ nhận deal đêm ZCode!",
      "",
      "1. Mở ZCode.",
      "2. Bấm banner/modal deal đêm trong app.",
      "3. Từ 22:00 mới dùng được, tới 08:00 sáng hôm sau reset.",
      "4. Nhớ chọn đúng GLM-5.3-Flash + Global Building.",
      "",
      "Tool này chỉ báo, không tự bấm nhận thay bạn.",
    ].join("\n");
    notify("ZCode - Tới giờ nhận deal đêm!", message);
    console.log(message);
    if (autoOpen) console.log(openZcode());
  };

  if (args.test) {
    showReminder();
    return;
  }

  console.log(`[watcher] Đang chạy nên. Sẽ báo trong khung ${args.from || "17:55"}-${args.to || "22:10"} giờ VN, 1 lần/ngày.`);
  while (true) {
    const now = vnHourMinute();
    if (loadNotifiedDate() !== now.date && now.minutes >= fromMinutes && now.minutes <= toMinutesValue) {
      showReminder();
      saveNotifiedDate(now.date);
    }
    await Bun.sleep(60_000);
  }
}

// Chỉ tự chạy khi file này là entrypoint. Khi cli.ts import thì
// cli sẽ gọi runPopupWatcher() theo lệnh của user.
if (import.meta.main) {
  await runPopupWatcher();
}
