#!/usr/bin/env python3
"""
ZCode deal-dem watcher -- CHI BAO, KHONG TU NHAN.

Muc dich:
    Quet theo khung gio deal dem mo (mac dinh tu 17:55 gio VN) va hien
    popup tren may ban, de ban tu mo ZCode va bam banner/modal trong
    app chinh thuc. Ban van la nguoi bam nut nhan.

Tool nay KHONG:
    - Doc, lay hay gui ZCode JWT / token cua ban di dau.
    - Goi API nao cua z.ai / ZCode.
    - Tu dong bam nut nhan, tu dong click trong app ZCode.
    - Quet man hinh / OCR lien tuc (banner nam trong Electron, khong co
      tin hieu file cuc bo on dinh de quet dang tin cay; quet anh man
      hinh lien tuc vua nang vua de bao sai).

Cach chay (tren may co cai ZCode, can Python 3.10+):
    Chay nen, no tu cho toi khung gio:
        python zcode_popup_watcher.py

    Hien popup ngay lap tuc de test giao dien (khong doi toi 17:55):
        python zcode_popup_watcher.py --test

    Chi in ra ke hoach, khong hien popup (de kiem tra logic):
        python zcode_popup_watcher.py --dry-run

    Chi hien 1 lan roi thoat (dung voi Task Scheduler / cron / launchd):
        python zcode_popup_watcher.py --once

    Tuy chinh khung gio, vi du bao tu 17:50:
        python zcode_popup_watcher.py --from 17:50 --to 22:10

Trang thai "da bao hom nay" luu o:
    ~/.zcode_popup_watcher/state.json  (chi co ngay, khong co token)

Nut trong popup:
    - "Mo ZCode": thu mo app ZCode neu tim thay duong dan cai dat
      thong dung tren Windows / macOS / Linux. Neu khong tim thay,
      popup se bao ban tu mo app.
    - "Da nhan xong": tat popup va khong bao lai trong ngay hom do.
    - "Nhac lai sau 10 phut": tam an popup, 10 phut sau hien lai.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import time
from datetime import datetime, time as dtime
from pathlib import Path

try:
    from zoneinfo import ZoneInfo
except ImportError:  # Python < 3.9, khong xay ra voi yeu cau 3.10+
    ZoneInfo = None  # type: ignore

APP_NAME = "ZCode"
DEFAULT_FROM = "17:55"
DEFAULT_TO = "22:10"
SNOOZE_MINUTES = 10
STATE_DIR = Path.home() / ".zcode_popup_watcher"
STATE_FILE = STATE_DIR / "state.json"

MESSAGE = (
    "Toi gio nhan deal dem ZCode!\n\n"
    "1. Mo ZCode.\n"
    "2. Bam banner/modal deal dem trong app.\n"
    "3. Tu 22:00 moi dung duoc, toi 08:00 sang hom sau reset.\n"
    "4. Nho chon dung GLM-5.3-Flash + Global Building\n"
    "   thi moi tru vao goi free dem do.\n\n"
    "Tool nay chi bao, khong tu bam nhan thay ban."
)


def vn_now() -> datetime:
    if ZoneInfo is not None:
        try:
            return datetime.now(ZoneInfo("Asia/Ho_Chi_Minh"))
        except Exception:
            pass
    # Fallback: gio VN = UTC+7, khong can thu vien ngoai.
    from datetime import timezone, timedelta

    return datetime.now(timezone(timedelta(hours=7)))


def parse_hhmm(value: str) -> dtime:
    hour, minute = value.split(":", 1)
    return dtime(int(hour), int(minute))


def in_window(now: datetime, start: dtime, end: dtime) -> bool:
    current = now.time().replace(second=0, microsecond=0)
    if start <= end:
        return start <= current <= end
    return current >= start or current <= end


def load_state() -> dict:
    try:
        return json.loads(STATE_FILE.read_text(encoding="utf-8"))
    except Exception:
        return {}


def save_state(state: dict) -> None:
    try:
        STATE_DIR.mkdir(parents=True, exist_ok=True)
        STATE_FILE.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    except Exception as exc:  # Khong bao thi van khong duoc lam hong tool
        print(f"[watcher] khong ghi duoc state: {exc}", file=sys.stderr)


def find_zcode_executable() -> str | None:
    found = shutil.which("zcode") or shutil.which("ZCode")
    if found:
        return found
    candidates: list[str] = []
    if sys.platform == "win32":
        local = os.environ.get("LOCALAPPDATA", "")
        pf = os.environ.get("ProgramFiles", "")
        if local:
            candidates.append(os.path.join(local, "Programs", "ZCode", "ZCode.exe"))
            candidates.append(os.path.join(local, "ZCode", "ZCode.exe"))
        if pf:
            candidates.append(os.path.join(pf, "ZCode", "ZCode.exe"))
    elif sys.platform == "darwin":
        candidates.append("/Applications/ZCode.app/Contents/MacOS/ZCode")
    else:
        candidates.extend(["/usr/bin/zcode", "/usr/local/bin/zcode", "/opt/ZCode/zcode"])
    for path in candidates:
        if path and os.path.isfile(path):
            return path
    return None


def open_zcode() -> str:
    exe = find_zcode_executable()
    if not exe:
        return "Khong tim thay ZCode tren may nay, ban mo app thu cong nhe."
    try:
        if sys.platform == "darwin" and exe.endswith(".app/Contents/MacOS/ZCode"):
            subprocess.Popen(["open", "/Applications/ZCode.app"])
        elif sys.platform == "win32":
            os.startfile(exe)  # type: ignore[attr-defined]
        else:
            subprocess.Popen([exe])
        return f"Da goi mo ZCode: {exe}"
    except Exception as exc:
        return f"Khong mo duoc ZCode tu dong ({exc}), ban mo app thu cong nhe."


def show_popup() -> str:
    """Hien popup tkinter, tra ve: 'done' | 'snooze' | 'closed'."""
    import tkinter as tk
    from tkinter import messagebox

    result = {"action": "closed"}

    root = tk.Tk()
    root.title("ZCode - Toi gio nhan deal dem!")
    root.attributes("-topmost", True)
    root.resizable(False, False)

    label = tk.Label(root, text=MESSAGE, justify="left", padx=20, pady=16, font=("Segoe UI", 11))
    label.pack()

    status = tk.Label(root, text="", fg="gray", padx=20, pady=4)
    status.pack()

    def do_open() -> None:
        status.config(text=open_zcode())

    def do_done() -> None:
        result["action"] = "done"
        root.destroy()

    def do_snooze() -> None:
        result["action"] = "snooze"
        root.destroy()

    buttons = tk.Frame(root, padx=20, pady=12)
    buttons.pack()
    tk.Button(buttons, text="Mo ZCode", command=do_open, width=14).pack(side="left", padx=4)
    tk.Button(buttons, text="Da nhan xong", command=do_done, width=14).pack(side="left", padx=4)
    tk.Button(buttons, text="Nhac lai sau 10 phut", command=do_snooze, width=18).pack(
        side="left", padx=4
    )

    # Can giua man hinh, tranh popup an sau cua so khac.
    root.update_idletasks()
    width, height = root.winfo_width(), root.winfo_height()
    x = (root.winfo_screenwidth() - width) // 2
    y = (root.winfo_screenheight() - height) // 3
    root.geometry(f"+{max(x, 0)}+{max(y, 0)}")
    try:
        root.lift()
        root.focus_force()
    except Exception:
        pass
    root.mainloop()
    return result["action"]


def main() -> int:
    parser = argparse.ArgumentParser(description="ZCode deal-dem watcher (chi bao, khong tu nhan).")
    parser.add_argument("--from", dest="from_time", default=DEFAULT_FROM, help="Gio bat dau, vd 17:55")
    parser.add_argument("--to", dest="to_time", default=DEFAULT_TO, help="Gio ket thuc, vd 22:10")
    parser.add_argument("--interval", type=int, default=60, help="So giay giua cac lan kiem tra (mac dinh 60)")
    parser.add_argument("--once", action="store_true", help="Hien 1 lan roi thoat")
    parser.add_argument("--test", action="store_true", help="Hien popup ngay, khong doi khung gio")
    parser.add_argument("--dry-run", action="store_true", help="Chi in ke hoach, khong hien popup")
    args = parser.parse_args()

    start = parse_hhmm(args.from_time)
    end = parse_hhmm(args.to_time)
    now = vn_now()

    if args.dry_run:
        print(f"[watcher] Gio VN hien tai: {now:%Y-%m-%d %H:%M}")
        print(f"[watcher] Khung bao: {args.from_time} - {args.to_time} (gio VN)")
        print(f"[watcher] Trong khung gio: {in_window(now, start, end)}")
        print(f"[watcher] ZCode tim thay: {find_zcode_executable() or 'khong thay (se bao mo thu cong)'}")
        print("[watcher] Che do: chi bao, khong doc token, khong goi API, khong tu bam nhan.")
        return 0

    if args.test or args.once:
        action = show_popup()
        if action == "done":
            state = load_state()
            state["notified_date"] = now.date().isoformat()
            save_state(state)
        print(f"[watcher] Popup dong voi action: {action}")
        return 0

    print(f"[watcher] Dang chay nen. Se bao trong khung {args.from_time}-{args.to_time} gio VN.")
    print("[watcher] Nhan Ctrl+C de dung.")
    snooze_until: float | None = None
    try:
        while True:
            now = vn_now()
            state = load_state()
            today = now.date().isoformat()
            if state.get("notified_date") == today:
                # Da nhan xong hom nay thi cho sang ngay moi roi kiem tra tiep.
                time.sleep(args.interval)
                continue
            if snooze_until and time.time() < snooze_until:
                time.sleep(min(args.interval, snooze_until - time.time()))
                continue
            if in_window(now, start, end):
                action = show_popup()
                if action == "done":
                    state["notified_date"] = today
                    save_state(state)
                elif action == "snooze":
                    snooze_until = time.time() + SNOOZE_MINUTES * 60
                # 'closed' (tat cua so bang nut X): khong danh dau da nhan,
                # vong sau se hien lai neu van trong khung gio.
            else:
                time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\n[watcher] Da dung.")
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
