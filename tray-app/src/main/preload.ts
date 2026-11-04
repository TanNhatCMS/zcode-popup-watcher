import { contextBridge, ipcRenderer } from "electron";

export interface TrayStatus {
  running: boolean;
  lastCheck: string | null;
  lastResult: string;
  dealsSeen: number;
  intervalSeconds: number;
  logTail: string[];
}

contextBridge.exposeInMainWorld("trayApi", {
  getStatus: (): Promise<TrayStatus> => ipcRenderer.invoke("tray:get-status"),
  checkNow: (): Promise<string> => ipcRenderer.invoke("tray:check-now"),
  openZcode: (): Promise<string> => ipcRenderer.invoke("tray:open-zcode"),
  openLog: (): Promise<string> => ipcRenderer.invoke("tray:open-log"),
  quit: (): Promise<void> => ipcRenderer.invoke("tray:quit"),
  onStatus: (cb: (status: TrayStatus) => void): (() => void) => {
    const listener = (_event: unknown, status: TrayStatus): void => cb(status);
    ipcRenderer.on("tray:status", listener);
    return () => {
      ipcRenderer.removeListener("tray:status", listener);
    };
  },
});
