import type {
  GitHubPullRequestChecksConclusion,
  GitHubPullRequestChecksRollup,
  GitHubPullRequestMergeable,
  GitHubPullRequestMergeStateStatus,
} from "../types";

// The PR sidebar row surfaces TWO independent facts, each tri-state and each
// sourced from the GitHub API snapshot:
//
//   1. CI status      — derived from `checks_rollup` (primary) + counts.
//   2. Mergeability   — derived from `mergeable` + `merge_state_status`.
//
// The two are intentionally decoupled: a PR can have failing checks AND a merge
// conflict, and both must show. Neither element is derived from the other, and
// neither is shown for terminal PRs (merged / closed) — the row's leading state
// icon already conveys terminal state; the caller applies that gate.
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
