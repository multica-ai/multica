import type { StorageAdapter } from "../types/storage";
import type { Logger } from "../logger";

export interface CoreProviderProps {
  children: React.ReactNode;
  /** API base URL. Default: "" (same-origin). */
  apiBaseUrl?: string;
  /** Optional API logger. Defaults to createLogger("api"). */
  apiLogger?: Logger;
  /** WebSocket URL. Default: "ws://localhost:8080/ws". */
  wsUrl?: string;
  /** Storage adapter. Default: SSR-safe localStorage wrapper. */
  storage?: StorageAdapter;
  /** Use HttpOnly cookies for auth instead of localStorage tokens. Default: false. */
  cookieAuth?: boolean;
  /** Add token query param for native compatibility with older deployed WS servers. */
  queryTokenAuth?: boolean;
  /** Optional native WebSocket headers. */
  wsHeaders?: Record<string, string>;
  /** Called after successful login (e.g. set cookie for Next.js middleware). */
  onLogin?: () => void;
  /** Called after logout (e.g. clear cookie). */
  onLogout?: () => void;
}
