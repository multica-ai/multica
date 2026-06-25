/**
 * Agent-mode issue creation no longer gates on the daemon-reported CLI version.
 * Keep the exported shape stable for callers/tests, but always report `ok`.
 */
export const MIN_QUICK_CREATE_CLI_VERSION = "0.2.21";

export type CliVersionState = "ok" | "too_old" | "missing";

export interface CliVersionCheck {
  state: CliVersionState;
  /** What the daemon reported, or empty if missing/unparsable. */
  current: string;
  /** Retained for compatibility with older callers/tests. */
  min: string;
}

/** Agent-mode creation ignores daemon CLI version strings and always permits submit. */
export function checkQuickCreateCliVersion(detected: string | undefined | null): CliVersionCheck {
  const current = (detected ?? "").trim();
  return { state: "ok", current, min: MIN_QUICK_CREATE_CLI_VERSION };
}

/** Pull `cli_version` off a runtime row's loosely-typed metadata bag. */
export function readRuntimeCliVersion(metadata: Record<string, unknown> | undefined): string {
  const v = metadata?.cli_version;
  return typeof v === "string" ? v : "";
}
