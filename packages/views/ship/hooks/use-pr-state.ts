import type { PullRequest } from "@multica/core/types";

// Phase 1 Kanban columns. Naming matches the i18n keys in ship.json so the
// derivation result can be used directly as a translation key:
//   t($ => $.kanban.drafted) etc.
export type ShipKanbanColumn =
  | "drafted"
  | "in_review"
  | "ready_to_land"
  | "recently_merged";

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

/**
 * Derive the Kanban column for a PR.
 *
 * Phase 1 — review_decision is empty in most cases (the backend doesn't
 * enrich it yet). The "In Review" predicate intentionally treats empty +
 * REVIEW_REQUIRED + CHANGES_REQUESTED as the same column so the board
 * degrades gracefully until Phase 2 adds the enrichment.
 *
 * "Recently Merged" is anchored on the merged-at timestamp; PRs merged
 * more than 7 days ago drop off the board entirely (return null).
 *
 * Returns null when the PR doesn't belong on the Kanban (closed-not-merged,
 * stale-merged). Caller is expected to filter those out — they're listed
 * elsewhere or implicitly excluded.
 */
export function deriveShipKanbanColumn(
  pr: PullRequest,
  now: Date = new Date(),
): ShipKanbanColumn | null {
  if (pr.state === "open") {
    if (pr.is_draft) return "drafted";
    // Empty review_decision treated as "In Review" so degraded data still
    // renders. APPROVED with no failing CI graduates to Ready to Land.
    if (
      pr.review_decision === "APPROVED" &&
      pr.ci_status !== "failure"
    ) {
      return "ready_to_land";
    }
    return "in_review";
  }
  if (pr.state === "merged" && pr.pr_merged_at) {
    const mergedAt = new Date(pr.pr_merged_at).getTime();
    if (Number.isFinite(mergedAt) && now.getTime() - mergedAt < SEVEN_DAYS_MS) {
      return "recently_merged";
    }
  }
  return null;
}

/**
 * Phase 1 "failing / blocked" rail predicate. A PR shows up here when the
 * Kanban already shows it (state === "open") AND it has a hard blocker —
 * failing CI or a merge conflict. The rail is rendered above the columns
 * so the user notices immediately; the same PR also still appears in its
 * derived column (so the rail isn't a hide-and-seek surface).
 */
export function isFailingOrBlocked(pr: PullRequest): boolean {
  if (pr.state !== "open") return false;
  if (pr.ci_status === "failure") return true;
  if (pr.mergeable === "CONFLICTING") return true;
  return false;
}

const RISK_KEYWORDS = ["migration", "schema"] as const;

/** Surface a small "touches schema / migration" warning chip on the PR card.
 *  Phase-1 heuristic — title substring or label name match. The full risk
 *  profiler lands in Phase 5; this gives a useful chip with zero backend
 *  work.
 *  Returns the matched keyword (caller uses it to pick a translation
 *  string) or null. */
export function deriveRiskHint(
  pr: PullRequest,
): "schema" | "migration" | null {
  const title = pr.title.toLowerCase();
  const labels = pr.labels.map((l) => l.name.toLowerCase());
  for (const kw of RISK_KEYWORDS) {
    if (title.includes(kw) || labels.includes(kw)) return kw;
  }
  return null;
}

/** Buckets a flat PR array into Kanban columns + the failing/blocked rail.
 *  Returned arrays preserve input order — the caller already sorted by
 *  pr_updated_at desc on the server side. The rail is a parallel view
 *  (not exclusive); a failing-PR also still shows up in `in_review` etc.
 */
export interface KanbanBuckets {
  drafted: PullRequest[];
  in_review: PullRequest[];
  ready_to_land: PullRequest[];
  recently_merged: PullRequest[];
  failing_blocked: PullRequest[];
}

export function bucketPullRequests(
  prs: PullRequest[],
  now: Date = new Date(),
): KanbanBuckets {
  const buckets: KanbanBuckets = {
    drafted: [],
    in_review: [],
    ready_to_land: [],
    recently_merged: [],
    failing_blocked: [],
  };
  for (const pr of prs) {
    const col = deriveShipKanbanColumn(pr, now);
    if (col) buckets[col].push(pr);
    if (isFailingOrBlocked(pr)) buckets.failing_blocked.push(pr);
  }
  return buckets;
}
