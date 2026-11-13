// Bringing a desktop app environment's window forward, through Desktop's
// main process and the local daemon that detected the app. Web has no bridge.

export type FocusEnvironmentResult =
  | { ok: true }
  | {
      ok: false;
      /** `gone`: the app exited. `unsupported`: the daemon cannot raise
       *  windows on its platform, or is too old to try. `unavailable`: no
       *  local daemon answered. */
      reason: "gone" | "unsupported" | "unavailable" | "failed";
      error?: string;
    };

interface DaemonFocusAPI {
  focusEnvironment?: (key: string) => Promise<FocusEnvironmentResult>;
}

function readDaemonAPI(): DaemonFocusAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { daemonAPI?: DaemonFocusAPI }).daemonAPI;
}

/** True in a Desktop build that can ask its daemon to raise a window. */
export function canFocusLocalEnvironments(): boolean {
  return typeof readDaemonAPI()?.focusEnvironment === "function";
}

export async function focusLocalEnvironment(key: string): Promise<FocusEnvironmentResult> {
  const api = readDaemonAPI();
  if (!api?.focusEnvironment) return { ok: false, reason: "unsupported" };
  try {
    return await api.focusEnvironment(key);
  } catch (error) {
    return { ok: false, reason: "failed", error: error instanceof Error ? error.message : String(error) };
  }
}
