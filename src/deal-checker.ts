#!/usr/bin/env bun
// deal-checker.ts -- Check deal ZCode bằng Bun, đọc config từ ~/.zcode/v2.
// CHỈ ĐỌC, KHÔNG NHẬN HỘ.
//
// Luong chạy:
//   1. Đọc ~/.zcode/v2/telemetry-state.json -> deviceMid.
//   2. Đọc ~/.zcode/v2/credentials.json -> giải mã key "zcodejwttoken"
//      (xem zcode-config.ts). Token chỉ nằm trong RAM, không in, không ghi file.
//      Nếu không giải mã được (sai ZCODE_CREDENTIAL_SECRET, hoặc chạy
//      khác user/máy với ZCode) thì tool dừng lại, không đoán token.
//   3. GET https://zcode.z.ai/api/v1/marketing/touch?seq=...
//      Header: X-Device-Mid, X-Client-Language, X-ZCode-App-Version,
//      Authorization: Bearer <jwt> nếu có token.
//   4. Nếu có delivery mới (banner/popup) thì hiện thông báo ra màn hình.
//      Bạn tự mở ZCode và bấm nút nhận trong app.
//
// Không gọi POST /api/v1/marketing/touch/action, không claim plan.
// Tần suất mặc định 600s (đúng app gốc), dùng đặt dưới 300s.
//
// Cách chạy (không cần cái package, không cần npm install):
//   bun run src/deal-checker.ts --once
//   bun run src/deal-checker.ts                 # chạy nên, 10 phút check 1 lần
//   bun run src/deal-checker.ts --self-test     # test parse, không gọi API
//   bun run src/deal-checker.ts --dry-run       # chỉ in cau hình (token chỉ hiện "có/không")
//   bun run src/deal-checker.ts --config-dir /duong/dan/khác/.zcode/v2
//   bun run src/deal-checker.ts --open          # có deal thì thử mở ZCode luôn
//
// Chế độ tray / chạy nên (1 lệnh, tắt terminal được):
//   bun run src/deal-checker.ts --tray          # tách daemon + icon tray, lệnh thoát ngay
//   bun run src/deal-checker.ts --status        # xem daemon có đang chạy không
//   bun run src/deal-checker.ts --stop          # thoát daemon (tương đương nút Thoát trong tray)
//   Trên Windows, --tray con mở run.ps1: icon tray có menu
//   "Mở ZCode", "Kiểm tra ngay", "Mở log", "Thoát". Bấm vào bong
//   thông báo deal cũng mở ZCode. Trên macOS/Linux chứa có menu tray
//   gốc (cần Swift/AppIndicator riêng), daemon vẫn chạy nên, báo bằng
//   thông báo hệ thống, và thoát bằng --stop.
//
// Biến môi trường tùy chọn (không bắt buộc nếu đã có config ZCode):
//   ZCODE_DATA_BASE_DIR  - đổi thư mục gốc, thay cho $HOME
//   ZCODE_JWT_TOKEN      - ghi đè token, ưu tiên hon credentials.json
//   ZCODE_CREDENTIAL_SECRET - dùng khi ZCode của bạn đặt secret riêng
//   ZCODE_LOCALE         - zh-CN hoặc en-US (mặc định en-US)
//   ZCODE_APP_VERSION    - X-ZCode-App-Version (mặc định 3.17.2)
//   ZCODE_MARKETING_URL  - chỉ dùng để test với server mẫu cục bộ

import { existsSync, readFileSync, writeFileSync, mkdirSync, unlinkSync, renameSync } from "node:fs";
import { spawn } from "node:child_process";
import { homedir } from "node:os";
import { join, dirname } from "node:path";
import { describeConfig, getZcodeConfigDir, readDeviceMid, readZcodeJwt } from "./zcode-config.ts";
import { notify, openZcode } from "./notify.ts";

