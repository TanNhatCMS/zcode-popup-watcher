// notify.ts -- Hien thong bao ra man hinh, chay bang Bun, khong can dep.
// Thu tu uu tien theo he dieu hanh, cai nao khong co thi roi xuong console:
//   macOS   -> osascript "display notification"
//   Linux   -> notify-send (neu co cai)
//   Windows -> PowerShell MessageBox (System.Windows.Forms, co san theo .NET)
//   Fallback -> in ra console + bip (khong im lang mat thong bao).
// Ham nay chi BAO, khong mo app, khong click gi trong ZCode.

import { execFileSync, spawn } from "node:child_process";
import { platform } from "node:os";

function escapeForAppleScript(text: string): string {
  return text.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function escapeForPowerShell(text: string): string {
  return text.replace(/'/g, "''");
}

function runQuiet(cmd: string, args: string[]): boolean {
  try {
    execFileSync(cmd, args, { stdio: "ignore", timeout: 10_000 });
    return true;
  } catch {
    return false;
  }
}

export function notify(title: string, message: string): void {
  const os = platform();
  const shortMessage = message.length > 900 ? message.slice(0, 900) + "..." : message;

  if (os === "darwin") {
    const script = `display notification "${escapeForAppleScript(shortMessage)}" with title "${escapeForAppleScript(title)}"`;
    if (runQuiet("osascript", ["-e", script])) return;
  }

  if (os === "linux") {
    if (runQuiet("notify-send", ["--urgency=normal", title, shortMessage])) return;
  }

  if (os === "win32") {
    const ps = [
      "Add-Type -AssemblyName System.Windows.Forms",
      `[System.Windows.Forms.MessageBox]::Show('${escapeForPowerShell(shortMessage)}', '${escapeForPowerShell(title)}', 'OK', 'Information') | Out-Null`,
    ].join("; ");
    if (runQuiet("powershell", ["-NoProfile", "-NonInteractive", "-Command", ps])) return;
  }

  // Fallback: khong co cong cu thong bao he thong thi van phai thay duoc.
  console.log(`\n=== ${title} ===\n${message}\n`);
  try {
    process.stdout.write("\x07");
  } catch {
    // Bo qua neu khong bip duoc.
  }
}

export function openZcode(): string {
  const os = platform();
  const candidates: string[] = [];
  if (os === "win32") {
    const local = process.env.LOCALAPPDATA || "";
    if (local) candidates.push(`${local}\\Programs\\ZCode\\ZCode.exe`);
  } else if (os === "darwin") {
    candidates.push("/Applications/ZCode.app");
  } else {
    candidates.push("/usr/bin/zcode", "/usr/local/bin/zcode");
  }
  for (const target of candidates) {
    try {
      if (os === "darwin") {
        spawn("open", [target], { detached: true, stdio: "ignore" }).unref();
      } else if (os === "win32") {
        spawn("cmd", ["/c", "start", "", target], { detached: true, stdio: "ignore" }).unref();
      } else {
        spawn(target, [], { detached: true, stdio: "ignore" }).unref();
      }
      return `Da goi mo ZCode: ${target}`;
    } catch {
      // Thu ung vien tiep theo.
    }
  }
  return "Khong tim thay ZCode tren may nay, ban mo app thu cong nhe.";
}
