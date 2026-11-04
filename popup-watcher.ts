#!/usr/bin/env bun
// popup-watcher.ts -- Ban Bun cua tool bao theo khung gio (khong goi API).
// Toi khung gio deal dem mo (mac dinh 17:55-22:10 gio VN) thi hien
// thong bao he thong 1 lan trong ngay, de ban tu mo ZCode bam banner.
// Khong doc token, khong goi API, khong tu nhan.
//
// Cach chay:
//   bun run popup-watcher.ts            # chay nen toi khung gio
//   bun run popup-watcher.ts --test     # bao ngay, khong doi khung gio
//   bun run popup-watcher.ts --from 17:50 --to 22:10

import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { notify, openZcode } from "./notify.ts";

// State chi nam o ~/.zcode/mod, cung thu muc voi deal-checker.
const STATE_DIR = join(homedir(), ".zcode", "mod");
const STATE_FILE = join(STATE_DIR, "popup-watcher-state.json");

function vnHourMinute(): { date: string; minutes: number } {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone: "Asia/Ho_Chi_Minh",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(new Date());
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "00";
  return {
    date: `${get("year")}-${get("month")}-${get("day")}`,
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
    console.error(`[watcher] khong ghi duoc state: ${error}`);
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

const args = parseArgs(process.argv.slice(2));
const fromMinutes = toMinutes(typeof args.from === "string" ? args.from : "17:55");
const toMinutesValue = toMinutes(typeof args.to === "string" ? args.to : "22:10");
const autoOpen = Boolean(args.open);

function showReminder(): void {
  const message = [
    "Toi gio nhan deal dem ZCode!",
    "",
    "1. Mo ZCode.",
    "2. Bam banner/modal deal dem trong app.",
    "3. Tu 22:00 moi dung duoc, toi 08:00 sang hom sau reset.",
    "4. Nho chon dung GLM-5.3-Flash + Global Building.",
    "",
    "Tool nay chi bao, khong tu bam nhan thay ban.",
  ].join("\n");
  notify("ZCode - Toi gio nhan deal dem!", message);
  console.log(message);
  if (autoOpen) console.log(openZcode());
}

if (args.test) {
  showReminder();
  process.exit(0);
}

console.log(`[watcher] Dang chay nen. Se bao trong khung ${args.from || "17:55"}-${args.to || "22:10"} gio VN, 1 lan/ngay.`);
while (true) {
  const now = vnHourMinute();
  if (loadNotifiedDate() !== now.date && now.minutes >= fromMinutes && now.minutes <= toMinutesValue) {
    showReminder();
    saveNotifiedDate(now.date);
  }
  await Bun.sleep(60_000);
}