const API_URL = process.env.ZCODE_MARKETING_URL || "https://zcode.z.ai/api/v1/marketing/touch";
const DEFAULT_INTERVAL_MS = 600_000;
// State của watcher chỉ nằm ở ~/.zcode/mod, không đọc thư mục cũ nào:
//   ~/.zcode/mod/{deal-checker.pid, deal-checker.log,
//     deal-notify.json, check-now.trigger, seen_deals.json}
const STATE_DIR = join(homedir(), ".zcode", "mod");
const SEEN_FILE = join(STATE_DIR, "seen_deals.json");
const PID_FILE = join(STATE_DIR, "deal-checker.pid");
const TRAY_PID_FILE = join(STATE_DIR, "tray.pid");
const LOG_FILE = join(STATE_DIR, "deal-checker.log");
const NOTIFY_FILE = join(STATE_DIR, "deal-notify.json");
const TRIGGER_FILE = join(STATE_DIR, "check-now.trigger");

function ensureStateDir(): void {
  try {
    mkdirSync(STATE_DIR, { recursive: true });
  } catch {
    // Bỏ qua, các hàm ghi file sẽ báo lỗi riêng nếu cần.
  }
}

function pidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function readPidFrom(path: string): number | null {
  try {
    const pid = Number(readFileSync(path, "utf-8").trim());
    return Number.isInteger(pid) && pid > 0 ? pid : null;
  } catch {
    return null;
  }
}

function readPidFile(): number | null {
  return readPidFrom(PID_FILE);
}

function removePidFilesIfMine(pid: number): void {
  try {
    if (readPidFrom(PID_FILE) === pid) unlinkSync(PID_FILE);
  } catch {
    // Bỏ qua.
  }
}

function appendLog(line: string): void {
  ensureStateDir();
  const stamped = `[${new Date().toISOString()}] ${line}\n`;
  try {
    const existing = existsSync(LOG_FILE) ? readFileSync(LOG_FILE, "utf-8") : "";
    // Giữ log gọn: chỉ giữ khoảng 200 dòng cuối.
    const kept = (existing + stamped).split("\n").slice(-201).join("\n");
    writeFileSync(LOG_FILE, kept, "utf-8");
  } catch {
    // Log hỏng thì không được làm hỏng vong poll.
  }
}

function writeNotifyFile(title: string, message: string, id: string): void {
  ensureStateDir();
  const tmp = `${NOTIFY_FILE}.tmp`;
  writeFileSync(
    tmp,
    JSON.stringify({ id, title, message, time: new Date().toISOString() }, null, 2),
    "utf-8",
  );
  renameSync(tmp, NOTIFY_FILE);
}

function scriptPath(): string {
  const main = (Bun as unknown as { main?: string }).main || process.argv[1] || "";
  return main;
}

// Bạn chạy bằng `bun run`: process.execPath là binary bun.
// Bản compile (`bun build --compile`): process.execPath là chính file exe.
function isBunRunner(): boolean {
  const exe = process.execPath || "";
  const base = exe.split(/[\\/]/).pop() || "";
  return base === "bun" || base === "bun.exe" || base.startsWith("bun-");
}

// Thư mục chứa run.ps1: bản compile thì cùng thư mục với file exe
// (user copy run.ps1 kèm exe); bản bun thì run.ps1 nằm ở gốc repo,
// tức thư mục cha của src/ (nơi chứa deal-checker.ts).
function appDir(): string {
  if (!isBunRunner()) return dirname(process.execPath);
  const script = scriptPath();
  return script ? dirname(script) : dirname(process.execPath);
}

function findTrayScript(): string | null {
  const candidates = [join(appDir(), "run.ps1"), join(appDir(), "..", "run.ps1")];
  for (const candidate of candidates) {
    if (existsSync(candidate)) return candidate;
  }
  return null;
}

