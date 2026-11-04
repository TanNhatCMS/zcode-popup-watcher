#!/usr/bin/env python3
"""
ZCode deal checker -- CHI DOC, KHONG NHAN HO.

Tool nay goi dung endpoint ma chinh app ZCode van poll moi 10 phut
o ban production:
    GET https://zcode.z.ai/api/v1/marketing/touch?seq=<tang dan>

Neu server tra ve delivery (banner va/hoac popup) ma truoc do
chua tung thay, tool hien popup tren man hinh de ban tu mo ZCode
va bam nut nhan trong app chinh thuc.

Tool nay KHONG:
    - Goi POST /api/v1/marketing/touch/action (khong bao confirm/cancel).
    - Goi claim plan, khong tu dong nhan deal thay ban.
    - In, ghi log hay gui token cua ban di bat cu dau.
    - Doc file credentials.json cua ZCode (gia tri trong do da duoc
      ma hoa, tool khong giai ma va khong co gang giai ma).

Token lay o dau, an toan the nao:
    - Tool chi doc token tu bien moi truong tren chinh may ban,
      hoac hoi bang getpass khi chay (nhap xong chi nam trong RAM
      cua phien chay do, khong ghi xuong dia).
    - TUYET DOI khong dan token vao chat, khong gui cho ai, khong
      commit vao repo, khong de trong file .py nay.
    - Neu khong co token, tool van goi duoc o che do an danh
      (khong kem Authorization), nhung deal gan voi tai khoan co
      the se khong hien -- luc do ban can chay kem token cua
      chinh tai khoan do moi thay dung deal cua minh.
    - DeviceMid thi khong phai bi mat: no la UUID nam o file
      plaintext ~/.zcode/v2/telemetry-state.json (hoac
      $ZCODE_DATA_BASE_DIR/.zcode/v2/telemetry-state.json),
      tool chi doc truong deviceMid trong file do.

Tan suat:
    - Mac dinh 600 giay (10 phut), dung tan suat cua app goc.
    - Dung dat duoi 300 giay: de bi 429, va neu app ZCode cung
      dang mo thi ban dang poll gap doi app.
    - Loi lien tiep thi tool tu doi gap doi toi da 10 phut,
      giong co che backoff cua app goc.

Cach chay (Python 3.10+, chi can thu vien chuan + tkinter):
    Kiem tra 1 lan roi thoat:
        ZCODE_JWT_TOKEN="<token-cua-ban>" python zcode_deal_checker.py --once

    Chay nen, cu 10 phut check 1 lan, co deal moi thi hien popup:
        ZCODE_JWT_TOKEN="<token-cua-ban>" python zcode_deal_checker.py

    Khong co token, chi check an danh:
        python zcode_deal_checker.py --once

    Tu test parse snapshot mau, khong goi API that:
        python zcode_deal_checker.py --self-test

    Chi in cau hinh se dung, khong goi API:
        python zcode_deal_checker.py --dry-run

Ngon ngu tra ve: tool gui X-Client-Language theo --locale,
chi nhan zh-CN hoac en-US (dung nhu app goc).
"""

from __future__ import annotations

import argparse
import getpass
import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

API_URL = "https://zcode.z.ai/api/v1/marketing/touch"
DEFAULT_INTERVAL = 600
DEFAULT_LOCALE = "en-US"
STATE_DIR = Path.home() / ".zcode_popup_watcher"
SEEN_FILE = STATE_DIR / "seen_deals.json"

