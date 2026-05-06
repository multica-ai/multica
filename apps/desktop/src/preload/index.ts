import { contextBridge, ipcRenderer } from "electron";
import { electronAPI } from "@electron-toolkit/preload";

import type { LocalStackStatus } from "../shared/local-stack-types";
import type { ResetResult } from "../shared/local-reset-types";
import type { LocalDataPaths } from "../main/local-data-paths";
import type { LocalDiagnostics } from "../main/local-diagnostics";
import type {
  TaskChangeApplyResult,
  TaskChangeInput,
  TaskChangePreview,
} from "../shared/task-change-types";

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
  /**
   * Show a native OS notification for a new inbox item. Fired from the
   * renderer only when the app is unfocused — in-focus feedback is the
   * inbox sidebar's unread styling. `slug`, `itemId`, and `issueKey` are
   * all round-tripped on click: slug pins routing to the source workspace
   * (the user may switch workspaces before clicking the banner), itemId
   * lets the renderer mark the row read, issueKey maps to the inbox URL
   * param.
   */
  showNotification: (payload: {
    slug: string;
    itemId: string;
    issueKey: string;
    title: string;
    body: string;
  }) => ipcRenderer.send("notification:show", payload),
  /**
   * Update the OS dock / taskbar unread badge. Pass 0 to clear. Values
   * above 99 render as "99+" (capping is handled in the main process).
   */
  setUnreadBadge: (count: number) =>
    ipcRenderer.send("badge:set", Math.max(0, Math.floor(count))),
  /**
   * Subscribe to "open this inbox row" requests sent by the main process
   * when the user clicks an OS notification banner. Returns an unsubscribe
   * function. The payload echoes the `slug`, `itemId`, and `issueKey` that
   * were passed to `showNotification`.
   */
  onInboxOpen: (
    callback: (payload: {
      slug: string;
      itemId: string;
      issueKey: string;
    }) => void,
  ) => {
    const handler = (
      _event: Electron.IpcRendererEvent,
      payload: { slug: string; itemId: string; issueKey: string },
    ) => callback(payload);
    ipcRenderer.on("inbox:open", handler);
    return () => {
      ipcRenderer.removeListener("inbox:open", handler);
    };
  },
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
  openLogFile: (): Promise<{ success: boolean; error?: string }> =>
    ipcRenderer.invoke("daemon:open-log-file"),
};

const localStackAPI = {
  getStatus: (): Promise<LocalStackStatus> =>
    ipcRenderer.invoke("localStack:get-status"),
  retry: (): Promise<void> => ipcRenderer.invoke("localStack:retry"),
  openLogs: (): Promise<{ ok: boolean }> =>
    ipcRenderer.invoke("localStack:open-logs"),
  onStatusChange: (callback: (status: LocalStackStatus) => void) => {
    const handler = (_: unknown, status: LocalStackStatus) => callback(status);
    ipcRenderer.on("localStack:status", handler);
    return () => ipcRenderer.removeListener("localStack:status", handler);
  },
};

const localResetAPI = {
  /** Wipe app-owned local data (postgres cluster, daemon logs, app logs).
   *  Stops the local stack first; never touches user repo checkouts or
   *  Electron's persisted preferences. Best-effort — see ResetResult for
   *  per-target outcomes. */
  reset: (): Promise<ResetResult> => ipcRenderer.invoke("localReset:reset"),
};

const localDiagnosticsAPI = {
  /** Live diagnostics snapshot — re-collected on each call. */
  get: (): Promise<LocalDiagnostics> =>
    ipcRenderer.invoke("diagnostics:get"),
  /** Pre-formatted plain-text version, ready for the clipboard. */
  formatAsText: (): Promise<string> =>
    ipcRenderer.invoke("diagnostics:formatText"),
  /** Open one of the well-known local data folders in the OS file manager. */
  openPath: (
    key: keyof LocalDataPaths,
  ): Promise<{ ok: boolean; error?: string }> =>
    ipcRenderer.invoke("diagnostics:openPath", key),
};

// Narrow surface for the "apply task diff to my checkout" flow (Phase 8).
// Only these four methods exist — the renderer cannot ask main to read or
// write arbitrary files. `openPath` is bounded to paths the user chose via
// `pickCheckoutDirectory` or paths surfaced by the daemon as task metadata;
// the main process additionally enforces that the argument is absolute.
const taskChangeAPI = {
  pickCheckoutDirectory: (): Promise<{ ok: boolean; path?: string }> =>
    ipcRenderer.invoke("taskChange:pickCheckoutDirectory"),
  previewApplyTaskDiff: (input: TaskChangeInput): Promise<TaskChangePreview> =>
    ipcRenderer.invoke("taskChange:previewApplyTaskDiff", input),
  applyTaskDiff: (input: TaskChangeInput): Promise<TaskChangeApplyResult> =>
    ipcRenderer.invoke("taskChange:applyTaskDiff", input),
  openPath: (target: string): Promise<{ ok: boolean; error?: string }> =>
    ipcRenderer.invoke("taskChange:openPath", target),
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

if (process.contextIsolated) {
  contextBridge.exposeInMainWorld("electron", electronAPI);
  contextBridge.exposeInMainWorld("desktopAPI", desktopAPI);
  contextBridge.exposeInMainWorld("daemonAPI", daemonAPI);
  contextBridge.exposeInMainWorld("localStackAPI", localStackAPI);
  contextBridge.exposeInMainWorld("localDiagnosticsAPI", localDiagnosticsAPI);
  contextBridge.exposeInMainWorld("localResetAPI", localResetAPI);
  contextBridge.exposeInMainWorld("taskChangeAPI", taskChangeAPI);
  contextBridge.exposeInMainWorld("updater", updaterAPI);
} else {
  // @ts-expect-error - fallback for non-isolated context
  window.electron = electronAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.desktopAPI = desktopAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.daemonAPI = daemonAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.localStackAPI = localStackAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.localDiagnosticsAPI = localDiagnosticsAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.localResetAPI = localResetAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.taskChangeAPI = taskChangeAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.updater = updaterAPI;
}