function forwardTrayArgs(original: string[]): string[] {
  // Chỉ chuyển các tùy chọn an toàn sang daemon con; --tray tự nó không chuyển.
  const keep = new Set(["interval", "config-dir"]);
  const out: string[] = [];
  for (let i = 0; i < original.length; i++) {
    const arg = original[i]!;
    if (!arg.startsWith("--")) continue;
    const key = arg.slice(2);
    if (key === "open") {
      out.push("--open");
      continue;
    }
    if (keep.has(key)) {
      const next = original[i + 1];
      if (next && !next.startsWith("--")) {
        out.push(arg, next);
        i++;
      }
    }
  }
  return out;
}

function printTrayHelp(): void {
  if (!isBunRunner()) {
    console.log("[tray] Xem trạng thái: zcode-deal status | Thoát: zcode-deal stop");
  } else if (scriptPath().endsWith("cli.ts")) {
    console.log("[tray] Xem trạng thái: bun run src/cli.ts status | Thoát: bun run src/cli.ts stop");
  } else {
    console.log("[tray] Xem trạng thái: bun run src/deal-checker.ts --status | Thoát: bun run src/deal-checker.ts --stop");
  }
}

// Mở icon tray Windows nếu chứa có. PID của powershell được ghi
// vào tray.pid ngay khi spawn, run.ps1 tự xoá file khi thoát;
// PID chet thì lần chạy sau spawn lại, không tạo 2 icon.
function spawnTrayUI(): void {
  if (process.platform !== "win32") return;
  const existingTray = readPidFrom(TRAY_PID_FILE);
  if (existingTray && pidAlive(existingTray)) {
    console.log(`[tray] Icon tray đã chạy rồi (PID ${existingTray}).`);
    appendLog(`[tray] Icon tray đã chạy rồi (PID ${existingTray}).`);
    return;
  }
  const trayScript = findTrayScript();
  if (!trayScript) {
    // Lỗi này trước đây chỉ in ra console rồi cửa sổ tắt mất nên
    // user không thấy: ghi thêm vào log để status đọc được.
    const message = `[tray] Không tìm thấy run.ps1 ở ${appDir()} (hoặc thư mục cha), daemon vẫn chạy nền nhưng không có icon tray. Tải run.ps1 từ Release về cùng thư mục với exe rồi chạy lại lệnh tray.`;
    console.log(message);
    appendLog(message);
    return;
  }
  try {
    const tray = spawn(
      "powershell",
      [
        "-NoProfile",
        "-WindowStyle",
        "Hidden",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        trayScript,
        "-PidFile",
        PID_FILE,
        "-NotifyFile",
        NOTIFY_FILE,
        "-TriggerFile",
        TRIGGER_FILE,
        "-LogFile",
        LOG_FILE,
        "-TrayPidFile",
        TRAY_PID_FILE,
      ],
      { detached: true, stdio: "ignore" },
    );
    tray.unref();
    if (tray.pid) {
      try {
        writeFileSync(TRAY_PID_FILE, String(tray.pid), "utf-8");
      } catch {
        // Bỏ qua, lần sau sẽ spawn lại nếu không thấy PID sống.
      }
    }
    console.log("[tray] Icon tray Windows đã mở (menu: Mở ZCode / Kiểm tra ngay / Mở log / Thoát).");
    appendLog(`[tray] Icon tray Windows đã mở (script: ${trayScript}, powershell PID: ${tray.pid ?? "?"}).`);
  } catch (error) {
    console.error(`[tray] Không mở được icon tray: ${error}`);
    appendLog(`[tray] Không mở được icon tray: ${error}`);
  }
}

