import { contextBridge, ipcRenderer } from "electron";
import { electronAPI } from "@electron-toolkit/preload";

const desktopAPI = {
  /** Listen for auth token delivered via deep link */
  onAuthToken: (callback: (token: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, token: string) =>
      callback(token);
    ipcRenderer.on("auth:token", handler);
    return () => {
      ipcRenderer.removeListener("auth:token", handler);
    };
  },
  /** Open a URL in the default browser */
  openExternal: (url: string) => ipcRenderer.invoke("shell:openExternal", url),
  /** Toggle immersive mode — hide macOS traffic lights for full-screen modals */
  setImmersiveMode: (immersive: boolean) =>
    ipcRenderer.invoke("window:setImmersive", immersive),
};

interface DaemonStatus {
  state: "running" | "stopped" | "starting" | "stopping" | "installing_cli" | "cli_not_found";
  pid?: number;
  uptime?: string;
  daemonId?: string;
  deviceName?: string;
  agents?: string[];
  workspaceCount?: number;
  profile?: string;
  serverUrl?: string;
}

const daemonAPI = {
  start: (): Promise<{ success: boolean; error?: string }> =>
    ipcRenderer.invoke("daemon:start"),
  stop: (): Promise<{ success: boolean; error?: string }> =>
    ipcRenderer.invoke("daemon:stop"),
  restart: (): Promise<{ success: boolean; error?: string }> =>
    ipcRenderer.invoke("daemon:restart"),
  getStatus: (): Promise<DaemonStatus> =>
    ipcRenderer.invoke("daemon:get-status"),
  onStatusChange: (callback: (status: DaemonStatus) => void) => {
    const handler = (_: unknown, status: DaemonStatus) => callback(status);
    ipcRenderer.on("daemon:status", handler);
    return () => ipcRenderer.removeListener("daemon:status", handler);
  },
  setTargetApiUrl: (url: string): Promise<void> =>
    ipcRenderer.invoke("daemon:set-target-api-url", url),
  syncToken: (token: string, userId: string): Promise<void> =>
    ipcRenderer.invoke("daemon:sync-token", token, userId),
  clearToken: (): Promise<void> =>
    ipcRenderer.invoke("daemon:clear-token"),
  isCliInstalled: (): Promise<boolean> =>
    ipcRenderer.invoke("daemon:is-cli-installed"),
  getPrefs: (): Promise<{ autoStart: boolean; autoStop: boolean }> =>
    ipcRenderer.invoke("daemon:get-prefs"),
  setPrefs: (prefs: Partial<{ autoStart: boolean; autoStop: boolean }>): Promise<{ autoStart: boolean; autoStop: boolean }> =>
    ipcRenderer.invoke("daemon:set-prefs", prefs),
  autoStart: (): Promise<void> =>
    ipcRenderer.invoke("daemon:auto-start"),
  retryInstall: (): Promise<void> =>
    ipcRenderer.invoke("daemon:retry-install"),
  startLogStream: () => ipcRenderer.send("daemon:start-log-stream"),
  stopLogStream: () => ipcRenderer.send("daemon:stop-log-stream"),
  onLogLine: (callback: (line: string) => void) => {
    const handler = (_: unknown, line: string) => callback(line);
    ipcRenderer.on("daemon:log-line", handler);
    return () => ipcRenderer.removeListener("daemon:log-line", handler);
  },
};

const updaterAPI = {
  onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => {
    const handler = (_: unknown, info: { version: string; releaseNotes?: string }) => callback(info);
    ipcRenderer.on("updater:update-available", handler);
    return () => ipcRenderer.removeListener("updater:update-available", handler);
  },
  onDownloadProgress: (callback: (progress: { percent: number }) => void) => {
    const handler = (_: unknown, progress: { percent: number }) => callback(progress);
    ipcRenderer.on("updater:download-progress", handler);
    return () => ipcRenderer.removeListener("updater:download-progress", handler);
  },
  onUpdateDownloaded: (callback: () => void) => {
    const handler = () => callback();
    ipcRenderer.on("updater:update-downloaded", handler);
    return () => ipcRenderer.removeListener("updater:update-downloaded", handler);
  },
  downloadUpdate: () => ipcRenderer.invoke("updater:download"),
  installUpdate: () => ipcRenderer.invoke("updater:install"),
};

if (process.contextIsolated) {
  contextBridge.exposeInMainWorld("electron", electronAPI);
  contextBridge.exposeInMainWorld("desktopAPI", desktopAPI);
  contextBridge.exposeInMainWorld("daemonAPI", daemonAPI);
  contextBridge.exposeInMainWorld("updater", updaterAPI);
} else {
  // @ts-expect-error - fallback for non-isolated context
  window.electron = electronAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.desktopAPI = desktopAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.daemonAPI = daemonAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.updater = updaterAPI;
}
