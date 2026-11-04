import React, { useCallback, useEffect, useState } from "react";

interface TrayStatus {
  running: boolean;
  lastCheck: string | null;
  lastResult: string;
  dealsSeen: number;
  intervalSeconds: number;
  logTail: string[];
}

declare global {
  interface Window {
    trayApi: {
      getStatus: () => Promise<TrayStatus>;
      checkNow: () => Promise<string>;
      openZcode: () => Promise<string>;
      openLog: () => Promise<string>;
      quit: () => Promise<void>;
      onStatus: (cb: (status: TrayStatus) => void) => () => void;
    };
  }
}

export function App(): React.ReactElement {
  const [status, setStatus] = useState<TrayStatus | null>(null);
  const [message, setMessage] = useState<string>("");

  const refresh = useCallback(() => {
    window.trayApi
      .getStatus()
      .then(setStatus)
      .catch((error: unknown) => setMessage(`Khong lay duoc trang thai: ${String(error)}`));
  }, []);

  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, 5000);
    const off = window.trayApi.onStatus(setStatus);
    return () => {
      clearInterval(timer);
      off();
    };
  }, [refresh]);

  const run = (fn: () => Promise<string | void>, label: string): void => {
    fn()
      .then((result) => {
        if (typeof result === "string") setMessage(result);
        refresh();
      })
      .catch((error: unknown) => setMessage(`${label} loi: ${String(error)}`));
  };

  return (
    <main>
      <h1>ZCode Deal Watcher</h1>
      <p className="note">
        App chi bao deal, khong tu nhan thay ban. Binh thuong app chi song o khay he thong, cua so nay chi mo khi
        ban chon tu menu tray.
      </p>
      <section className="cards">
        <div className="card">
          <span className="label">Trang thai</span>
          <strong>{status?.running ? "Dang chay" : "Chua ro"}</strong>
        </div>
        <div className="card">
          <span className="label">Check cuoi</span>
          <strong>{status?.lastCheck ? new Date(status.lastCheck).toLocaleString("vi-VN") : "Chua check"}</strong>
        </div>
        <div className="card">
          <span className="label">Deal da thay</span>
          <strong>{status?.dealsSeen ?? 0}</strong>
        </div>
        <div className="card">
          <span className="label">Chu ky check</span>
          <strong>{status?.intervalSeconds ?? 600} giay</strong>
        </div>
      </section>
      <p className="result">{status?.lastResult ?? ""}</p>
      {message ? <p className="message">{message}</p> : null}
      <section className="actions">
        <button type="button" onClick={() => run(() => window.trayApi.checkNow(), "Kiem tra ngay")}>
          Kiem tra ngay
        </button>
        <button type="button" onClick={() => run(() => window.trayApi.openZcode(), "Mo ZCode")}>
          Mo ZCode
        </button>
        <button type="button" onClick={() => run(() => window.trayApi.openLog(), "Mo log")}>
          Mo log
        </button>
        <button type="button" className="danger" onClick={() => void window.trayApi.quit()}>
          Thoat app
        </button>
      </section>
      <section>
        <h2>Log cuoi</h2>
        <pre>{(status?.logTail ?? []).join("\n")}</pre>
      </section>
    </main>
  );
}
