import type {
  LocaleAdapter,
  LocaleResources,
  SupportedLocale,
} from "../i18n";
import type { StorageAdapter } from "../types/storage";

/** Identifies the calling client to the server. Threaded through to
 *  ApiClient and WSClient so all HTTP requests and WS connections from
 *  this app instance are tagged with platform / version / os. */
export interface ClientIdentity {
  /** Logical client kind: "web" | "desktop" | "cli" | "daemon". */
  platform?: string;
  /** Client/app version string (e.g. "0.1.0"). */
  version?: string;
  /** Operating system: "macos" | "windows" | "linux". */
  os?: string;
}

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
  /** Identifies the calling client (web/desktop + version + os) to the server. */
  identity?: ClientIdentity;
  /** Official release baseline of the loaded frontend bundle, as a raw
   *  build-time tag (e.g. NEXT_PUBLIC_APP_VERSION). The config store validates
   *  it via officialBaseline, so pass the raw value — dev/non-tag input
   *  becomes "" (unavailable) rather than a false baseline. */
  frontendBaseline?: string;
  /** Active locale, determined server-side (web) or at app boot (desktop). */
  locale: SupportedLocale;
  /** i18next resources, server-preloaded for the active locale. */
  resources: Record<string, LocaleResources>;
  /** Locale adapter for persisting user choice (used by Settings switcher).
   *  Optional because some shells (e.g. CLI auth pages) don't need switching. */
  localeAdapter?: LocaleAdapter;
}
