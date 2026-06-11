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

/** Storage key for toggling frontend logging at runtime. */
const LOG_LEVEL_KEY = "multica_log_level";

function isSilent(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(LOG_LEVEL_KEY) === "off";
  } catch {
    return false;
  }
}

export interface Logger {
  debug(msg: string, ...data: unknown[]): void;
  info(msg: string, ...data: unknown[]): void;
  warn(msg: string, ...data: unknown[]): void;
  error(msg: string, ...data: unknown[]): void;
}

export function createLogger(namespace: string): Logger {
  if (isSilent()) return noopLogger;

  const make =
    (level: LogLevel) =>
    (msg: string, ...data: unknown[]) => {
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