async function spawnTray(cliArgs: string[]): Promise<number> {
  ensureStateDir();
  const existing = readPidFile();
  if (existing && pidAlive(existing)) {
    console.log(`[tray] Daemon đã chạy rồi (PID ${existing}).`);
    spawnTrayUI();
    printTrayHelp();
    return 0;
  }
  const forwarded = forwardTrayArgs(cliArgs);
  let childArgs: string[];
  if (isBunRunner()) {
    const script = scriptPath();
    if (!script) {
      console.error("[tray] LỖI: không xác định được đường dẫn script để tách daemon.");
      return 2;
    }
    childArgs = ["run", script, "--tray-child", ...forwarded];
  } else {
    // Bản compile: exe tự chạy lệnh ẩn __tray-child (cli.ts dịch thành --tray-child).
    childArgs = ["__tray-child", ...forwarded];
  }
  const child = spawn(process.execPath, childArgs, { detached: true, stdio: "ignore", env: process.env });
  child.unref();
  // Cho daemon con ghi PID file rồi mới kết luận.
  for (let i = 0; i < 20; i++) {
    await Bun.sleep(100);
    const pid = readPidFile();
    if (pid && pidAlive(pid)) {
      console.log(`[tray] Daemon đã chạy (PID ${pid}). Bạn có thể tắt của số terminal này.`);
      console.log(`[tray] Log: ${LOG_FILE}`);
      if (process.platform === "win32") {
        spawnTrayUI();
      } else {
        console.log("[tray] He này chứa có menu tray gốc: daemon chạy nên + thông báo hệ thống, thoát bằng --stop.");
      }
      printTrayHelp();
      return 0;
    }
  }
  console.error("[tray] LỖI: daemon không ghi PID file. Xem log (nếu có):");
  try {
    console.error(readFileSync(LOG_FILE, "utf-8").split("\n").slice(-10).join("\n"));
  } catch {
    console.error("(chứa có log file)");
  }
  return 1;
}

async function handleStop(): Promise<number> {
  const pid = readPidFile();
  if (!pid) {
    console.log("[tray] Không có daemon nào đang chạy (không thấy PID file).");
    return 0;
  }
  if (!pidAlive(pid)) {
    console.log(`[tray] PID ${pid} đã chet rồi, don PID file cũ.`);
    try {
      unlinkSync(PID_FILE);
    } catch {
      // Bỏ qua.
    }
    return 0;
  }
  try {
    process.kill(pid, "SIGTERM");
  } catch (error) {
    console.error(`[tray] Không gui được SIGTERM tới PID ${pid}: ${error}`);
    return 1;
  }
  for (let i = 0; i < 30; i++) {
    await Bun.sleep(100);
    if (!pidAlive(pid)) break;
  }
  if (pidAlive(pid)) {
    try {
      process.kill(pid, "SIGKILL");
    } catch {
      // Bỏ qua.
    }
  }
  try {
    unlinkSync(PID_FILE);
  } catch {
    // Daemon con cùng tự xoá PID file khi thoát.
  }
  console.log(`[tray] Đã thoát daemon (PID ${pid}). Trên Windows, icon tray sẽ tự biến mất khi thấy PID file mất.`);
  return 0;
}

function handleStatus(): number {
  const pid = readPidFile();
  if (pid && pidAlive(pid)) {
    console.log(`[tray] Daemon DANG CHAY (PID ${pid}).`);
  } else if (pid) {
    console.log(`[tray] PID file còn ${pid} nhưng tiến trình đã chết (stale).`);
  } else {
    console.log("[tray] Daemon KHÔNG chạy.");
  }
  const trayPid = readPidFrom(TRAY_PID_FILE);
  if (trayPid && pidAlive(trayPid)) {
    console.log(`[tray] Icon tray Windows ĐANG CHẠY (powershell PID ${trayPid}).`);
  } else if (trayPid) {
    console.log(`[tray] tray.pid còn ${trayPid} nhưng tiến trình đã chết (stale) - icon tray không còn, chạy lại lệnh tray để mở lại.`);
  } else {
    console.log("[tray] Icon tray Windows KHÔNG chạy (không có tray.pid). Trên Windows, kiểm tra run.ps1 có nằm cùng thư mục với exe không, và icon có bị giấu trong vùng overflow (^) không.");
  }
  let logText: string | null = null;
  try {
    logText = readFileSync(LOG_FILE, "utf-8");
  } catch {
    logText = null;
  }
  if (logText !== null) {
    console.log("[tray] 10 dòng log cuối:");
    for (const line of logText.trim().split("\n").slice(-10)) console.log(`  ${line}`);
  } else {
    console.log("[tray] Chưa có log file.");
  }
  const seen = loadSeen();
  console.log(`[tray] Đã thấy ${seen.size} deal (seen file).`);
  return 0;
}

