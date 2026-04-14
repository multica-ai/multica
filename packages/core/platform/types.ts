import type { StorageAdapter } from "../types/storage";

export interface CoreProviderProps {
  children: React.ReactNode;
  /** API base URL. Default: "" (same-origin). */
  apiBaseUrl?: string;
  /** WebSocket URL. Default: "ws://localhost:8080/ws". */
  wsUrl?: string;
  /** Storage adapter. Default: SSR-safe localStorage wrapper. */
  storage?: StorageAdapter;
  /** Use HttpOnly cookies for auth instead of localStorage tokens. Default: false. */
  cookieAuth?: boolean;
  /** Called after successful login (e.g. set cookie for Next.js middleware). */
  onLogin?: () => void;
  /** Called after logout (e.g. clear cookie). */
  onLogout?: () => void;
}
