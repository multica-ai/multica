import type { Logger } from "@multica/core/logger";

function logApi(level: "debug" | "info" | "warn" | "error", msg: string, data: unknown[]) {
  const ts = new Date().toISOString().slice(11, 23);
  const prefix = `${ts} [api]`;
  const method = level === "debug" ? "log" : level;

  console[method](prefix, msg, ...data);
}

export const apiLogger: Logger = {
  debug(msg, ...data) {
    logApi("debug", msg, data);
  },
  info(msg, ...data) {
    logApi("info", msg, data);
  },
  warn(msg, ...data) {
    logApi("warn", msg, data);
  },
  error(msg, ...data) {
    logApi("error", msg, data);
  },
};
