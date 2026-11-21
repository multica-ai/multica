import type { UpdaterFailureReason } from "../shared/updater-types";

export function classifyUpdaterError(error: unknown): UpdaterFailureReason {
  const details = typeof error === "object" && error !== null
    ? error as { code?: unknown; message?: unknown; statusCode?: unknown }
    : {};
  const code = typeof details.code === "string" ? details.code : "";
  const message = typeof details.message === "string" ? details.message : "";
  if (code === "ERR_UPDATER_CHANNEL_FILE_NOT_FOUND"
    || code === "ERR_UPDATER_LATEST_VERSION_NOT_FOUND"
    || [404, 502, 503, 504].includes(Number(details.statusCode))) {
    return "unavailable";
  }
  // electron-updater sometimes wraps the network error without keeping code.
  if (/\b(ENOTFOUND|EAI_AGAIN|ETIMEDOUT|ECONNRESET|ECONNREFUSED|ENETUNREACH|EHOSTUNREACH|ERR_INTERNET_DISCONNECTED|ERR_NAME_NOT_RESOLVED|ERR_CONNECTION_RESET|ERR_CONNECTION_TIMED_OUT|ERR_NETWORK_CHANGED)\b/.test(`${code} ${message}`)) {
    return "network";
  }
  return "unknown";
}
