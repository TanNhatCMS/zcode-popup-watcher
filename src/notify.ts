// notify.ts -- Hiện thông báo ra màn hình, chạy bằng Bun, không cần dep.
// Thứ tự ưu tiên theo hệ điều hành, cái nào không có thì rồi xuống console:
//   macOS   -> osascript "display notification"
//   Linux   -> notify-send (nếu có cái)
//   Windows -> PowerShell MessageBox (System.Windows.Forms, có san theo .NET)
//   Fallback -> in ra console + bip (không im lặng mất thông báo).
// Hàm này chỉ BÁO, không mở app, không click gi trong ZCode.

import { execFileSync, spawn } from "node:child_process";
import { existsSync } from "node:fs";
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

  // Fallback: không có công cụ thông báo hệ thống thì vẫn phải thấy được.
  console.log(`\n=== ${title} ===\n${message}\n`);
  try {
    process.stdout.write("\x07");
  } catch {
    // Bỏ qua nếu không bip được.
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
    // Chỉ gọi mở khi file/app thật sự tồn tại, không đoán mở.
    if (!existsSync(target)) continue;
    try {
      if (os === "darwin") {
        spawn("open", [target], { detached: true, stdio: "ignore" }).unref();
      } else if (os === "win32") {
        spawn("cmd", ["/c", "start", "", target], { detached: true, stdio: "ignore" }).unref();
      } else {
        spawn(target, [], { detached: true, stdio: "ignore" }).unref();
      }
      return `Đã gọi mở ZCode: ${target}`;
    } catch {
      // Thử ung vien tiếp theo.
    }
  }
  return "Không tìm thấy ZCode trên máy này, bạn mở app thủ công nhé.";
}
