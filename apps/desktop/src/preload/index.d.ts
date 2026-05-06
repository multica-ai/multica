import { ElectronAPI } from "@electron-toolkit/preload";
import type { RuntimeConfigResult } from "../shared/runtime-config";

interface DesktopAPI {
  /** App version + normalized OS, captured synchronously at preload time. */
  appInfo: {
    version: string;
    os: "macos" | "windows" | "linux" | "unknown";
  };
  /** Validated runtime endpoint config, or a blocking config error. */
  runtimeConfig: RuntimeConfigResult;
  /** Listen for auth token delivered via deep link. Returns an unsubscribe function. */
  onAuthToken: (callback: (token: string) => void) => () => void;
  /** Listen for invitation IDs delivered via deep link. Returns an unsubscribe function. */
  onInviteOpen: (callback: (invitationId: string) => void) => () => void;
  /** Open a URL in the default browser. */
  openExternal: (url: string) => Promise<void>;
  /** Hide macOS traffic lights for full-screen modals; restore when false. */
  setImmersiveMode: (immersive: boolean) => Promise<void>;
  /** Show a native OS notification for a new inbox item. */
  showNotification: (payload: {
    slug: string;
    itemId: string;
    issueKey: string;
    title: string;
    body: string;
  }) => void;
  /** Update the OS dock / taskbar unread badge. Pass 0 to clear. */
  setUnreadBadge: (count: number) => void;
  /** Listen for "open inbox row" requests from notification clicks. Returns an unsubscribe function. */
  onInboxOpen: (
    callback: (payload: {
      slug: string;
      itemId: string;
      issueKey: string;
    }) => void,
  ) => () => void;
}

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

interface DaemonPrefs {
  autoStart: boolean;
  autoStop: boolean;
}

interface DaemonAPI {
  start: () => Promise<{ success: boolean; error?: string }>;
  stop: () => Promise<{ success: boolean; error?: string }>;
  restart: () => Promise<{ success: boolean; error?: string }>;
  getStatus: () => Promise<DaemonStatus>;
  onStatusChange: (callback: (status: DaemonStatus) => void) => () => void;
  setTargetApiUrl: (url: string) => Promise<void>;
  syncToken: (token: string, userId: string) => Promise<void>;
  clearToken: () => Promise<void>;
  isCliInstalled: () => Promise<boolean>;
  getPrefs: () => Promise<DaemonPrefs>;
  setPrefs: (prefs: Partial<DaemonPrefs>) => Promise<DaemonPrefs>;
  autoStart: () => Promise<void>;
  retryInstall: () => Promise<void>;
  startLogStream: () => void;
  stopLogStream: () => void;
  onLogLine: (callback: (line: string) => void) => () => void;
  openLogFile: () => Promise<{ success: boolean; error?: string }>;
}

interface UpdaterAPI {
  onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => () => void;
  onDownloadProgress: (callback: (progress: { percent: number }) => void) => () => void;
  onUpdateDownloaded: (callback: () => void) => () => void;
  downloadUpdate: () => Promise<void>;
  installUpdate: () => Promise<void>;
  checkForUpdates: () => Promise<
    | { ok: true; currentVersion: string; latestVersion: string; available: boolean }
    | { ok: false; error: string }
  >;
}

declare global {
  interface Window {
    electron: ElectronAPI;
    desktopAPI: DesktopAPI;
    daemonAPI: DaemonAPI;
    updater: UpdaterAPI;
  }
}

export {};
