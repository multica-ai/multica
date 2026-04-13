import { ElectronAPI } from "@electron-toolkit/preload";

interface DesktopAPI {
  /** Listen for auth token delivered via deep link. Returns an unsubscribe function. */
  onAuthToken: (callback: (token: string) => void) => () => void;
  /** Open a URL in the default browser. */
  openExternal: (url: string) => Promise<void>;
}

declare global {
  interface Window {
    electron: ElectronAPI;
    desktopAPI: DesktopAPI;
  }
}

export {};
