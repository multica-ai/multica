import { closeSync, constants, fstatSync, ftruncateSync, openSync, writeSync } from "node:fs";
import { join } from "node:path";

type UpdaterEvent =
  | "startup"
  | "automatic-check"
  | "update-downloaded"
  | "install-requested"
  | "install-on-quit"
  | "app-will-quit"
  | "install-quit-timeout"
  | "updater-error";

const MAX_LOG_BYTES = 64 * 1024;

export function createUpdaterDiagnostics(directory: string, version: string) {
  const path = join(directory, "updater.log");
  return (event: UpdaterEvent): void => {
    const line = `${new Date().toISOString()} ${version} ${event}\n`;
    // Write before requesting quit so the record survives immediate process exit.
    try {
      // Windows append-only handles cannot be truncated.
      const fd = openSync(path, constants.O_WRONLY | constants.O_CREAT, 0o600);
      try {
        let offset = fstatSync(fd).size;
        if (offset + Buffer.byteLength(line) > MAX_LOG_BYTES) {
          ftruncateSync(fd, 0);
          offset = 0;
        }
        writeSync(fd, line, offset);
      } finally {
        closeSync(fd);
      }
    } catch {
      // Diagnostics must not affect updating or quitting.
    }
  };
}
