/**
 * Frontend mirror of a self-hosted version gate. The desktop app and the
 * self-hosted API server are versioned and released independently, so after a
 * desktop update the locally-run server can lag behind and silently lack the
 * endpoints the new app calls (e.g. the issue board's `/api/issues/table/*`
 * routes). This check compares the server-reported `server_version` against
 * the running app version so we can tell the user "upgrade your server"
 * instead of letting every board column fail with an opaque
 * "load more failed" error.
 *
 * The server is the authoritative trust boundary; this frontend pre-check
 * just produces a friendly banner before the user is buried in 404s.
 */

export type ServerVersionState = "ok" | "too_old" | "unknown";

export interface ServerVersionCheck {
  state: ServerVersionState;
  /** What the server reported, or empty if missing/unparsable. */
  current: string;
  /** The app version used as the compatibility baseline. */
  min: string;
}

const SEMVER_RE = /v?(\d+)\.(\d+)\.(\d+)/;

// Matches `git describe --tags --always --dirty` output for a build past the
// latest tag, e.g. `v0.2.15-235-gdaf0e935`. Dev/source-built servers report
// this shape; treating them as OK keeps local `make server` unblocked without
// weakening the gate for production self-hosters on stale stable releases.
const DEV_DESCRIBE_RE = /^v?\d+\.\d+\.\d+-\d+-g[0-9a-fA-F]+/;

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
 * Compare the server-reported version against the running app version.
 * Returns:
 *  - "unknown": the server didn't report a version (omitted by managed cloud,
 *    or a server older than this feature). We can't tell, so don't nag — the
 *    managed cloud is always current and old self-hosted servers predate this
 *    check entirely.
 *  - "too_old": the server version parses but is below the app version. This is
 *    the self-host "you updated the app but not the server" case — show the
 *    upgrade banner.
 *  - "ok": versions match or the server is newer.
 *
 * Dev/source-built servers (git-describe shape) are always OK, matching the
 * policy in cli-version.ts so frontend and server agree by construction.
 */
export function checkServerVersion(
  detected: string | undefined | null,
  appVersion: string,
): ServerVersionCheck {
  const current = (detected ?? "").trim();
  if (!current) return { state: "unknown", current: "", min: appVersion };
  if (DEV_DESCRIBE_RE.test(current)) return { state: "ok", current, min: appVersion };

  const parsed = parseSemver(current);
  if (!parsed) return { state: "unknown", current, min: appVersion };

  const app = parseSemver(appVersion);
  if (!app) return { state: "unknown", current, min: appVersion };

  if (lessThan(parsed, app)) return { state: "too_old", current, min: appVersion };
  return { state: "ok", current, min: appVersion };
}
