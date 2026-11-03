import { Platform } from "react-native";
import { requireOptionalNativeModule } from "expo";
import { LogCapture, type LogSink } from "./log-capture";

interface NativeLogs extends LogSink {
  clearLogs(): void;
  remoteLogReadAllowed(): boolean;
  setRemoteLogReadAllowed(allowed: boolean): void;
  setLogRuntime(runtime: string): void;
  exportLogs(sinceMinutes: number): Promise<string>;
  clearLogExports(): Promise<void>;
}

const native = Platform.OS === "ios" ? requireOptionalNativeModule<NativeLogs>("MulticaDeviceLink") : null;

/** The app's one log capture, writing to the native log shared with the link. */
export const logCapture = new LogCapture(native);

export function setLogRuntime(runtime: Record<string, unknown>) {
  try { native?.setLogRuntime(JSON.stringify(runtime)); } catch { /* Diagnostics must not change behavior. */ }
}

/** Clears the log for a sign-out the user chose, dropping what has not reached it. */
export function clearLogs() {
  logCapture.discard();
  try { native?.clearLogs(); } catch { /* A later sign-in as another account clears it. */ }
}

export function remoteLogReadAllowed() {
  try { return native?.remoteLogReadAllowed() ?? false; } catch { return false; }
}

export function setRemoteLogReadAllowed(allowed: boolean) {
  native?.setRemoteLogReadAllowed(allowed);
}

/** Writes the last sinceMinutes to a temporary file for the user to share. */
export async function exportLogsToFile(sinceMinutes: number): Promise<string> {
  if (!native) throw new Error("native_unavailable");
  logCapture.flush();
  const { path } = JSON.parse(await native.exportLogs(sinceMinutes)) as { path: string };
  return path;
}

export function clearLogExports() {
  void native?.clearLogExports().catch(() => {});
}
