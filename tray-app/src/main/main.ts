// main.ts -- Tray app Electron cho ZCode Deal Watcher.
//
// Thay the run.ps1: icon tray + menu la native cua Electron (Tray,
// Menu), khong qua PowerShell, nen khong bi execution policy,
// khong bi loi enum WinForms, khong can file script roi rac.
// Cua so React (renderer) chi mo khi user chon "Mo bang dieu khien"
// hoac bam vao icon tray; binh thuong app chi song o khay he thong.
//
// Chi BAO deal, khong tu nhan thay ban. Khong POST action, khong
// claim plan. Token chi doc tu ~/.zcode/v2 tren cung may/user,
// khong in, khong ghi file.

import { app, BrowserWindow, Menu, Notification, Tray, ipcMain, shell } from "electron";
import { existsSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  DEFAULT_INTERVAL_MS,
  LOG_FILE,
  STATE_DIR,
  appendLog,
  ensureStateDir,
  getZcodeConfigDir,
  loadSeen,
  queryDeals,
  readDeviceMid,
  readZcodeJwt,
  saveSeen,
  sleep,
  writeNotifyFile,
} from "./checker";

const PID_FILE = join(STATE_DIR, "electron-tray.pid");

let tray: Tray | null = null;
let window: BrowserWindow | null = null;
let quitting = false;
let polling = false;
let checkRequested = false;
let seq = 0;
let failures = 0;
let lastCheck: string | null = null;
let lastResult = "Chua check lan nao.";
let intervalMs = DEFAULT_INTERVAL_MS;

function iconPath(): string {
  // Ban dong goi: assets nam cung dist. Ban dev: assets o goc tray-app.
  const packed = join(__dirname, "..", "..", "assets", process.platform === "win32" ? "icon.ico" : "icon.png");
  if (existsSync(packed)) return packed;
  return join(app.getAppPath(), "assets", process.platform === "win32" ? "icon.ico" : "icon.png");
}

function log(line: string): void {
  console.log(line);
  appendLog(line);
}

function statusSnapshot(): {
  running: boolean;
  lastCheck: string | null;
  lastResult: string;
  dealsSeen: number;
  intervalSeconds: number;
  logTail: string[];
} {
  let logTail: string[] = [];
  try {
    logTail = readFileSync(LOG_FILE, "utf-8").trim().split("\n").slice(-30);
  } catch {
    logTail = [];
  }
  return {
    running: true,
    lastCheck,
    lastResult,
    dealsSeen: loadSeen().size,
    intervalSeconds: Math.round(intervalMs / 1000),
    logTail,
  };
}

function pushStatus(): void {
  if (window && !window.isDestroyed()) {
    window.webContents.send("tray:status", statusSnapshot());
  }
}

