import type {
  GitHubPullRequestChecksConclusion,
  GitHubPullRequestChecksRollup,
  GitHubPullRequestMergeable,
  GitHubPullRequestMergeStateStatus,
  GitHubPullRequestState,
} from "../types";

// A PR's health comes from TWO independent facts, each tri-state and each
// sourced from the GitHub API snapshot:
//
//   1. CI status      — derived from `checks_rollup` (primary) + counts.
//   2. Mergeability   — derived from `mergeable` + `merge_state_status`.
//
// The two are derived independently: a PR can have failing checks AND a merge
// conflict. The sidebar row folds them into one verdict (see
// `derivePullRequestVerdict` below) that still carries a conflict alongside a
// failure, so neither fact is lost.
//
// Every input field is optional because older backends omit the snapshot
// fields; each rule defaults defensively (`?? 0`, `?? []`, explicit `=== "..."`
// checks) so an absent field never fabricates a positive verdict.

// ---------------------------------------------------------------------------
// CI status
// ---------------------------------------------------------------------------

// Discriminated union for the CI element. `none` is a current snapshot with no
// checks; `unavailable` means there is no current snapshot region to render.
export type PullRequestChecksStatus =
  | { kind: "failed"; failed: number; total: number; names: string[] }
  | { kind: "pending"; passed: number; total: number; running: number }
  | { kind: "passed"; total: number }
  | { kind: "none" }
  | { kind: "unavailable" };

export interface PullRequestChecksInput {
  snapshot_available?: boolean;
  checks_rollup?: GitHubPullRequestChecksRollup | null;
  checks_conclusion?: GitHubPullRequestChecksConclusion | null;
  checks_total?: number;
  checks_passed?: number;
  checks_failed?: number;
  checks_running?: number;
  checks_pending?: number;
  failed_check_names?: string[];
}

// Priority (high → low):
//   0. explicitly unavailable snapshot             → unavailable
//   1. rollup failure/error OR any failed count → failed
//   2. rollup pending/expected                  → pending
//   3. rollup success                           → passed
//   4. legacy provider conclusion               → matching coarse state
//   5. current snapshot + null rollup            → none ("no checks yet")
//   6. otherwise                                 → unavailable
//
// Failure trusts the count as well as the rollup so a known legacy failure is
// surfaced even if its coarse conclusion lags. An explicit false availability
// gate always wins, so disabled or wrong-head GitHub data never leaks through.
export function deriveChecksStatus(input: PullRequestChecksInput): PullRequestChecksStatus {
  if (input.snapshot_available === false) {
    return { kind: "unavailable" };
  }
  const rollup = input.checks_rollup ?? null;
  const total = input.checks_total ?? 0;
  const passed = input.checks_passed ?? 0;
  const failed = input.checks_failed ?? 0;
  const running = input.checks_running ?? input.checks_pending ?? 0;
  const names = input.failed_check_names ?? [];

  if (rollup === "failure" || rollup === "error" || failed > 0) {
    return { kind: "failed", failed, total, names };
  }
  if (rollup === "pending" || rollup === "expected") {
    return { kind: "pending", passed, total, running };
  }
  if (rollup === "success") {
    return { kind: "passed", total };
  }
  // Forgejo / Gitea / GitLab and older GitHub backends expose the coarse
  // webhook-derived conclusion rather than a GraphQL rollup. Preserve those
  // known passed/pending/failed states without confusing an absent API
  // snapshot with a current "no checks" verdict.
  if (input.checks_conclusion === "failed") {
    return { kind: "failed", failed, total, names };
  }
  if (input.checks_conclusion === "pending") {
    return { kind: "pending", passed, total, running };
  }
  if (input.checks_conclusion === "passed") {
    return { kind: "passed", total };
  }
  return input.snapshot_available === true ? { kind: "none" } : { kind: "unavailable" };
}

// ---------------------------------------------------------------------------
// Mergeability
// ---------------------------------------------------------------------------

// Discriminated union for the mergeability element. `none` renders nothing:
// when GitHub has not decided (mergeable unknown/null and no decisive
// merge_state_status) the card asserts neither "conflict" nor "ready".
export type PullRequestMergeStatus =
  | { kind: "conflicting" }
  | { kind: "ready" }
  | { kind: "blocked" }
  | { kind: "behind" }
  | { kind: "unstable" }
  | { kind: "has_hooks" }
  | { kind: "none" };

export interface PullRequestMergeInput {
  snapshot_available?: boolean;
  mergeable?: GitHubPullRequestMergeable | null;
  merge_state_status?: GitHubPullRequestMergeStateStatus | null;
}

// Priority (high → low):
//   1. mergeable conflicting OR merge_state dirty → conflicting
//   2. merge_state clean                          → ready
//   3. merge_state blocked/behind/unstable/hooks  → that faithful label
//   4. otherwise                                  → none  (render nothing)
//
// `mergeable` answers only "is there a conflict"; `merge_state_status === dirty`
// is GitHub's other view of the same fact (an unmergeable conflict), so both
// map to `conflicting`. "Ready" is asserted ONLY from `clean` — never inferred
// from `mergeable === "mergeable"`, which does not account for required checks
// or branch protection.
export function deriveMergeStatus(input: PullRequestMergeInput): PullRequestMergeStatus {
  if (input.snapshot_available === false) return { kind: "none" };
  const mergeable = input.mergeable ?? null;
  const mergeState = input.merge_state_status ?? null;

  if (mergeable === "conflicting" || mergeState === "dirty") return { kind: "conflicting" };
  if (mergeState === "clean") return { kind: "ready" };
  if (mergeState === "blocked") return { kind: "blocked" };
  if (mergeState === "behind") return { kind: "behind" };
  if (mergeState === "unstable") return { kind: "unstable" };
  if (mergeState === "has_hooks") return { kind: "has_hooks" };
  return { kind: "none" };
}

