import { ElectronAPI } from "@electron-toolkit/preload";

interface DesktopAPI {
  /** Listen for auth token delivered via deep link. Returns an unsubscribe function. */
  onAuthToken: (callback: (token: string) => void) => () => void;
  /** Open a URL in the default browser. */
  openExternal: (url: string) => Promise<void>;
}

interface DaemonStatus {
  state: "running" | "stopped" | "starting" | "stopping" | "cli_not_found";
  pid?: number;
  uptime?: string;
  daemonId?: string;
  deviceName?: string;
  agents?: string[];
  workspaceCount?: number;
}

interface DaemonAPI {
  start: () => Promise<{ success: boolean; error?: string }>;
  stop: () => Promise<{ success: boolean; error?: string }>;
  restart: () => Promise<{ success: boolean; error?: string }>;
  getStatus: () => Promise<DaemonStatus>;
  onStatusChange: (callback: (status: DaemonStatus) => void) => () => void;
  syncToken: (token: string) => Promise<void>;
  clearToken: () => Promise<void>;
  isCliInstalled: () => Promise<boolean>;
  startLogStream: () => void;
  stopLogStream: () => void;
  onLogLine: (callback: (line: string) => void) => () => void;
}

interface UpdaterAPI {
  onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => () => void;
  onDownloadProgress: (callback: (progress: { percent: number }) => void) => () => void;
  onUpdateDownloaded: (callback: () => void) => () => void;
  downloadUpdate: () => Promise<void>;
  installUpdate: () => Promise<void>;
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
