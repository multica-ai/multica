type LogLevel = "debug" | "info" | "warn" | "error";

const COLORS: Record<LogLevel, string> = {
  debug: "color:#888",
  info: "color:#2196F3",
  warn: "color:#FF9800",
  error: "color:#F44336;font-weight:bold",
};

const CONSOLE_METHOD: Record<LogLevel, "log" | "info" | "warn" | "error"> = {
  debug: "log",
  info: "info",
  warn: "warn",
  error: "error",
};

const LEVEL_PRIORITY: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

/** Storage key for toggling frontend logging at runtime. */
const LOG_LEVEL_KEY = "multica_log_level";

const DEFAULT_LEVEL: LogLevel = "info";

type StoredLevel = LogLevel | "off";

const VALID_LEVELS: StoredLevel[] = ["debug", "info", "warn", "error", "off"];

function isValidLevel(level: string | null): level is StoredLevel {
  return level !== null && (VALID_LEVELS as string[]).includes(level);
}

function readStoredLevel(): StoredLevel {
  if (typeof window === "undefined") return DEFAULT_LEVEL;
  try {
    const raw = window.localStorage.getItem(LOG_LEVEL_KEY);
    return isValidLevel(raw) ? raw : DEFAULT_LEVEL;
  } catch {
    return DEFAULT_LEVEL;
  }
}

function shouldLog(level: LogLevel): boolean {
  const stored = readStoredLevel();
  if (stored === "off") return false;
  return LEVEL_PRIORITY[level] >= LEVEL_PRIORITY[stored];
}

export interface Logger {
  debug(msg: string, ...data: unknown[]): void;
  info(msg: string, ...data: unknown[]): void;
  warn(msg: string, ...data: unknown[]): void;
  error(msg: string, ...data: unknown[]): void;
}

export function createLogger(namespace: string): Logger {
  const make =
    (level: LogLevel) =>
    (msg: string, ...data: unknown[]) => {
      if (!shouldLog(level)) return;

      const ts = new Date().toISOString().slice(11, 23);
      const prefix = `%c${ts} [${namespace}]`;
      if (data.length > 0) {
        console[CONSOLE_METHOD[level]](prefix, COLORS[level], msg, ...data);
      } else {
        console[CONSOLE_METHOD[level]](prefix, COLORS[level], msg);
      }
    };

  return {
    debug: make("debug"),
    info: make("info"),
    warn: make("warn"),
    error: make("error"),
  };
}

/** No-op logger for when logging is not needed. */
export const noopLogger: Logger = {
  debug() {},
  info() {},
  warn() {},
  error() {},
};

/** Set the runtime log level. Persists in localStorage and takes effect immediately. */
export function setLogLevel(level: StoredLevel): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(LOG_LEVEL_KEY, level);
  } catch {
    // Ignore storage errors (e.g. private mode).
  }
}

/** Get the current runtime log level. */
export function getLogLevel(): StoredLevel {
  return readStoredLevel();
}
