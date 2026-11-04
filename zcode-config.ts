// zcode-config.ts -- Doc config ZCode tu ~/.zcode/v2, chay bang Bun, khong can dep.
// Chi doc tren chinh may nay, chi dung cho tai khoan cua chinh ban.
//
// Thu muc goc (giong ZCode, packages/services/src/paths.ts):
//   $ZCODE_DATA_BASE_DIR  >  $HOME  >  os.homedir()
//   + "/.zcode/v2"
// File quan tam nhat:
//   telemetry-state.json  -> { deviceMid }  (UUID plaintext, khong phai bi mat)
//   credentials.json      -> { "zcodejwttoken": "enc:v1:...", ... } (gia tri da ma hoa)
//
// Giai ma credentials (giong ZCode, credentialCipherProvider.ts):
//   AES-256-GCM, key = SHA256(secret)
//   secret = $ZCODE_CREDENTIAL_SECRET neu co,
//            neu khong: `zcode-credential-fallback:<platform>:<homedir>:<username>`
//   Dinh dang: "enc:v1:<iv base64url>.<authTag base64url>.<ciphertext base64url>"
//   Gia tri khong co tien to "enc:v1:" thi tra nguyen (giong app goc).
//
// He qua an ninh can noi thang:
//   - Neu khong dat ZCODE_CREDENTIAL_SECRET, khoa giai ma chi suy ra tu
//     platform + homedir + username, nen bat cu ai chay duoc voi cung
//     user tren cung may nay cung giai ma duoc. Day la thiet ke cua
//     ZCode, khong phai do tool nay lam yeu di.
//   - Neu ZCODE_CREDENTIAL_SECRET duoc dat, tool phai chay voi cung
//     bien moi truong do moi giai ma duoc; sai secret -> fail-closed,
//     tool bao loi chu khong doan token.
//   - Tool khong bao gio in token, khong ghi token ra file/log, chi
//     giu trong RAM va gui lam header Authorization toi zcode.z.ai.

import { createCipheriv, createDecipheriv, createHash, randomBytes } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { homedir, platform, userInfo } from "node:os";
import { join } from "node:path";

export function getZcodeConfigDir(override?: string): string {
  if (override) return override;
  const envBase = process.env.ZCODE_DATA_BASE_DIR?.trim();
  if (envBase) return join(envBase, ".zcode", "v2");
  const home = process.env.HOME?.trim() || homedir();
  return join(home, ".zcode", "v2");
}

export function listConfigFiles(configDir: string): string[] {
  // Chi liet ke ten file, khong doc noi dung nhay cam.
  const names = [
    "telemetry-state.json",
    "credentials.json",
    "config.json",
    "onboarding-record.json",
    "data-sync-state.json",
  ];
  return names.filter((name) => existsSync(join(configDir, name)));
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

// Chi dung cho test/fixture cuc bo, khong dung trong tool chay that.
export function encryptCredentialForTest(value: string): string {
  const iv = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", cipherKey(), iv);
  const encrypted = Buffer.concat([cipher.update(value, "utf-8"), cipher.final()]);
  const authTag = cipher.getAuthTag();
  return `enc:v1:${iv.toString("base64url")}.${authTag.toString("base64url")}.${encrypted.toString("base64url")}`;
}

export function readCredentialKeys(configDir: string): string[] {
  const data = readJsonFile(join(configDir, "credentials.json"));
  return data ? Object.keys(data) : [];
}

export function readZcodeJwt(configDir: string): string | null {
  const data = readJsonFile(join(configDir, "credentials.json"));
  const raw = data?.["zcodejwttoken"];
  if (typeof raw !== "string" || !raw.trim()) return null;
  const plain = decryptCredential(raw.trim());
  return plain.trim() ? plain.trim() : null;
}

export function mask(value: string | null): string {
  if (!value) return "(khong co)";
  return value.length <= 8 ? "(co, da an)" : `${value.slice(0, 8)}...`;
}

export interface ConfigSummary {
  configDir: string;
  files: string[];
  deviceMid: string | null;
  credentialKeys: string[];
  hasJwt: boolean;
}

export function describeConfig(configDir: string): ConfigSummary {
  let hasJwt = false;
  try {
    hasJwt = readZcodeJwt(configDir) !== null;
  } catch {
    hasJwt = false;
  }
  return {
    configDir,
    files: listConfigFiles(configDir),
    deviceMid: readDeviceMid(configDir),
    credentialKeys: readCredentialKeys(configDir),
    hasJwt,
  };
}