// ---------------------------------------------------------------------------
// Diff stats
// ---------------------------------------------------------------------------

export interface PullRequestStatsInput {
  additions?: number;
  deletions?: number;
  changed_files?: number;
}

// shouldShowPullRequestStats encodes the "old backend → new frontend" guard:
// when the backend that served this PR row doesn't know about the stats
// columns yet, every numeric field defaults to 0. Rendering "+0 −0 · 0 files"
// in that case would be a lie (the PR almost certainly has real changes),
// so we hide the entire stats row until at least one signal is non-zero.
export function shouldShowPullRequestStats(input: PullRequestStatsInput): boolean {
  const a = input.additions ?? 0;
  const d = input.deletions ?? 0;
  const f = input.changed_files ?? 0;
  return a + d + f > 0;
}

// ---------------------------------------------------------------------------
// Row verdict
// ---------------------------------------------------------------------------

// The one thing the sidebar row says about a PR: what stands between it and a
// merge, or how it ended. `failed` carries the failing check names and whether
// the branch also conflicts, so a PR with both problems still shows both.
export type PullRequestVerdict =
  | { kind: "merged" }
  | { kind: "closed" }
  | { kind: "failed"; failed: number; total: number; names: string[]; conflicting: boolean }
  | { kind: "conflicting" }
  | { kind: "running"; passed: number; total: number; running: number }
  | { kind: "behind" }
  | { kind: "blocked" }
  | { kind: "draft" }
  | { kind: "ready" }
  | { kind: "passed"; total: number }
  | { kind: "no_checks" }
  | { kind: "unknown" };

export interface PullRequestVerdictInput extends PullRequestChecksInput, PullRequestMergeInput {
  state: GitHubPullRequestState;
}

// Priority (high → low):
//   merged / closed → failed → conflicting → running → behind → blocked
//   → draft → ready → passed → no checks → unknown
//
// `unstable` (non-passing checks) only restates the CI verdict, and `has_hooks`
// is GitHub's "mergeable, with pre-receive hooks", so neither gets a verdict of
// its own. A draft never reads as ready: it cannot be merged until marked
// ready for review. `unknown` means there is nothing current to say (no
// snapshot, no legacy conclusion); the row renders no verdict for it.
export function derivePullRequestVerdict(input: PullRequestVerdictInput): PullRequestVerdict {
  if (input.state === "merged") return { kind: "merged" };
  if (input.state === "closed") return { kind: "closed" };
  const checks = deriveChecksStatus(input);
  const merge = deriveMergeStatus(input);
  if (checks.kind === "failed") {
    return {
      kind: "failed",
      failed: checks.failed,
      total: checks.total,
      names: checks.names,
      conflicting: merge.kind === "conflicting",
    };
  }
  if (merge.kind === "conflicting") return { kind: "conflicting" };
  if (checks.kind === "pending") {
    return { kind: "running", passed: checks.passed, total: checks.total, running: checks.running };
  }
  if (merge.kind === "behind") return { kind: "behind" };
  if (merge.kind === "blocked") return { kind: "blocked" };
  if (input.state === "draft") return { kind: "draft" };
  if (merge.kind === "ready" || merge.kind === "has_hooks") return { kind: "ready" };
  if (checks.kind === "passed") return { kind: "passed", total: checks.total };
  if (checks.kind === "none") return { kind: "no_checks" };
  return { kind: "unknown" };
}

// ---------------------------------------------------------------------------
// Row copy
// ---------------------------------------------------------------------------

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// A PR titled "MUL-1: fix x" or "fix x (MUL-1)" repeats the issue it is listed
// under. Drop that one key — a leading "MUL-1:" / "MUL-1 " or a trailing
// "(MUL-1)" — so the truncated title spends its width on what the PR does.
// Other keys, and titles that are nothing but the key, stay as written.
export function stripIssueKeyFromTitle(title: string, identifier: string): string {
  if (!identifier) return title;
  const key = escapeRegExp(identifier);
  const stripped = title
    .replace(new RegExp(`^\\s*${key}(?![\\w-])\\s*:?\\s*`, "i"), "")
    .replace(new RegExp(`\\s*\\(${key}\\)\\s*$`, "i"), "");
  return stripped.trim() ? stripped : title;
}

// Diff counts for the narrow sidebar: exact below 1,000, then one decimal of
// thousands ("6.7k"), dropping the decimal from 100k up ("123k").
export function formatPullRequestDiffCount(count: number): string {
  if (count < 1000) return String(count);
  const thousands = count / 1000;
  if (thousands >= 100) return `${Math.round(thousands)}k`;
  return `${thousands.toFixed(1).replace(/\.0$/, "")}k`;
}