SAMPLE_SNAPSHOT = {
    "code": 0,
    "data": {
        "server_time": 1793000000,
        "language": "en-US",
        "deliveries": [
            {
                "campaign_id": "sample-night-deal",
                "priority": 80,
                "resource_position": "banner",
                "banner": {
                    "buttons": [
                        {"text": {"format": "plaintext", "content": "Nhan ngay"}, "action": {"type": "claim_zcode_plan", "args": {"plan_id": "sample-plan"}}},
                    ],
                    "success_popup": {"title": {"format": "plaintext", "content": "Nhan thanh cong"}},
                },
            },
            {
                "campaign_id": "sample-popup",
                "priority": 90,
                "resource_position": "popup",
                "popup": {
                    "title": {"format": "plaintext", "content": "Deal dem"},
                    "description": {"format": "plaintext", "content": "300M token GLM-5.3-Flash"},
                    "buttons": [
                        {"text": {"format": "plaintext", "content": "Nhan"}, "action": {"type": "claim_zcode_plan", "args": {"plan_id": "sample-plan"}}},
                    ],
                },
            },
            # Delivery thu 2 cung vi tri banner phai bi loai, giong app goc.
            {
                "campaign_id": "sample-banner-duplicate",
                "priority": 10,
                "resource_position": "banner",
                "banner": {"buttons": []},
            },
        ],
    },
}


def text_content(node) -> str:
    if isinstance(node, dict):
        content = node.get("content")
        return content.strip() if isinstance(content, str) else ""
    return ""


def parse_snapshot(payload: dict) -> list[dict]:
    """Tra ve deliveries hop le, moi vi tri banner/popup chi lay 1 cai dau tien."""
    if not isinstance(payload, dict) or payload.get("code") != 0:
        raise ValueError(f"Snapshot khong hop le: code={payload.get('code') if isinstance(payload, dict) else 'not-a-dict'}")
    data = payload.get("data")
    if not isinstance(data, dict) or not isinstance(data.get("deliveries"), list):
        raise ValueError("Snapshot thieu data.deliveries")
    seen_positions: set[str] = set()
    results: list[dict] = []
    for raw in data["deliveries"]:
        if not isinstance(raw, dict):
            continue
        campaign_id = raw.get("campaign_id")
        position = raw.get("resource_position")
        if not isinstance(campaign_id, str) or not campaign_id.strip():
            continue
        if position not in ("banner", "popup") or position in seen_positions:
            continue
        seen_positions.add(position)
        if position == "banner":
            banner = raw.get("banner") or {}
            title = text_content((banner.get("success_popup") or {}).get("title"))
            buttons = [text_content(b.get("text")) for b in banner.get("buttons", []) if isinstance(b, dict)]
            detail = " / ".join(b for b in buttons if b) or "(banner, khong co nut doc duoc)"
        else:
            popup = raw.get("popup") or {}
            title = text_content(popup.get("title"))
            desc = text_content(popup.get("description"))
            buttons = [text_content(b.get("text")) for b in popup.get("buttons", []) if isinstance(b, dict)]
            detail = " - ".join(part for part in [desc, " / ".join(b for b in buttons if b)] if part)
        results.append(
            {
                "key": f"{position}:{campaign_id}",
                "campaign_id": campaign_id,
                "position": position,
                "priority": raw.get("priority"),
                "title": title,
                "detail": detail,
            }
        )
    return results


def find_device_mid() -> str | None:
    candidates: list[Path] = []
    base = os.environ.get("ZCODE_DATA_BASE_DIR", "").strip()
    if base:
        candidates.append(Path(base) / ".zcode" / "v2" / "telemetry-state.json")
    candidates.append(Path.home() / ".zcode" / "v2" / "telemetry-state.json")
    extra = os.environ.get("ZCODE_DEVICE_MID_FILE", "").strip()
    if extra:
        candidates.insert(0, Path(extra))
    for path in candidates:
        try:
            raw = json.loads(path.read_text(encoding="utf-8"))
        except Exception:
            continue
        value = raw.get("deviceMid") if isinstance(raw, dict) else None
        if isinstance(value, str) and value.strip():
            return value.strip()
    return None


def load_seen() -> set[str]:
    try:
        data = json.loads(SEEN_FILE.read_text(encoding="utf-8"))
        return set(data) if isinstance(data, list) else set()
    except Exception:
        return set()