function resolveAuthForTray(configDir: string): { deviceMid: string; token: string | null } {
  const deviceMid = readDeviceMid(configDir);
  if (!deviceMid) throw new Error("không tìm thấy deviceMid trong telemetry-state.json");
  let token: string | null = process.env.ZCODE_JWT_TOKEN?.trim() || null;
  if (!token) token = readZcodeJwt(configDir); // Nem lỗi nếu giải mã thật bai.
  return { deviceMid, token };
}

async function sleepWithTriggerCheck(ms: number): Promise<boolean> {
  // Tra ve true nếu có file trigger (cần check ngay), false nếu het giờ ngu.
  const step = 2000;
  let remaining = ms;
  while (remaining > 0) {
    if (existsSync(TRIGGER_FILE)) {
      try {
        unlinkSync(TRIGGER_FILE);
      } catch {
        // Bỏ qua.
      }
      return true;
    }
    const wait = Math.min(step, remaining);
    await Bun.sleep(wait);
    remaining -= wait;
  }
  return existsSync(TRIGGER_FILE);
}

async function runTrayChild(args: Record<string, string | boolean>): Promise<number> {
  ensureStateDir();
  const configDir = getZcodeConfigDir(typeof args["config-dir"] === "string" ? args["config-dir"] : undefined);
  const intervalMs = args.interval ? Number(args.interval) * 1000 : DEFAULT_INTERVAL_MS;
  const locale = (process.env.ZCODE_LOCALE || "en-US") as string;
  const appVersion = process.env.ZCODE_APP_VERSION || "3.17.2";
  const autoOpen = Boolean(args.open);
  const isWindowsTray = process.platform === "win32";
  const log = (line: string): void => {
    console.log(line);
    appendLog(line);
  };
  const cleanup = (): void => {
    removePidFilesIfMine(process.pid);
  };
  process.on("SIGTERM", () => {
    log("[tray-child] Nhận SIGTERM, thoát.");
    cleanup();
    process.exit(0);
  });
  process.on("SIGINT", () => {
    log("[tray-child] Nhận SIGINT, thoát.");
    cleanup();
    process.exit(0);
  });
  writeFileSync(PID_FILE, String(process.pid), "utf-8");
  log(`[tray-child] Daemon bat đầu (PID ${process.pid}), interval ${intervalMs / 1000}s.`);
  let deviceMid: string;
  let token: string | null;
  try {
    ({ deviceMid, token } = resolveAuthForTray(configDir));
  } catch (error) {
    log(`[tray-child] LỖI auth: ${error}`);
    cleanup();
    return 2;
  }
  if (!token) log("[tray-child] Lưu ý: không có token, đang check ẩn danh.");
  let seq = 0;
  let failures = 0;
  const seen = loadSeen();
  while (true) {
    try {
      const deals = await queryDeals(token, deviceMid, locale, appVersion, seq);
      seq++;
      failures = 0;
      const fresh = deals.filter((d) => !seen.has(d.key));
      log(`[tray-child] Có ${deals.length} delivery, ${fresh.length} mới: ${deals.map((d) => d.key).join(", ") || "(rỗng)"}`);
      if (fresh.length > 0) {
        for (const deal of fresh) seen.add(deal.key);
        saveSeen(seen);
        const lines = ["ZCode có deal mới! Mở app và bấm nút nhận trong app.", ""];
        for (const deal of fresh) {
          lines.push(`- [${deal.position}] ${deal.campaignId} (priority ${String(deal.priority)})`);
          if (deal.title) lines.push(`  ${deal.title}`);
          if (deal.detail) lines.push(`  ${deal.detail}`);
        }
        lines.push("", "Tool này chỉ báo, không tự nhận thay bạn.");
        const message = lines.join("\n");
        writeNotifyFile("ZCode - Có deal mới!", message, fresh.map((d) => d.key).join(","));
        if (isWindowsTray) {
          // Tray Windows sẽ đọc notify file và hiện BalloonTip + click mở ZCode.
          // Không gọi MessageBox ở đây vì nó là modal chặn màn hình.
          log("[tray-child] Đã ghi notify file cho tray Windows.");
        } else {
          notify("ZCode - Có deal mới!", message);
        }
        if (autoOpen) log(openZcode());
      }
    } catch (error) {
      failures++;
      const delay = Math.min(600_000, 30_000 * 2 ** Math.min(failures - 1, 5));
      log(`[tray-child] Check lỗi (${error}). Đợi ${Math.round(delay / 1000)}s.`);
      await Bun.sleep(delay);
      continue;
    }
    await sleepWithTriggerCheck(intervalMs);
  }
}

