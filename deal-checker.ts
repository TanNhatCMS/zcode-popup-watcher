#!/usr/bin/env bun
// deal-checker.ts -- Check deal ZCode bang Bun, doc config tu ~/.zcode/v2.
// CHI DOC, KHONG NHAN HO.
//
// Luong chay:
//   1. Doc ~/.zcode/v2/telemetry-state.json -> deviceMid.
//   2. Doc ~/.zcode/v2/credentials.json -> giai ma key "zcodejwttoken"
//      (xem zcode-config.ts). Token chi nam trong RAM, khong in, khong ghi file.
//      Neu khong giai ma duoc (sai ZCODE_CREDENTIAL_SECRET, hoac chay
//      khac user/may voi ZCode) thi tool dung lai, khong doan token.
//   3. GET https://zcode.z.ai/api/v1/marketing/touch?seq=...
//      Header: X-Device-Mid, X-Client-Language, X-ZCode-App-Version,
//      Authorization: Bearer <jwt> neu co token.
//   4. Neu co delivery moi (banner/popup) thi hien thong bao ra man hinh.
//      Ban tu mo ZCode va bam nut nhan trong app.
//
// Khong goi POST /api/v1/marketing/touch/action, khong claim plan.
// Tan suat mac dinh 600s (dung app goc), dung dat duoi 300s.
//
// Cach chay (khong can cai package, khong can npm install):
//   bun run deal-checker.ts --once
//   bun run deal-checker.ts                 # chay nen, 10 phut check 1 lan
//   bun run deal-checker.ts --self-test     # test parse, khong goi API
//   bun run deal-checker.ts --dry-run       # chi in cau hinh (token chi hien "co/khong")
//   bun run deal-checker.ts --config-dir /duong/dan/khac/.zcode/v2
//   bun run deal-checker.ts --open          # co deal thi thu mo ZCode luon
//
// Che do tray / chay nen (1 lenh, tat terminal duoc):
//   bun run deal-checker.ts --tray          # tach daemon + icon tray, lenh thoat ngay
//   bun run deal-checker.ts --status        # xem daemon co dang chay khong
//   bun run deal-checker.ts --stop          # thoat daemon (tuong duong nut Thoat trong tray)
//   Tren Windows, --tray con mo run.ps1: icon tray co menu
//   "Mo ZCode", "Kiem tra ngay", "Mo log", "Thoát". Bam vao bong
//   thong bao deal cung mo ZCode. Tren macOS/Linux chua co menu tray
//   goc (can Swift/AppIndicator rieng), daemon van chay nen, bao bang
//   thong bao he thong, va thoat bang --stop.
//
// Bien moi truong tuy chon (khong bat buoc neu da co config ZCode):
//   ZCODE_DATA_BASE_DIR  - doi thu muc goc, thay cho $HOME
//   ZCODE_JWT_TOKEN      - ghi de token, uu tien hon credentials.json
//   ZCODE_CREDENTIAL_SECRET - dung khi ZCode cua ban dat secret rieng
//   ZCODE_LOCALE         - zh-CN hoac en-US (mac dinh en-US)
//   ZCODE_APP_VERSION    - X-ZCode-App-Version (mac dinh 3.17.2)
//   ZCODE_MARKETING_URL  - chi dung de test voi server mau cuc bo

import { existsSync, readFileSync, writeFileSync, mkdirSync, unlinkSync, renameSync } from "node:fs";
import { spawn } from "node:child_process";
import { homedir } from "node:os";
import { join, dirname } from "node:path";
import { describeConfig, getZcodeConfigDir, readDeviceMid, readZcodeJwt } from "./zcode-config.ts";
import { notify, openZcode } from "./notify.ts";

const API_URL = process.env.ZCODE_MARKETING_URL || "https://zcode.z.ai/api/v1/marketing/touch";
const DEFAULT_INTERVAL_MS = 600_000;
// State cua watcher chi nam o ~/.zcode/mod, khong doc thu muc cu nao:
//   ~/.zcode/mod/{deal-checker.pid, deal-checker.log,
//     deal-notify.json, check-now.trigger, seen_deals.json}
const STATE_DIR = join(homedir(), ".zcode", "mod");
const SEEN_FILE = join(STATE_DIR, "seen_deals.json");
const PID_FILE = join(STATE_DIR, "deal-checker.pid");
const LOG_FILE = join(STATE_DIR, "deal-checker.log");
const NOTIFY_FILE = join(STATE_DIR, "deal-notify.json");
const TRIGGER_FILE = join(STATE_DIR, "check-now.trigger");

function ensureStateDir(): void {
  try {
    mkdirSync(STATE_DIR, { recursive: true });
  } catch {
    // Bo qua, cac ham ghi file se bao loi rieng neu can.
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
    // Bo qua.
  }
}

