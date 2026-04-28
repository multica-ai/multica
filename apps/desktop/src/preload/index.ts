import { contextBridge, ipcRenderer } from "electron";
import { electronAPI } from "@electron-toolkit/preload";

// Synchronously fetch app metadata from main at preload time so the renderer
// can pass it into CoreProvider during the initial render — the alternative
// (async ipc.invoke) would race the ApiClient construction in initCore and
// the first few HTTP requests would go out without X-Client-Version/OS.
function fetchAppInfo(): { version: string; os: "macos" | "windows" | "linux" | "unknown" } {
  try {
    const info = ipcRenderer.sendSync("app:get-info") as
      | { version: string; os: "macos" | "windows" | "linux" | "unknown" }
      | undefined;
    if (info && typeof info.version === "string" && typeof info.os === "string") return info;
  } catch {
    // fall through
  }
  // Fallback: derive OS from process.platform; version unknown.
  const p = process.platform;
  const os: "macos" | "windows" | "linux" | "unknown" =
    p === "darwin" ? "macos" : p === "win32" ? "windows" : p === "linux" ? "linux" : "unknown";
  return { version: "unknown", os };
}

const appInfo = fetchAppInfo();

const desktopAPI = {
  /** App version + normalized OS. Read once at preload time so the renderer
   *  can use it synchronously when initializing the API client. */
  appInfo,
  /** Listen for auth token delivered via deep link */
  onAuthToken: (callback: (token: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, token: string) =>
      callback(token);
    ipcRenderer.on("auth:token", handler);
    return () => {
      ipcRenderer.removeListener("auth:token", handler);
    };
  },
  /** Listen for invitation IDs delivered via deep link */
  onInviteOpen: (callback: (invitationId: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, invitationId: string) =>
      callback(invitationId);
    ipcRenderer.on("invite:open", handler);
    return () => {
      ipcRenderer.removeListener("invite:open", handler);
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
  checkForUpdates: (): Promise<
    | { ok: true; currentVersion: string; latestVersion: string; available: boolean }
    | { ok: false; error: string }
  > => ipcRenderer.invoke("updater:check"),
};

const quickAddAPI = {
  close: () => ipcRenderer.send("quick-add:close"),
  setSize: (width: number, height: number) =>
    ipcRenderer.send("quick-add:set-size", width, height),
};

const workspaceAPI = {
  onSendCurrentSlug: (callback: () => void) => {
    const handler = () => callback();
    ipcRenderer.on("workspace:sendCurrentSlug", handler);
    return () => ipcRenderer.removeListener("workspace:sendCurrentSlug", handler);
  },
  receiveCurrentSlug: (slug: string | null) => {
    ipcRenderer.send("workspace:receiveCurrentSlug", slug);
  },
};

if (process.contextIsolated) {
  contextBridge.exposeInMainWorld("electron", electronAPI);
  contextBridge.exposeInMainWorld("desktopAPI", desktopAPI);
  contextBridge.exposeInMainWorld("daemonAPI", daemonAPI);
  contextBridge.exposeInMainWorld("updater", updaterAPI);
  contextBridge.exposeInMainWorld("quickAddAPI", quickAddAPI);
  contextBridge.exposeInMainWorld("workspaceAPI", workspaceAPI);
} else {
  // @ts-expect-error - fallback for non-isolated context
  window.electron = electronAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.desktopAPI = desktopAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.daemonAPI = daemonAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.updater = updaterAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.quickAddAPI = quickAddAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.workspaceAPI = workspaceAPI;
}