function createWindow(): BrowserWindow {
  if (window && !window.isDestroyed()) {
    window.show();
    window.focus();
    return window;
  }
  window = new BrowserWindow({
    width: 720,
    height: 560,
    title: "ZCode Deal Watcher",
    autoHideMenuBar: true,
    webPreferences: {
      preload: join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  window.loadFile(join(__dirname, "..", "renderer", "index.html"));
  window.on("closed", () => {
    window = null;
  });
  return window;
}

function openZcode(): string {
  const candidates: string[] = [];
  if (process.platform === "win32") {
    const local = process.env.LOCALAPPDATA || "";
    if (local) {
      candidates.push(join(local, "Programs", "ZCode", "ZCode.exe"));
      candidates.push(join(local, "ZCode", "ZCode.exe"));
    }
    const programFiles = process.env.ProgramFiles || "";
    if (programFiles) candidates.push(join(programFiles, "ZCode", "ZCode.exe"));
  } else if (process.platform === "darwin") {
    candidates.push("/Applications/ZCode.app");
  } else {
    candidates.push("/usr/bin/zcode", "/usr/local/bin/zcode");
  }
  for (const target of candidates) {
    if (!existsSync(target)) continue;
    shell.openPath(target).catch(() => {
      // Bo qua, thu ung vien tiep theo o lan goi sau.
    });
    return `Da goi mo ZCode: ${target}`;
  }
  return "Khong tim thay ZCode tren may nay, ban mo app thu cong nhe.";
}

function openLog(): string {
  shell.openPath(LOG_FILE).catch(() => {
    // Bo qua neu khong mo duoc.
  });
  return `Da goi mo log: ${LOG_FILE}`;
}

function buildMenu(): void {
  if (!tray) return;
  const menu = Menu.buildFromTemplate([
    {
      label: "Mo ZCode",
      click: () => {
        log(openZcode());
      },
    },
    {
      label: "Kiem tra ngay",
      click: () => {
        checkRequested = true;
      },
    },
    {
      label: "Mo bang dieu khien",
      click: () => {
        createWindow();
      },
    },
    {
      label: "Mo log",
      click: () => {
        log(openLog());
      },
    },
    { type: "separator" },
    {
      label: "Thoat",
      click: () => {
        app.quit();
      },
    },
  ]);
  tray.setContextMenu(menu);
  tray.setToolTip("ZCode Deal Watcher");
}

async function pollLoop(): Promise<void> {
  if (polling) return;
  polling = true;
  const configDir = getZcodeConfigDir();
  const locale = process.env.ZCODE_LOCALE || "en-US";
  const appVersion = process.env.ZCODE_APP_VERSION || "3.17.2";
  const autoOpen = process.env.ZCODE_TRAY_AUTO_OPEN === "1";
  let deviceMid: string | null = null;
  let token: string | null = null;
  try {
    deviceMid = readDeviceMid(configDir);
    if (!deviceMid) throw new Error("khong tim thay deviceMid trong telemetry-state.json");
    token = process.env.ZCODE_JWT_TOKEN?.trim() || readZcodeJwt(configDir);
  } catch (error) {
    log(`[electron] LOI auth: ${error}`);
    lastResult = `Loi auth: ${error}`;
    pushStatus();
    return;
  }
  if (!token) log("[electron] Luu y: khong co token, dang check an danh.");
  if (intervalMs < 300_000) {
    log("[electron] Canh bao: interval duoi 300s de bi 429.");
  }
  const seen = loadSeen();
  while (!quitting) {
    try {
      const deals = await queryDeals(token, deviceMid, locale, appVersion, seq);
      seq++;
      failures = 0;
      const fresh = deals.filter((d) => !seen.has(d.key));
      lastCheck = new Date().toISOString();
      lastResult = `Co ${deals.length} delivery, ${fresh.length} moi.`;
      log(`[electron] Co ${deals.length} delivery, ${fresh.length} moi: ${deals.map((d) => d.key).join(", ") || "(rong)"}`);
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
        const note = new Notification({ title: "ZCode - Co deal moi!", body: message.slice(0, 500) });
        note.on("click", () => {
          log(openZcode());
        });
        note.show();
        if (autoOpen) log(openZcode());
      }
    } catch (error) {
      failures++;
      const delay = Math.min(600_000, 30_000 * 2 ** Math.min(failures - 1, 5));
      lastResult = `Check loi: ${error}`;
      log(`[electron] Check loi (${error}). Doi ${Math.round(delay / 1000)}s.`);
      pushStatus();
      await sleep(delay);
      continue;
    }
    pushStatus();
    // Cho het interval, nhung neu user bam "Kiem tra ngay" thi check som.
    const step = 1000;
    let remaining = intervalMs;
    checkRequested = false;
    while (remaining > 0 && !quitting) {
      if (checkRequested) break;
      const wait = Math.min(step, remaining);
      await sleep(wait);
      remaining -= wait;
    }
  }
}

function createTray(): void {
  const icon = iconPath();
  tray = new Tray(icon);
  tray.setToolTip("ZCode Deal Watcher");
  buildMenu();
  // Bam vao icon thi mo bang dieu khien (menu van mo bang bam phai).
  tray.on("click", () => {
    createWindow();
  });
  log(`[electron] Tray icon da tao (icon: ${icon}).`);
}

app.on("ready", () => {
  ensureStateDir();
  try {
    writeFileSync(PID_FILE, String(process.pid), "utf-8");
  } catch {
    // Bo qua.
  }
  createTray();
  void pollLoop();
});

app.on("window-all-closed", () => {
  // App tray khong thoat khi dong cua so, chi thoat bang menu Thoat.
});

app.on("before-quit", () => {
  quitting = true;
  try {
    if (existsSync(PID_FILE)) unlinkSync(PID_FILE);
  } catch {
    // Bo qua.
  }
});

ipcMain.handle("tray:get-status", () => statusSnapshot());
ipcMain.handle("tray:check-now", () => {
  checkRequested = true;
  return "Da yeu cau kiem tra ngay.";
});
ipcMain.handle("tray:open-zcode", () => openZcode());
ipcMain.handle("tray:open-log", () => openLog());
ipcMain.handle("tray:quit", () => {
  app.quit();
});