def save_seen(seen: set[str]) -> None:
    try:
        STATE_DIR.mkdir(parents=True, exist_ok=True)
        SEEN_FILE.write_text(json.dumps(sorted(seen), ensure_ascii=False, indent=2), encoding="utf-8")
    except Exception as exc:
        print(f"[checker] khong ghi duoc seen file: {exc}", file=sys.stderr)


def query_deals(token: str | None, device_mid: str, locale: str, app_version: str, seq: int) -> list[dict]:
    url = f"{API_URL}?seq={seq}"
    headers = {
        "X-Device-Mid": device_mid,
        "X-Client-Language": locale,
        "X-ZCode-App-Version": app_version,
    }
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(url, headers=headers, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"HTTP {exc.code} tu marketing/touch (khong in token)") from exc
    except Exception as exc:
        raise RuntimeError(f"Khong goi duoc marketing/touch: {exc}") from exc
    return parse_snapshot(payload)


def find_zcode_executable() -> str | None:
    import shutil

    found = shutil.which("zcode") or shutil.which("ZCode")
    if found:
        return found
    candidates: list[str] = []
    if sys.platform == "win32":
        local = os.environ.get("LOCALAPPDATA", "")
        if local:
            candidates.append(os.path.join(local, "Programs", "ZCode", "ZCode.exe"))
    elif sys.platform == "darwin":
        candidates.append("/Applications/ZCode.app/Contents/MacOS/ZCode")
    else:
        candidates.extend(["/usr/bin/zcode", "/usr/local/bin/zcode"])
    for path in candidates:
        if path and os.path.isfile(path):
            return path
    return None


def show_popup(deals: list[dict]) -> None:
    import tkinter as tk

    lines = ["ZCode co deal moi! Mo app va bam nut nhan trong app.\n"]
    for deal in deals:
        lines.append(f"- [{deal['position']}] {deal['campaign_id']} (priority {deal['priority']})")
        if deal["title"]:
            lines.append(f"  {deal['title']}")
        if deal["detail"]:
            lines.append(f"  {deal['detail']}")
    lines.append("\nTool nay chi bao, khong tu nhan thay ban.")
    text = "\n".join(lines)

    root = tk.Tk()
    root.title("ZCode - Co deal moi!")
    root.attributes("-topmost", True)
    root.resizable(False, False)
    tk.Label(root, text=text, justify="left", padx=20, pady=16, font=("Segoe UI", 11)).pack()
    status = tk.Label(root, text="", fg="gray", padx=20, pady=4)
    status.pack()

    def do_open() -> None:
        exe = find_zcode_executable()
        if not exe:
            status.config(text="Khong tim thay ZCode tren may nay, ban mo app thu cong nhe.")
            return
        try:
            if sys.platform == "darwin" and exe.endswith(".app/Contents/MacOS/ZCode"):
                import subprocess

                subprocess.Popen(["open", "/Applications/ZCode.app"])
            elif sys.platform == "win32":
                os.startfile(exe)  # type: ignore[attr-defined]
            else:
                import subprocess

                subprocess.Popen([exe])
            status.config(text=f"Da goi mo ZCode: {exe}")
        except Exception as exc:
            status.config(text=f"Khong mo duoc tu dong ({exc}), ban mo thu cong nhe.")

    buttons = tk.Frame(root, padx=20, pady=12)
    buttons.pack()
    tk.Button(buttons, text="Mo ZCode", command=do_open, width=14).pack(side="left", padx=4)
    tk.Button(buttons, text="Da thay", command=root.destroy, width=14).pack(side="left", padx=4)
    root.update_idletasks()
    width, height = root.winfo_width(), root.winfo_height()
    root.geometry(f"+{max((root.winfo_screenwidth() - width) // 2, 0)}+{max((root.winfo_screenheight() - height) // 3, 0)}")
    try:
        root.lift()
        root.focus_force()
    except Exception:
        pass
    root.mainloop()


