import type { DeployEnvironment, PullRequest } from "@multica/core/types";

// Phase 2 Kanban columns. Naming matches the i18n keys in ship.json so the
// derivation result can be used directly as a translation key:
//   t($ => $.kanban.column.in_staging) etc.
//
// Columns are listed left-to-right as they appear on the board. The Phase 1
// names ("drafted" / "in_review" / "ready_to_land" / "recently_merged") were
// renamed to "merged_pre_staging" because the rest of the deploy lifecycle
// columns now exist after it.
export type ShipKanbanColumn =
  | "drafted"
  | "in_review"
  | "ready_to_land"
  | "merged_pre_staging"
  | "in_staging"
  | "promoting"
  | "in_production"
  | "done";

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;
const ONE_DAY_MS = 24 * 60 * 60 * 1000;

/** Phase 2 deploy snapshot the column derivation needs. The Kanban hook
 *  receives this from the per-project deploy environments query and the
 *  per-environment recent deploys query (for the in-flight detection). It's
 *  intentionally a pure data shape — no live event subscription here.
 *
 *  For the "Promoting" check we need to know whether ANY in-flight deploy
 *  to production is currently targeting a given SHA. We pass the set of
 *  SHAs that are currently being deployed to production rather than the
 *  full deploy list — the Kanban doesn't need anything else and a string
 *  Set is the cheapest predicate. */
export interface ShipDeploySnapshot {
  staging?: DeployEnvironment | null;
  production?: DeployEnvironment | null;
  /** SHAs currently being deployed to production (status === "pending" |
   *  "in_progress"). Used by the "Promoting" predicate. */
  productionInFlightShas: Set<string>;
}

/** Helper: which SHA represents this PR on the deploy lane?
 *
 *  Phase 2 simplification — exact head_sha match. The "merge commit
 *  reachable from current_sha" case requires walking git history, which
 *  the frontend can't do without a backend endpoint. Since GitHub uses a
 *  squash-merge by default and we record `head_sha` from the squash result,
 *  exact match works for the squash-merge case. Phase 5 revisits for
 *  rebase-and-merge teams. */
function prShas(pr: PullRequest): string[] {
  // Empty SHAs are filtered out so a not-yet-fetched PR doesn't accidentally
  // match other empty fields.
  return [pr.head_sha].filter(Boolean);
}

function isPRDeployedTo(
  pr: PullRequest,
  env: DeployEnvironment | null | undefined,
): boolean {
  if (!env || !env.current_sha) return false;
  return prShas(pr).includes(env.current_sha);
}

/**
 * Derive the Kanban column for a PR.
 *
 * Phase 2 — review_decision and ci_status are now populated in real time
 * from webhooks. The "In Review" predicate still treats empty +
 * REVIEW_REQUIRED + CHANGES_REQUESTED as the same column so the board
 * degrades gracefully on workspaces that haven't received webhook traffic
 * yet (e.g. fresh setup, GitHub down).
 *
 * Returns null when the PR doesn't belong on the Kanban (closed-not-merged,
 * stale-merged with no production trace).
 */
export function deriveShipKanbanColumn(
  pr: PullRequest,
  snapshot: ShipDeploySnapshot,
  now: Date = new Date(),
): ShipKanbanColumn | null {
  // Open PRs flow through the review/CI gate columns.
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

  if (pr.state !== "merged") return null;

  // Merged PRs progress through deploy columns. The order of these checks
  // matters — the most-advanced state wins so a PR that's currently in
  // production isn't surfaced under "in_staging" too.
  const inProd = isPRDeployedTo(pr, snapshot.production);
  if (inProd && snapshot.production?.current_deployed_at) {
    const prodAt = new Date(snapshot.production.current_deployed_at).getTime();
    if (Number.isFinite(prodAt) && now.getTime() - prodAt < ONE_DAY_MS) {
      return "in_production";
    }
  }

  // "Promoting" — there's a production deploy in flight whose target SHA
  // matches this PR. Even before production.current_sha catches up, we
  // want to show the user that the PR is *being* promoted right now.
  for (const sha of prShas(pr)) {
    if (snapshot.productionInFlightShas.has(sha)) return "promoting";
  }

  // "In Staging" — head SHA matches the staging environment's current SHA.
  if (isPRDeployedTo(pr, snapshot.staging)) {
    return "in_staging";
  }

  // "Done" — was on prod some time in the past. Phase 2 simplification (per
  // the spec): a merged PR that's older than 24h on production OR was merged
  // more than 7 days ago lands here. The exact "was on prod" tracking lives
  // in Phase 5 once we record deploy history per SHA.
  if (inProd) return "done";

  if (pr.pr_merged_at) {
    const mergedAt = new Date(pr.pr_merged_at).getTime();
    if (Number.isFinite(mergedAt)) {
      const ageMs = now.getTime() - mergedAt;
      if (ageMs >= SEVEN_DAYS_MS) return "done";
      // Recent merge that's not yet on staging → "Merged · Pre-Staging".
      return "merged_pre_staging";
    }
  }

  // Defensive: merged PR with no merged_at timestamp (server bug) lands in
  // pre-staging so it stays visible. Better to show than to drop silently.
  return "merged_pre_staging";
}

/**
 * Phase 2 "failing / blocked" rail predicate. A PR shows up here when it's
 * still open AND has a hard blocker — failing CI or a merge conflict. The
 * rail is rendered above the columns so the user notices immediately; the
 * same PR also still appears in its derived column.
 *
 * Closed/merged PRs are excluded from the rail because the user can't
 * action them anymore (a merged PR with failing CI is post-hoc info, not a
 * todo).
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
  merged_pre_staging: PullRequest[];
  in_staging: PullRequest[];
  promoting: PullRequest[];
  in_production: PullRequest[];
  done: PullRequest[];
  failing_blocked: PullRequest[];
}

/** Empty snapshot used by tests and by the kanban while the deploy queries
 *  are still loading. With no environments, every merged PR falls into
 *  `merged_pre_staging` (recent) or `done` (old) — no "in staging /
 *  promoting / in production" classification, which mirrors the Phase 1
 *  behavior. */
export const EMPTY_DEPLOY_SNAPSHOT: ShipDeploySnapshot = {
  staging: null,
  production: null,
  productionInFlightShas: new Set<string>(),
};

export function bucketPullRequests(
  prs: PullRequest[],
  snapshot: ShipDeploySnapshot = EMPTY_DEPLOY_SNAPSHOT,
  now: Date = new Date(),
): KanbanBuckets {
  const buckets: KanbanBuckets = {
    drafted: [],
    in_review: [],
    ready_to_land: [],
    merged_pre_staging: [],
    in_staging: [],
    promoting: [],
    in_production: [],
    done: [],
    failing_blocked: [],
  };
  for (const pr of prs) {
    const col = deriveShipKanbanColumn(pr, snapshot, now);
    if (col) buckets[col].push(pr);
    if (isFailingOrBlocked(pr)) buckets.failing_blocked.push(pr);
  }
  return buckets;
}