function appendLog(line: string): void {
  ensureStateDir();
  const stamped = `[${new Date().toISOString()}] ${line}\n`;
  try {
    const existing = existsSync(LOG_FILE) ? readFileSync(LOG_FILE, "utf-8") : "";
    // Giu log gon: chi giu khoang 200 dong cuoi.
    const kept = (existing + stamped).split("\n").slice(-201).join("\n");
    writeFileSync(LOG_FILE, kept, "utf-8");
  } catch {
    // Log hong thi khong duoc lam hong vong poll.
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

function forwardTrayArgs(original: string[]): string[] {
  // Chi chuyen cac tuy chon an toan sang daemon con; --tray tu no khong chuyen.
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

async function spawnTray(): Promise<number> {
  ensureStateDir();
  const existing = readPidFile();
  if (existing && pidAlive(existing)) {
    console.log(`[tray] Daemon da chay roi (PID ${existing}). Dung --status de xem, --stop de thoat.`);
    return 0;
  }
  const script = scriptPath();
  if (!script) {
    console.error("[tray] LOI: khong xac dinh duoc duong dan script de tach daemon.");
    return 2;
  }
  const childArgs = ["run", script, "--tray-child", ...forwardTrayArgs(process.argv.slice(2))];
  const child = spawn(process.execPath, childArgs, { detached: true, stdio: "ignore", env: process.env });
  child.unref();
  // Cho daemon con ghi PID file roi moi ket luan.
  for (let i = 0; i < 20; i++) {
    await Bun.sleep(100);
    const pid = readPidFile();
    if (pid && pidAlive(pid)) {
      console.log(`[tray] Daemon da chay (PID ${pid}). Ban co the tat cua so terminal nay.`);
      console.log(`[tray] Log: ${LOG_FILE}`);
      if (process.platform === "win32") {
        const trayScript = join(dirname(script), "run.ps1");
        if (existsSync(trayScript)) {
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
            ],
            { detached: true, stdio: "ignore" },
          );
          tray.unref();
          console.log("[tray] Icon tray Windows da mo (menu: Mo ZCode / Kiem tra ngay / Mo log / Thoát).");
        } else {
          console.log(`[tray] Khong tim thay run.ps1 o ${trayScript}, daemon van chay nen nhung khong co icon tray.`);
        }
      } else {
        console.log("[tray] He nay chua co menu tray goc: daemon chay nen + thong bao he thong, thoat bang --stop.");
      }
      console.log("[tray] Xem trang thai: bun run deal-checker.ts --status | Thoat: bun run deal-checker.ts --stop");
      return 0;
    }
  }
  console.error("[tray] LOI: daemon khong ghi PID file. Xem log (neu co):");
  try {
    console.error(readFileSync(LOG_FILE, "utf-8").split("\n").slice(-10).join("\n"));
  } catch {
    console.error("(chua co log file)");
  }
  return 1;
}

async function handleStop(): Promise<number> {
  const pid = readPidFile();
  if (!pid) {
    console.log("[tray] Khong co daemon nao dang chay (khong thay PID file).");
    return 0;
  }
  if (!pidAlive(pid)) {
    console.log(`[tray] PID ${pid} da chet roi, don PID file cu.`);
    try {
      unlinkSync(PID_FILE);
    } catch {
      // Bo qua.
    }
    return 0;
  }
  try {
    process.kill(pid, "SIGTERM");
  } catch (error) {
    console.error(`[tray] Khong gui duoc SIGTERM toi PID ${pid}: ${error}`);
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
      // Bo qua.
    }
  }
  try {
    unlinkSync(PID_FILE);
  } catch {
    // Daemon con cung tu xoa PID file khi thoat.
  }
  console.log(`[tray] Da thoat daemon (PID ${pid}). Tren Windows, icon tray se tu bien mat khi thay PID file mat.`);
  return 0;
}

function handleStatus(): number {
  const pid = readPidFile();
  if (pid && pidAlive(pid)) {
    console.log(`[tray] Daemon DANG CHAY (PID ${pid}).`);
  } else if (pid) {
    console.log(`[tray] PID file con ${pid} nhung tien trinh da chet (stale).`);
  } else {
    console.log("[tray] Daemon KHONG chay.");
  }
  let logText: string | null = null;
  try {
    logText = readFileSync(LOG_FILE, "utf-8");
  } catch {
    logText = null;
  }
  if (logText !== null) {
    console.log("[tray] 10 dong log cuoi:");
    for (const line of logText.trim().split("\n").slice(-10)) console.log(`  ${line}`);
  } else {
    console.log("[tray] Chua co log file.");
  }
  const seen = loadSeen();
  console.log(`[tray] Da thay ${seen.size} deal (seen file).`);
  return 0;
}

