/**
 * Frontend mirror of the server's MinQuickCreateCLIVersion gate. The
 * agent-create flow (Quick Create modal) requires the daemon's bundled
 * multica CLI to be at least this version — older daemons either
 * double-create issues on partial CLI failures, drop quick-create attachment
 * bindings, or mishandle pasted screenshot URLs (see PR #1851 / MUL-1496).
 *
 * Both the frontend pre-validation in the modal and the server's
 * `/api/issues/quick-create` handler enforce this; the server is the
 * authoritative trust boundary, the frontend just lets us tell the user
 * "your daemon needs an upgrade" before they hit submit.
 */
export const MIN_QUICK_CREATE_CLI_VERSION = "0.2.21";
export const MIN_QUICK_CREATE_FIELDS_CLI_VERSION = "0.4.3";

export type CliVersionState = "ok" | "too_old" | "missing";

export interface CliVersionCheck {
  state: CliVersionState;
  /** What the daemon reported, or empty if missing/unparsable. */
  current: string;
  /** The hard minimum we gate on. */
  min: string;
}

const SEMVER_RE = /v?(\d+)\.(\d+)\.(\d+)/;

// Matches the `git describe --tags --always --dirty` output for a build past
// the latest tag, e.g. `v0.2.15-235-gdaf0e935` or `v0.2.15-235-gdaf0e935-dirty`.
// Daemons built from source (Makefile `make build` / `make daemon`) report this
// shape; tagged releases are bare semver. Treating dev-described daemons as OK
// is what keeps `pnpm dev:desktop` + `make daemon` unblocked without weakening
// the gate for staging or production users running stale stable releases.
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
 * Check a daemon-reported CLI version string against the minimum. Returns
 * `"missing"` for empty/unparsable input (fail closed — same policy as the
 * server) and `"too_old"` for a parsable version below the threshold.
 * Dev-built daemons (git-describe shape) are always OK — the version string
 * itself is the shared signal, so frontend and server agree by construction.
 */
export function checkQuickCreateCliVersion(detected: string | undefined | null): CliVersionCheck {
  return checkCliVersion(detected, MIN_QUICK_CREATE_CLI_VERSION);
}

/** Capability gate for explicit quick-create priority and due-date fields. */
export function checkQuickCreateFieldsCliVersion(
  detected: string | undefined | null,
): CliVersionCheck {
  return checkCliVersion(detected, MIN_QUICK_CREATE_FIELDS_CLI_VERSION);
}

function checkCliVersion(
  detected: string | undefined | null,
  minimum: string,
): CliVersionCheck {
  const current = (detected ?? "").trim();
  if (DEV_DESCRIBE_RE.test(current)) {
    return { state: "ok", current, min: minimum };
  }
  const parsed = current ? parseSemver(current) : null;
  if (!parsed) {
    return { state: "missing", current, min: minimum };
  }
  const min = parseSemver(minimum)!;
  if (lessThan(parsed, min)) {
    return { state: "too_old", current, min: minimum };
  }
  return { state: "ok", current, min: minimum };
}

/** Pull `cli_version` off a runtime row's loosely-typed metadata bag. */
export function readRuntimeCliVersion(metadata: Record<string, unknown> | undefined): string {
  const v = metadata?.cli_version;
  return typeof v === "string" ? v : "";
}

/**
 * First release whose daemon renders the chat session's project context
 * (description + resources) into the run brief (PR #5765, ships in v0.4.10).
 * Older daemons still receive and honor the project's repos — the server
 * pre-extracts those into the generic `repos` claim field — but silently skip
 * the Project Context section, so the durable description never reaches the
 * agent. SOFT gate: selecting a project always works; the UI only warns.
 *
 * Frontend-only constant: there is no server preview endpoint computing this,
 * so there is no server twin to keep in lockstep with.
 */
export const MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION = "0.4.10";

/**
 * Whether a daemon-reported CLI version is new enough to inject a chat
 * session's project description into the run brief. Missing / unparsable /
 * below-minimum are `false`; dev-built daemons (git-describe shape) always pass.
 */
export function chatProjectContextSupported(detected: string | undefined | null): boolean {
  return meetsMinCliVersion(detected, MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION);
}

function meetsMinCliVersion(detected: string | undefined | null, minimum: string): boolean {
  const current = (detected ?? "").trim();
  if (!current) return false;
  if (DEV_DESCRIBE_RE.test(current)) return true;
  const parsed = parseSemver(current);
  if (!parsed) return false;
  return !lessThan(parsed, parseSemver(minimum)!);
}

/**
 * Capability a daemon advertises when it implements worktree mode for
 * local_directory resources. Mirrors `DaemonCapabilityLocalWorktreeV1` in
 * `server/pkg/protocol/messages.go`.
 */
export const LOCAL_WORKTREE_CAPABILITY = "local-worktree-v1";

/** Minimal runtime shape this module needs; keeps callers from importing types. */
type RuntimeCapabilityRow = {
  daemon_id?: string | null;
  last_seen_at?: string | null;
  metadata?: unknown;
};

/**
 * Whether the machine behind `daemonId` has ADVERTISED worktree support, judged
 * by its most recently seen runtime row.
 *
 * Deliberately a positive signal only, and deliberately not a gate. `false`
 * means "no runtime row here says yes" — which covers a daemon that genuinely
 * cannot, a row written before the server recorded capabilities at all, and a
 * server too old to record them in the first place. Those have different
 * remedies and the client cannot reliably tell them apart: it would have to
 * infer the backend's age from data the backend wrote, and a row's silence
 * outlives the upgrade that fixed it (#7113).
 *
 * So the client stopped trying. The server is asked instead — it knows its own
 * version by construction — at every write path (create project, add resource,
 * update resource) and again at claim time, which is the gate that actually
 * keeps agents out of the user's working copy (MUL-5707). This function only
 * decides whether to PRESELECT parallel mode, where a wrong guess costs a
 * radio button, not a blocked user or a misleading instruction.
 *
 * Newest-row, not any-row: deregistering a runtime only marks it offline and
 * its metadata survives, so a machine that once advertised the capability and
 * then downgraded would otherwise keep vouching for itself forever.
 */
export function runtimeAdvertisesLocalWorktree(
  runtimes: RuntimeCapabilityRow[],
  daemonId: string | null | undefined,
): boolean {
  if (!daemonId) return false;
  let newest: RuntimeCapabilityRow | undefined;
  for (const rt of runtimes) {
    if (rt.daemon_id !== daemonId) continue;
    if (!newest) {
      newest = rt;
      continue;
    }
    // A row that never reported sorts oldest, so a live row always wins.
    const candidateSeen = rt.last_seen_at ?? "";
    const currentSeen = newest.last_seen_at ?? "";
    if (candidateSeen > currentSeen) newest = rt;
  }
  const metadata = newest?.metadata;
  if (!metadata || typeof metadata !== "object") return false;
  const caps = (metadata as { capabilities?: unknown }).capabilities;
  return Array.isArray(caps) && caps.includes(LOCAL_WORKTREE_CAPABILITY);
}
