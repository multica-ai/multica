import type { GitHubPullRequest } from "../types";

// Fail-only alert model for the PR sidebar row (MUL-5180).
//
// The read-only GitHub App only ever receives *completed* check suites, so
// "some observed suite failed" is the single CI signal Multica can vouch for.
// "Everything passed", "still running", and "all done" are unknowable from
// webhooks alone: a green roll-up could just mean the slower provider has not
// reported yet. Rather than render answers we cannot stand behind, the row
// stays silent unless something needs attention:
//
//   - checksFailed: a completed suite on the current head concluded failure.
//   - conflicts:    GitHub says the PR cannot merge (mergeable_state dirty).
//
// Both are suppressed on terminal PRs (closed / merged) — the alert is no
// longer actionable and the row's state icon already tells the story.
//
// Do not add passed / pending / progress states back here without a data
// source that actually observes them (check_run events or a checks API
// reconciliation) — see the MUL-5180 discussion.
export interface PullRequestAlertsInput {
  state: GitHubPullRequest["state"];
  mergeable_state?: string | null;
  checks_failed?: number;
}

export interface PullRequestAlerts {
  checksFailed: boolean;
  conflicts: boolean;
}

export function derivePullRequestAlerts(input: PullRequestAlertsInput): PullRequestAlerts {
  const terminal = input.state === "closed" || input.state === "merged";
  return {
    checksFailed: !terminal && (input.checks_failed ?? 0) > 0,
    conflicts: !terminal && input.mergeable_state === "dirty",
  };
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