interface Deal {
  key: string;
  campaignId: string;
  position: string;
  priority: unknown;
  title: string;
  detail: string;
}

function textContent(node: unknown): string {
  if (typeof node === "object" && node !== null) {
    const content = (node as Record<string, unknown>).content;
    return typeof content === "string" ? content.trim() : "";
  }
  return "";
}

export function parseSnapshot(payload: unknown): Deal[] {
  if (typeof payload !== "object" || payload === null) throw new Error("snapshot is not an object");
  const obj = payload as Record<string, unknown>;
  if (obj.code !== 0) throw new Error(`snapshot code != 0 (got ${String(obj.code)})`);
  const data = obj.data as Record<string, unknown> | undefined;
  if (!data || !Array.isArray(data.deliveries)) throw new Error("snapshot missing data.deliveries");
  const seenPositions = new Set<string>();
  const results: Deal[] = [];
  for (const raw of data.deliveries as unknown[]) {
    if (typeof raw !== "object" || raw === null) continue;
    const delivery = raw as Record<string, unknown>;
    const campaignId = delivery.campaign_id;
    const position = delivery.resource_position;
    if (typeof campaignId !== "string" || !campaignId.trim()) continue;
    if (position !== "banner" && position !== "popup") continue;
    if (seenPositions.has(position)) continue; // Mỗi vị trí chỉ lấy 1 cái đầu tiên, giống app gốc.
    seenPositions.add(position);
    let title = "";
    let detail = "";
    if (position === "banner") {
      const banner = (delivery.banner ?? {}) as Record<string, unknown>;
      const buttons = Array.isArray(banner.buttons) ? banner.buttons : [];
      const labels = buttons.map((b) => textContent((b as Record<string, unknown>).text)).filter(Boolean);
      title = textContent(((banner.success_popup ?? {}) as Record<string, unknown>).title);
      detail = labels.join(" / ") || "(banner, không có nút đọc được)";
    } else {
      const popup = (delivery.popup ?? {}) as Record<string, unknown>;
      title = textContent(popup.title);
      const desc = textContent(popup.description);
      const buttons = Array.isArray(popup.buttons) ? popup.buttons : [];
      const labels = buttons.map((b) => textContent((b as Record<string, unknown>).text)).filter(Boolean);
      detail = [desc, labels.join(" / ")].filter(Boolean).join(" - ");
    }
    results.push({
      key: `${position}:${campaignId}`,
      campaignId,
      position,
      priority: delivery.priority,
      title,
      detail,
    });
  }
  return results;
}

