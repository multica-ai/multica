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
 * Frontend mirror of the server's `MinHandoffCLIVersion` soft gate
 * (`server/pkg/agent/version.go`). The assignment handoff note is only rendered
 * into the run's opening prompt by daemons at or above this multica CLI version
 * (MUL-3375); older daemons silently drop it. Unlike the quick-create gate this
 * never blocks the assignment — the UI just grays out the note box and warns.
 *
 * Keep in lockstep with the server constant; the two are enforced independently
 * (the server is authoritative) but must agree so the warning matches reality.
 */
export const MIN_HANDOFF_CLI_VERSION = "0.3.28";

/**
 * Whether a daemon-reported CLI version is new enough to render a handoff note.
 * Mirrors server `agent.HandoffSupported`: missing / unparsable / below-minimum
 * all degrade to `false`, and dev-built daemons (git-describe shape) always
 * pass — the version string is the shared signal, so frontend and server agree
 * by construction. Pure and synchronous, so the note box can settle from the
 * already-warm runtime cache instead of waiting on the trigger-preview
 * round-trip, exactly like the quick-create version gate.
 */
export function handoffSupported(detected: string | undefined | null): boolean {
  return meetsMinCliVersion(detected, MIN_HANDOFF_CLI_VERSION);
}

/**
 * First release whose daemon renders the chat session's project context
 * (description + resources) into the run brief (PR #5765, ships in v0.4.10).
 * Older daemons still receive and honor the project's repos — the server
 * pre-extracts those into the generic `repos` claim field — but silently skip
 * the Project Context section, so the durable description never reaches the
 * agent. SOFT gate: selecting a project always works; the UI only warns.
 *
 * Frontend-only constant: unlike handoff there is no server preview endpoint
 * computing this, so there is no server twin to keep in lockstep with.
 */
export const MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION = "0.4.10";

/**
 * Whether a daemon-reported CLI version is new enough to inject a chat
 * session's project description into the run brief. Same degrade rules as
 * `handoffSupported`: missing / unparsable / below-minimum are `false`,
 * dev-built daemons (git-describe shape) always pass.
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
 * Minimum daemon CLI version that implements worktree mode for
 * `local_directory` resources (`execution_mode: "worktree"`).
 *
 * Server twin: `MinLocalWorktreeCLIVersion` in `server/pkg/agent/version.go`.
 * Keep the two in lockstep — the server refuses to SAVE a worktree resource
 * below this floor, and this constant only exists so the UI can say so before
 * the user submits rather than surfacing a bare 422.
 *
 * HARD gate, unlike `handoffSupported`: a daemon below the floor does not know
 * the field exists, so it would run the task in place — editing the working
 * copy the user asked to isolate. Degrading to "just try it" is not an option.
 */
export const MIN_LOCAL_WORKTREE_CLI_VERSION = "0.4.24";

/**
 * Whether a daemon-reported CLI version can run worktree mode. Missing /
 * unparsable / below-minimum are `false`; dev-built daemons (git-describe
 * shape) pass, matching every other gate in this file.
 */
export function localWorktreeSupported(detected: string | undefined | null): boolean {
  return meetsMinCliVersion(detected, MIN_LOCAL_WORKTREE_CLI_VERSION);
}

/**
 * Capability a daemon advertises when it implements worktree mode for
 * local_directory resources. Mirrors `DaemonCapabilityLocalWorktreeV1` in
 * `server/pkg/protocol/messages.go`.
 */
export const LOCAL_WORKTREE_CAPABILITY = "local-worktree-v1";

/**
 * Whether a runtime advertised worktree support at registration.
 *
 * This replaces a version comparison on purpose. A daemon without the
 * implementation does not merely lose concurrency — it ignores `execution_mode`
 * and edits the user's working copy, which is what the mode exists to prevent.
 * Version strings could not answer that: a dev build reports a git-describe
 * string that `meetsMinCliVersion` deliberately exempts, so a daemon with no
 * worktree code at all passed the check (MUL-5707).
 *
 * Absent metadata (an older daemon that never sent the header) is `false` —
 * this must fail closed, and the server enforces the same thing at claim time.
 */
export function runtimeSupportsLocalWorktree(metadata: unknown): boolean {
  if (!metadata || typeof metadata !== "object") return false;
  const caps = (metadata as { capabilities?: unknown }).capabilities;
  return Array.isArray(caps) && caps.includes(LOCAL_WORKTREE_CAPABILITY);
}

/** Minimal runtime shape this module needs; keeps callers from importing types. */
type RuntimeCapabilityRow = {
  daemon_id?: string | null;
  last_seen_at?: string | null;
  metadata?: unknown;
};

/**
 * Whether the machine behind `daemonId` currently runs a daemon that supports
 * worktree mode, judged by its MOST RECENTLY SEEN runtime row.
 *
 * Deliberately not "any row advertised it". Deregistering a runtime only marks
 * the row offline — its metadata survives — so a machine that once ran a
 * capable daemon and then downgraded still has an old capable row beside the
 * fresh incapable one, and an any-match would answer yes forever. The server's
 * `daemonAdvertisesWorktree` uses the same newest-wins rule; the two must agree
 * or the UI offers a mode the API will refuse.
 */
export function daemonSupportsLocalWorktree(
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
  return newest ? runtimeSupportsLocalWorktree(newest.metadata) : false;
}
