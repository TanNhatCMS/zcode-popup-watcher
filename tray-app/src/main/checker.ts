// checker.ts -- Logic kiem tra deal ZCode, port tu src/deal-checker.ts
// (Bun) sang Node (Electron main). Khong dung API rieng cua Bun:
// sleep bang setTimeout, fetch/AbortController cua Node 18+.
// State dung chung voi CLI o ~/.zcode/mod de khong bao trung:
// seen_deals.json, deal-checker.log.

import { createDecipheriv, createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, renameSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir, platform, userInfo } from "node:os";
import { join } from "node:path";

export const API_URL = process.env.ZCODE_MARKETING_URL || "https://zcode.z.ai/api/v1/marketing/touch";
export const DEFAULT_INTERVAL_MS = 600_000;
export const STATE_DIR = join(homedir(), ".zcode", "mod");
export const SEEN_FILE = join(STATE_DIR, "seen_deals.json");
export const LOG_FILE = join(STATE_DIR, "deal-checker.log");
export const NOTIFY_FILE = join(STATE_DIR, "deal-notify.json");

export function ensureStateDir(): void {
  try {
    mkdirSync(STATE_DIR, { recursive: true });
  } catch {
    // Bo qua, ham ghi file se bao loi rieng neu can.
  }
}

export function appendLog(line: string): void {
  ensureStateDir();
  const stamped = `[${new Date().toISOString()}] ${line}\n`;
  try {
    const existing = existsSync(LOG_FILE) ? readFileSync(LOG_FILE, "utf-8") : "";
    const kept = (existing + stamped).split("\n").slice(-201).join("\n");
    writeFileSync(LOG_FILE, kept, "utf-8");
  } catch {
    // Log hong thi khong duoc lam hong vong poll.
  }
}

export function getZcodeConfigDir(override?: string): string {
  if (override) return override;
  const envBase = process.env.ZCODE_DATA_BASE_DIR?.trim();
  if (envBase) return join(envBase, ".zcode", "v2");
  const home = process.env.HOME?.trim() || homedir();
  return join(home, ".zcode", "v2");
}

function readJsonFile(path: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(readFileSync(path, "utf-8"));
    return typeof parsed === "object" && parsed !== null ? (parsed as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

export function readDeviceMid(configDir: string): string | null {
  const data = readJsonFile(join(configDir, "telemetry-state.json"));
  const value = data?.deviceMid;
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function cipherKey(): Buffer {
  const configured = process.env.ZCODE_CREDENTIAL_SECRET?.trim();
  let username = "unknown";
  try {
    username = userInfo().username || "unknown";
  } catch {
    // Giong app goc: khong lay duoc username thi dung "unknown".
  }
  const secret = configured || `zcode-credential-fallback:${platform()}:${homedir()}:${username}`;
  return createHash("sha256").update(secret).digest();
}

export function decryptCredential(value: string): string {
  const prefix = "enc:v1:";
  if (!value.startsWith(prefix)) return value;
  const parts = value.slice(prefix.length).split(".");
  if (parts.length !== 3) throw new Error("credential ciphertext format invalid");
  const [ivRaw, authTagRaw, cipherRaw] = parts;
  if (!ivRaw || !authTagRaw || cipherRaw === undefined) {
    throw new Error("credential ciphertext format invalid");
  }
  const iv = Buffer.from(ivRaw, "base64url");
  const authTag = Buffer.from(authTagRaw, "base64url");
  const cipherText = Buffer.from(cipherRaw, "base64url");
  if (iv.length !== 12) throw new Error("credential IV length invalid");
  if (authTag.length !== 16) throw new Error("credential authTag length invalid");
  const decipher = createDecipheriv("aes-256-gcm", cipherKey(), iv);
  decipher.setAuthTag(authTag);
  return Buffer.concat([decipher.update(cipherText), decipher.final()]).toString("utf-8");
}

export function readZcodeJwt(configDir: string): string | null {
  const data = readJsonFile(join(configDir, "credentials.json"));
  const raw = data?.["zcodejwttoken"];
  if (typeof raw !== "string" || !raw.trim()) return null;
  const plain = decryptCredential(raw.trim());
  return plain.trim() ? plain.trim() : null;
}

export interface Deal {
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
    results.push({ key: `${position}:${campaignId}`, campaignId, position, priority: delivery.priority, title, detail });
  }
  return results;
}

export function loadSeen(): Set<string> {
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

export function saveSeen(seen: Set<string>): void {
  try {
    ensureStateDir();
    writeFileSync(SEEN_FILE, JSON.stringify([...seen].sort(), null, 2), "utf-8");
  } catch (error) {
    appendLog(`[electron] khong ghi duoc seen file: ${error}`);
  }
}

export async function queryDeals(
  token: string | null,
  deviceMid: string,
  locale: string,
  appVersion: string,
  seq: number,
): Promise<Deal[]> {
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

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function writeNotifyFile(title: string, message: string, id: string): void {
  ensureStateDir();
  const tmp = `${NOTIFY_FILE}.tmp`;
  writeFileSync(tmp, JSON.stringify({ id, title, message, time: new Date().toISOString() }, null, 2), "utf-8");
  renameSync(tmp, NOTIFY_FILE);
}

export function removeFileIfExists(path: string): void {
  try {
    if (existsSync(path)) unlinkSync(path);
  } catch {
    // Bo qua.
  }
}