def main() -> int:
    parser = argparse.ArgumentParser(description="ZCode deal checker (chi doc API, khong claim).")
    parser.add_argument("--once", action="store_true", help="Check 1 lan roi thoat")
    parser.add_argument("--interval", type=int, default=DEFAULT_INTERVAL, help="Giay giua cac lan check (mac dinh 600, dung dat duoi 300)")
    parser.add_argument("--locale", default=os.environ.get("ZCODE_LOCALE", DEFAULT_LOCALE), choices=["zh-CN", "en-US"])
    parser.add_argument("--app-version", default=os.environ.get("ZCODE_APP_VERSION", "3.17.2"), help="X-ZCode-App-Version, nen de dung version app ban dang dung")
    parser.add_argument("--device-mid", default=os.environ.get("ZCODE_DEVICE_MID", ""), help="Ghi de deviceMid (mac dinh doc tu telemetry-state.json)")
    parser.add_argument("--self-test", action="store_true", help="Test parse snapshot mau, khong goi API")
    parser.add_argument("--dry-run", action="store_true", help="Chi in cau hinh, khong goi API")
    parser.add_argument("--prompt-token", action="store_true", help="Hoi token bang getpass thay vi doc env")
    args = parser.parse_args()

    if args.interval < 300:
        print("[checker] Canh bao: interval duoi 300s de bi 429. Tool van chay theo so ban dat.", file=sys.stderr)

    if args.self_test:
        deals = parse_snapshot(SAMPLE_SNAPSHOT)
        print(f"[checker] self-test OK: {len(deals)} delivery (banner 1 + popup 1, ban trung vi tri da bi loai):")
        for deal in deals:
            print(f"  - {deal['key']} | {deal['title']} | {deal['detail']}")
        return 0

    device_mid = (args.device_mid or "").strip() or find_device_mid()
    if args.dry_run:
        print(f"[checker] API: {API_URL}")
        print(f"[checker] deviceMid: {'tim thay (' + device_mid[:8] + '...)' if device_mid else 'KHONG TIM THAY - can ZCODE_DEVICE_MID hoac file telemetry-state.json'}")
        print(f"[checker] locale: {args.locale}, app-version: {args.app_version}, interval: {args.interval}s")
        print(f"[checker] token: {'co trong env' if os.environ.get('ZCODE_JWT_TOKEN') else 'khong co (se check an danh neu khong dung --prompt-token)'}")
        print("[checker] Che do: chi GET marketing/touch, khong POST action, khong claim.")
        return 0

    if not device_mid:
        print("[checker] LOI: khong tim thay deviceMid. App goc bat buoc header nay (UUID).", file=sys.stderr)
        print("[checker] Cach khac phuc: mo ZCode 1 lan de no tao ~/.zcode/v2/telemetry-state.json, hoac dat ZCODE_DEVICE_MID.", file=sys.stderr)
        return 2

    token = (os.environ.get("ZCODE_JWT_TOKEN") or "").strip() or None
    if args.prompt_token and not token:
        token = getpass.getpass("Nhap ZCode JWT (chi nam trong RAM phien nay, khong ghi file): ").strip() or None
    if not token:
        print("[checker] Luu y: khong co token, dang check an danh. Deal gan tai khoan co the khong hien.", file=sys.stderr)

    seq = 0
    failures = 0
    seen = load_seen()
    while True:
        try:
            deals = query_deals(token, device_mid, args.locale, args.app_version, seq)
            seq += 1
            failures = 0
        except Exception as exc:
            failures += 1
            delay = min(600, 30 * (2 ** min(failures - 1, 5))) if failures else args.interval
            print(f"[checker] Check loi ({exc}). Doi {delay}s roi thu lai.", file=sys.stderr)
            if args.once:
                return 1
            time.sleep(delay)
            continue
        new_deals = [d for d in deals if d["key"] not in seen]
        print(f"[checker] Co {len(deals)} delivery, {len(new_deals)} moi: {[d['key'] for d in deals]}")
        if new_deals:
            seen.update(d["key"] for d in new_deals)
            save_seen(seen)
            show_popup(new_deals)
        if args.once:
            return 0
        time.sleep(args.interval)


if __name__ == "__main__":
    raise SystemExit(main())
