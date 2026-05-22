import type { GitHubPullRequest } from "../types";

// Status kinds rendered in the PR sidebar row's detail line. Order in the
// pass-through table matters — the first matching rule wins. The order is
// chosen so terminal PR states (closed / merged) short-circuit before any
// transient CI/conflict signal, since those signals are no longer actionable
// on a terminal PR.
//
// Priority (high → low):
//   1. closed (not merged)        → status_closed
//   2. merged                     → status_merged
//   3. mergeable_state = "dirty"  → status_conflicts
//   4. any failed suite           → status_checks_failed
//   5. any pending suite          → status_checks_pending
//   6. any passed suite           → status_checks_passed
//   7. no suite + mergeable=clean → status_ready
//   8. otherwise                  → status_unknown
//
// Note: this table is the single source of truth for the sidebar PR row. The
// older row-with-badges implementation used a separate "hide status row for
// terminal PRs" branch — the current row renders
// with status_closed / status_merged text, never falling through to a
// conflicts / checks line on a terminal PR. Keep this priority order in sync
// with the i18n keys `pull_request_card_status_*` and with the progress-strip
// derivation in `derivePullRequestProgressSegments` (terminal kinds get a
// solid bar; the rest map onto the per-suite counts).
export type PullRequestStatusKind =
  | "closed"
  | "merged"
  | "conflicts"
  | "checks_failed"
  | "checks_pending"
  | "checks_passed"
  | "ready"
  | "unknown";

export interface PullRequestStatusInput {
  state: GitHubPullRequest["state"];
  mergeable_state?: string | null;
  checks_failed?: number;
  checks_pending?: number;
  checks_passed?: number;
}

export function derivePullRequestStatusKind(input: PullRequestStatusInput): PullRequestStatusKind {
  if (input.state === "closed") return "closed";
  if (input.state === "merged") return "merged";
  if (input.mergeable_state === "dirty") return "conflicts";
  if ((input.checks_failed ?? 0) > 0) return "checks_failed";
  if ((input.checks_pending ?? 0) > 0) return "checks_pending";
  if ((input.checks_passed ?? 0) > 0) return "checks_passed";
  if (input.mergeable_state === "clean") return "ready";
  return "unknown";
}

export interface PullRequestProgressSegment {
  kind: "failed" | "pending" | "passed";
  ratio: number;
}

// Segmented progress bar input. Returns null when:
//   - the PR is terminal (closed/merged) — the card paints a solid bar
//     in a state-specific color, no segmentation needed;
//   - no check_suite has been observed (total === 0) — the card hides
//     the bar entirely.
// Otherwise emits the segments left-to-right: failed → pending → passed.
// "Failure first" is intentional: problems should be visible before signal
// that everything is fine.
export function derivePullRequestProgressSegments(
  input: PullRequestStatusInput,
): PullRequestProgressSegment[] | null {
  if (input.state === "closed" || input.state === "merged") return null;
  const failed = input.checks_failed ?? 0;
  const pending = input.checks_pending ?? 0;
  const passed = input.checks_passed ?? 0;
  const total = failed + pending + passed;
  if (total === 0) return null;
  const segments: PullRequestProgressSegment[] = [];
  if (failed > 0) segments.push({ kind: "failed", ratio: failed / total });
  if (pending > 0) segments.push({ kind: "pending", ratio: pending / total });
  if (passed > 0) segments.push({ kind: "passed", ratio: passed / total });
  return segments;
}

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