const SAMPLE_SNAPSHOT = {
  code: 0,
  data: {
    server_time: 1793000000,
    language: "en-US",
    deliveries: [
      {
        campaign_id: "sample-night-deal",
        priority: 80,
        resource_position: "banner",
        banner: {
          buttons: [{ text: { format: "plaintext", content: "Nhận ngay" } }],
          success_popup: { title: { format: "plaintext", content: "Nhận thanh công" } },
        },
      },
      {
        campaign_id: "sample-popup",
        priority: 90,
        resource_position: "popup",
        popup: {
          title: { format: "plaintext", content: "Deal đêm" },
          description: { format: "plaintext", content: "300M token GLM-5.3-Flash" },
          buttons: [{ text: { format: "plaintext", content: "Nhận" } }],
        },
      },
      { campaign_id: "sample-banner-duplicate", priority: 10, resource_position: "banner", banner: { buttons: [] } },
    ],
  },
};

function loadSeen(): Set<string> {
  try {
    const parsed = JSON.parse(readFileSync(SEEN_FILE, "utf-8"));
    if (!Array.isArray(parsed)) return new Set();
    const seen = new Set<string>();
    for (const key of parsed as unknown[]) if (typeof key === "string") seen.add(key);
    return seen;
  } catch {
    return new Set();
  }
}

function saveSeen(seen: Set<string>): void {
  try {
    ensureStateDir();
    writeFileSync(SEEN_FILE, JSON.stringify([...seen].sort(), null, 2), "utf-8");
  } catch (error) {
    console.error(`[checker] không ghi được seen file: ${error}`);
  }
}