function resolveAuthForTray(configDir: string): { deviceMid: string; token: string | null } {
  const deviceMid = readDeviceMid(configDir);
  if (!deviceMid) throw new Error("khong tim thay deviceMid trong telemetry-state.json");
  let token: string | null = process.env.ZCODE_JWT_TOKEN?.trim() || null;
  if (!token) token = readZcodeJwt(configDir); // Nem loi neu giai ma that bai.
  return { deviceMid, token };
}

async function sleepWithTriggerCheck(ms: number): Promise<boolean> {
  // Tra ve true neu co file trigger (can check ngay), false neu het gio ngu.
  const step = 2000;
  let remaining = ms;
  while (remaining > 0) {
    if (existsSync(TRIGGER_FILE)) {
      try {
        unlinkSync(TRIGGER_FILE);
      } catch {
        // Bo qua.
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
    log("[tray-child] Nhan SIGTERM, thoat.");
    cleanup();
    process.exit(0);
  });
  process.on("SIGINT", () => {
    log("[tray-child] Nhan SIGINT, thoat.");
    cleanup();
    process.exit(0);
  });
  writeFileSync(PID_FILE, String(process.pid), "utf-8");
  log(`[tray-child] Daemon bat dau (PID ${process.pid}), interval ${intervalMs / 1000}s.`);
  let deviceMid: string;
  let token: string | null;
  try {
    ({ deviceMid, token } = resolveAuthForTray(configDir));
  } catch (error) {
    log(`[tray-child] LOI auth: ${error}`);
    cleanup();
    return 2;
  }
  if (!token) log("[tray-child] Luu y: khong co token, dang check an danh.");
  let seq = 0;
  let failures = 0;
  const seen = loadSeen();
  while (true) {
    try {
      const deals = await queryDeals(token, deviceMid, locale, appVersion, seq);
      seq++;
      failures = 0;
      const fresh = deals.filter((d) => !seen.has(d.key));
      log(`[tray-child] Co ${deals.length} delivery, ${fresh.length} moi: ${deals.map((d) => d.key).join(", ") || "(rong)"}`);
      if (fresh.length > 0) {
        for (const deal of fresh) seen.add(deal.key);
        saveSeen(seen);
        const lines = ["ZCode co deal moi! Mo app va bam nut nhan trong app.", ""];
        for (const deal of fresh) {
          lines.push(`- [${deal.position}] ${deal.campaignId} (priority ${String(deal.priority)})`);
          if (deal.title) lines.push(`  ${deal.title}`);
          if (deal.detail) lines.push(`  ${deal.detail}`);
        }
        lines.push("", "Tool nay chi bao, khong tu nhan thay ban.");
        const message = lines.join("\n");
        writeNotifyFile("ZCode - Co deal moi!", message, fresh.map((d) => d.key).join(","));
        if (isWindowsTray) {
          // Tray Windows se doc notify file va hien BalloonTip + click mo ZCode.
          // Khong goi MessageBox o day vi no la modal chan man hinh.
          log("[tray-child] Da ghi notify file cho tray Windows.");
        } else {
          notify("ZCode - Co deal moi!", message);
        }
        if (autoOpen) log(openZcode());
      }
    } catch (error) {
      failures++;
      const delay = Math.min(600_000, 30_000 * 2 ** Math.min(failures - 1, 5));
      log(`[tray-child] Check loi (${error}). Doi ${Math.round(delay / 1000)}s.`);
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
    if (seenPositions.has(position)) continue; // Moi vi tri chi lay 1 cai dau tien, giong app goc.
    seenPositions.add(position);
    let title = "";
    let detail = "";
    if (position === "banner") {
      const banner = (delivery.banner ?? {}) as Record<string, unknown>;
      const buttons = Array.isArray(banner.buttons) ? banner.buttons : [];
      const labels = buttons.map((b) => textContent((b as Record<string, unknown>).text)).filter(Boolean);
      title = textContent(((banner.success_popup ?? {}) as Record<string, unknown>).title);
      detail = labels.join(" / ") || "(banner, khong co nut doc duoc)";
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
          buttons: [{ text: { format: "plaintext", content: "Nhan ngay" } }],
          success_popup: { title: { format: "plaintext", content: "Nhan thanh cong" } },
        },
      },
      {
        campaign_id: "sample-popup",
        priority: 90,
        resource_position: "popup",
        popup: {
          title: { format: "plaintext", content: "Deal dem" },
          description: { format: "plaintext", content: "300M token GLM-5.3-Flash" },
          buttons: [{ text: { format: "plaintext", content: "Nhan" } }],
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
    console.error(`[checker] khong ghi duoc seen file: ${error}`);
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
    if (!response.ok) throw new Error(`HTTP ${response.status} tu marketing/touch`);
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

async function main(): Promise<number> {
  const args = parseArgs(process.argv.slice(2));
  const configDir = getZcodeConfigDir(typeof args["config-dir"] === "string" ? args["config-dir"] : undefined);
  const intervalMs = args.interval ? Number(args.interval) * 1000 : DEFAULT_INTERVAL_MS;
  const locale = (process.env.ZCODE_LOCALE || "en-US") as string;
  const appVersion = process.env.ZCODE_APP_VERSION || "3.17.2";
  const autoOpen = Boolean(args.open);

  if (args["self-test"]) {
    const deals = parseSnapshot(SAMPLE_SNAPSHOT);
    console.log(`[checker] self-test OK: ${deals.length} delivery (banner 1 + popup 1, ban trung vi tri da bi loai):`);
    for (const deal of deals) console.log(`  - ${deal.key} | ${deal.title} | ${deal.detail}`);
    return 0;
  }

  const summary = describeConfig(configDir);
  if (args["dry-run"]) {
    console.log(`[checker] config dir: ${summary.configDir}`);
    console.log(`[checker] files trong v2: ${summary.files.join(", ") || "(khong thay file nao)"}`);
    console.log(`[checker] deviceMid: ${summary.deviceMid ? summary.deviceMid.slice(0, 8) + "..." : "(khong co)"}`);
    console.log(`[checker] credential keys: ${summary.credentialKeys.join(", ") || "(khong co)"}`);
    console.log(`[checker] co zcodejwttoken: ${summary.hasJwt ? "co" : "khong"}`);
    console.log(`[checker] API: ${API_URL}, locale: ${locale}, app-version: ${appVersion}, interval: ${intervalMs / 1000}s`);
    console.log("[checker] Che do: chi GET marketing/touch, khong POST action, khong claim, khong in token.");
    return 0;
  }

  if (args.tray) return await spawnTray();
  if (args["tray-child"]) return await runTrayChild(args);
  if (args.stop) return await handleStop();
  if (args.status) return handleStatus();

  if (intervalMs < 300_000) {
    console.error("[checker] Canh bao: interval duoi 300s de bi 429. Tool van chay theo so ban dat.");
  }

  const deviceMid = readDeviceMid(configDir);
  if (!deviceMid) {
    console.error("[checker] LOI: khong tim thay deviceMid trong telemetry-state.json.");
    console.error("[checker] Mo ZCode 1 lan de no tao file, hoac kiem tra lai ZCODE_DATA_BASE_DIR.");
    return 2;
  }

  // Thu tu token: env ghi de > credentials.json giai ma. Khong bao gio in token.
  let token: string | null = process.env.ZCODE_JWT_TOKEN?.trim() || null;
  if (!token) {
    try {
      token = readZcodeJwt(configDir);
    } catch (error) {
      console.error(`[checker] LOI: khong giai ma duoc zcodejwttoken (${error}).`);
      console.error("[checker] Thuong do chay khac user/may voi ZCode, hoac ZCODE_CREDENTIAL_SECRET khac voi luc ZCode ma hoa.");
      return 2;
    }
  }
  if (!token) {
    console.error("[checker] Luu y: khong co token, dang check an danh. Deal gan tai khoan co the khong hien.");
  } else {
    console.log("[checker] Da co token tu config (khong hien token ra day).");
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
      console.log(`[checker] Co ${deals.length} delivery, ${fresh.length} moi: ${deals.map((d) => d.key).join(", ") || "(rong)"}`);
      if (fresh.length > 0) {
        for (const deal of fresh) seen.add(deal.key);
        saveSeen(seen);
        const lines = ["ZCode co deal moi! Mo app va bam nut nhan trong app.", ""];
        for (const deal of fresh) {
          lines.push(`- [${deal.position}] ${deal.campaignId} (priority ${String(deal.priority)})`);
          if (deal.title) lines.push(`  ${deal.title}`);
          if (deal.detail) lines.push(`  ${deal.detail}`);
        }
        lines.push("", "Tool nay chi bao, khong tu nhan thay ban.");
        notify("ZCode - Co deal moi!", lines.join("\n"));
        if (autoOpen) console.log(openZcode());
      }
    } catch (error) {
      failures++;
      const delay = Math.min(600_000, 30_000 * 2 ** Math.min(failures - 1, 5));
      console.error(`[checker] Check loi (${error}). Doi ${Math.round(delay / 1000)}s roi thu lai.`);
      if (once) return 1;
      await Bun.sleep(delay);
      continue;
    }
    if (once) return 0;
    await Bun.sleep(intervalMs);
  }
}

process.exitCode = await main();
