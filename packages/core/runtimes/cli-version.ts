/**
 * Frontend mirror of the server's MinQuickCreateCLIVersion gate. The
 * agent-create flow (Quick Create modal) requires the daemon's bundled
 * multica CLI to be at least this version — older daemons either
 * double-create issues on partial CLI failures or mishandle pasted
 * screenshot URLs (see PR #1851 / MUL-1496).
 *
 * Both the frontend pre-validation in the modal and the server's
 * `/api/issues/quick-create` handler enforce this; the server is the
 * authoritative trust boundary, the frontend just lets us tell the user
 * "your daemon needs an upgrade" before they hit submit.
 */
export const MIN_QUICK_CREATE_CLI_VERSION = "0.2.20";

export type CliVersionState = "ok" | "too_old" | "missing";

export interface CliVersionCheck {
  state: CliVersionState;
  /** What the daemon reported, or empty if missing/unparsable. */
  current: string;
  /** The hard minimum we gate on. */
  min: string;
}

const SEMVER_RE = /v?(\d+)\.(\d+)\.(\d+)/;

function parseSemver(raw: string): [number, number, number] | null {
  const m = SEMVER_RE.exec(raw.trim());
  if (!m) return null;
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

function lessThan(a: [number, number, number], b: [number, number, number]) {
  if (a[0] !== b[0]) return a[0] < b[0];
  if (a[1] !== b[1]) return a[1] < b[1];
  return a[2] < b[2];
}

/**
 * Check a daemon-reported CLI version string against the minimum. Returns
 * `"missing"` for empty/unparsable input (fail closed — same policy as the
 * server) and `"too_old"` for a parsable version below the threshold.
 *
 * `"dev"` (from `go run` / untagged builds) is treated as always-ok: dev
 * builds are built from HEAD and are at least as new as any released version.
 */
export function checkQuickCreateCliVersion(detected: string | undefined | null): CliVersionCheck {
  const current = (detected ?? "").trim();
  // Dev builds (go run, untagged) report "dev" — always pass the gate.
  if (current === "dev") {
    return { state: "ok", current, min: MIN_QUICK_CREATE_CLI_VERSION };
  }
  const parsed = current ? parseSemver(current) : null;
  if (!parsed) {
    return { state: "missing", current, min: MIN_QUICK_CREATE_CLI_VERSION };
  }
  const min = parseSemver(MIN_QUICK_CREATE_CLI_VERSION)!;
  if (lessThan(parsed, min)) {
    return { state: "too_old", current, min: MIN_QUICK_CREATE_CLI_VERSION };
  }
  return { state: "ok", current, min: MIN_QUICK_CREATE_CLI_VERSION };
}

/** Pull `cli_version` off a runtime row's loosely-typed metadata bag. */
export function readRuntimeCliVersion(metadata: Record<string, unknown> | undefined): string {
  const v = metadata?.cli_version;
  return typeof v === "string" ? v : "";
}