async function queryDeals(token: string | null, deviceMid: string, locale: string, appVersion: string, seq: number): Promise<Deal[]> {
  const url = `${API_URL}?seq=${seq}`;
  const headers: Record<string, string> = {
    "X-Device-Mid": deviceMid,
    "X-Client-Language": locale,
    "X-ZCode-App-Version": appVersion,
  };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 15_000);
  try {
    const response = await fetch(url, { headers, signal: controller.signal });
    if (!response.ok) throw new Error(`HTTP ${response.status} từ marketing/touch`);
    return parseSnapshot(await response.json());
  } finally {
    clearTimeout(timeout);
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

// Hàm chính, dùng được cả khi chạy trực tiếp (bun run src/deal-checker.ts)
// và khi được cli.ts gọi. argv mặc định là process.argv.slice(2)
// (dùng cho cả bản bun và bản compile, vì argv[1] luôn là script/exe).
export async function runDealChecker(argv: string[] = process.argv.slice(2)): Promise<number> {
  const args = parseArgs(argv);
  const configDir = getZcodeConfigDir(typeof args["config-dir"] === "string" ? args["config-dir"] : undefined);
  const intervalMs = args.interval ? Number(args.interval) * 1000 : DEFAULT_INTERVAL_MS;
  const locale = (process.env.ZCODE_LOCALE || "en-US") as string;
  const appVersion = process.env.ZCODE_APP_VERSION || "3.17.2";
  const autoOpen = Boolean(args.open);

  if (args["self-test"]) {
    const deals = parseSnapshot(SAMPLE_SNAPSHOT);
    console.log(`[checker] self-test OK: ${deals.length} delivery (banner 1 + popup 1, bản trùng vị trí đã bị loại):`);
    for (const deal of deals) console.log(`  - ${deal.key} | ${deal.title} | ${deal.detail}`);
    return 0;
  }

  const summary = describeConfig(configDir);
  if (args["dry-run"]) {
    console.log(`[checker] config dir: ${summary.configDir}`);
    console.log(`[checker] files trong v2: ${summary.files.join(", ") || "(không thấy file nào)"}`);
    console.log(`[checker] deviceMid: ${summary.deviceMid ? summary.deviceMid.slice(0, 8) + "..." : "(không có)"}`);
    console.log(`[checker] credential keys: ${summary.credentialKeys.join(", ") || "(không có)"}`);
    console.log(`[checker] có zcodejwttoken: ${summary.hasJwt ? "có" : "không"}`);
    console.log(`[checker] API: ${API_URL}, locale: ${locale}, app-version: ${appVersion}, interval: ${intervalMs / 1000}s`);
    console.log("[checker] Chế độ: chỉ GET marketing/touch, không POST action, không claim, không in token.");
    return 0;
  }

  if (args.tray) return await spawnTray(argv);
  if (args["tray-child"]) return await runTrayChild(args);
  if (args.stop) return await handleStop();
  if (args.status) return handleStatus();

  if (intervalMs < 300_000) {
    console.error("[checker] Canh báo: interval dưới 300s dễ bị 429. Tool vẫn chạy theo số bạn đặt.");
  }

  const deviceMid = readDeviceMid(configDir);
  if (!deviceMid) {
    console.error("[checker] LỖI: không tìm thấy deviceMid trong telemetry-state.json.");
    console.error("[checker] Mở ZCode 1 lần để nó tạo file, hoặc kiểm tra lại ZCODE_DATA_BASE_DIR.");
    return 2;
  }

  // Thứ tự token: env ghi đè > credentials.json giải mã. Không báo giờ in token.
  let token: string | null = process.env.ZCODE_JWT_TOKEN?.trim() || null;
  if (!token) {
    try {
      token = readZcodeJwt(configDir);
    } catch (error) {
      console.error(`[checker] LỖI: không giải mã được zcodejwttoken (${error}).`);
      console.error("[checker] Thường do chạy khác user/máy với ZCode, hoặc ZCODE_CREDENTIAL_SECRET khác với lúc ZCode mã hoá.");
      return 2;
    }
  }
  if (!token) {
    console.error("[checker] Lưu ý: không có token, đang check ẩn danh. Deal gắn tài khoản có thể không hiện.");
  } else {
    console.log("[checker] Đã có token từ config (không hiện token ra đây).");
  }

  let seq = 0;
  let failures = 0;
  const seen = loadSeen();
  const once = Boolean(args.once);

  while (true) {
    try {
      const deals = await queryDeals(token, deviceMid, locale, appVersion, seq);
      seq++;
      failures = 0;
      const fresh = deals.filter((d) => !seen.has(d.key));
      console.log(`[checker] Có ${deals.length} delivery, ${fresh.length} mới: ${deals.map((d) => d.key).join(", ") || "(rỗng)"}`);
      if (fresh.length > 0) {
        for (const deal of fresh) seen.add(deal.key);
        saveSeen(seen);
        const lines = ["ZCode có deal mới! Mở app và bấm nút nhận trong app.", ""];
        for (const deal of fresh) {
          lines.push(`- [${deal.position}] ${deal.campaignId} (priority ${String(deal.priority)})`);
          if (deal.title) lines.push(`  ${deal.title}`);
          if (deal.detail) lines.push(`  ${deal.detail}`);
        }
        lines.push("", "Tool này chỉ báo, không tự nhận thay bạn.");
        notify("ZCode - Có deal mới!", lines.join("\n"));
        if (autoOpen) console.log(openZcode());
      }
    } catch (error) {
      failures++;
      const delay = Math.min(600_000, 30_000 * 2 ** Math.min(failures - 1, 5));
      console.error(`[checker] Check lỗi (${error}). Đợi ${Math.round(delay / 1000)}s rồi thử lại.`);
      if (once) return 1;
      await Bun.sleep(delay);
      continue;
    }
    if (once) return 0;
    await Bun.sleep(intervalMs);
  }
}

// Chỉ tự chạy khi file này là entrypoint. Khi cli.ts import thì
// cli sẽ gọi runDealChecker() theo lệnh của user.
if (import.meta.main) {
  process.exitCode = await runDealChecker();
}
