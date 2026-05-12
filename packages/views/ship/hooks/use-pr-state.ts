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

/** Parse env.current_deployed_at into a ms timestamp, defaulting to 0
 *  (epoch) on missing/invalid so the `prodAt > mergedAt` comparisons in
 *  the time-based fallback never accidentally consider a missing
 *  deploy as "newer than any merge". */
function parseDeployedAt(iso: string | null | undefined): number {
  if (!iso) return 0;
  const t = new Date(iso).getTime();
  return Number.isFinite(t) ? t : 0;
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

  // Bug fix — fall back to release stage when SHA matching has nothing
  // to say. Squash-merging changes the commit SHA, so a PR's head_sha
  // never matches main's deploy SHA and the snapshot-based logic above
  // can't promote merged PRs out of "merged_pre_staging" even after
  // they've been deployed via a release. The release object tracks
  // user-intent ("this PR shipped in this release, the release is in
  // production") which is the source of truth when the snapshot lags.
  //
  // Only apply this fallback when SHA-matching had nothing definitive
  // — if the snapshot says "in production right now" we still trust
  // that over a stale release stage.
  const releaseCol = releaseStageToColumn(pr.active_release?.stage);
  if (releaseCol) return releaseCol;

  // Time-based deploy fallback — the only reliable signal for
  // squash-merged PRs that aren't part of a Ship Hub release. If
  // env.current_deployed_at is AFTER pr.pr_merged_at, the PR's merge
  // commit was on main when the env deployed, so it shipped. Works
  // because squash-merges land on main as a new commit whose timestamp
  // is >= pr.pr_merged_at; the env's deploy of main captures that
  // commit. Doesn't need any sha matching.
  //
  // Age check runs FIRST so old PRs land in "done" regardless of
  // recent redeploys that happened to include their code — the
  // Kanban is for in-flight work, not "what's live right now". A
  // PR merged 30 days ago that gets redeployed today is still
  // historically "done" from a flow-of-work standpoint.
  if (pr.pr_merged_at) {
    const mergedAt = new Date(pr.pr_merged_at).getTime();
    if (Number.isFinite(mergedAt)) {
      const ageMs = now.getTime() - mergedAt;
      if (ageMs >= SEVEN_DAYS_MS) return "done";

      // Recent merge: a deploy that's NEWER than the merge timestamp
      // includes this PR. Promote out of pre-staging accordingly.
      const prodAt = parseDeployedAt(snapshot.production?.current_deployed_at);
      if (prodAt > mergedAt) {
        if (now.getTime() - prodAt < ONE_DAY_MS) return "in_production";
        return "done";
      }
      const stagingAt = parseDeployedAt(snapshot.staging?.current_deployed_at);
      if (stagingAt > mergedAt) {
        return "in_staging";
      }
      // Recent merge that's not yet on staging → "Merged · Pre-Staging".
      return "merged_pre_staging";
    }
  }

  // Defensive: merged PR with no merged_at timestamp (server bug) lands in
  // pre-staging so it stays visible. Better to show than to drop silently.
  return "merged_pre_staging";
}

/** Map a release stage to a Kanban column when the snapshot SHA-matching
 *  path can't place a merged PR. Returns null for stages where the
 *  release doesn't tell us anything more advanced than the default
 *  (pre-staging) — those fall through to the merge-age based logic.
 *
 *  Defensive against server-driven enum drift (CLAUDE.md "Enum drift
 *  downgrades, not crashes"): unknown stage strings return null and the
 *  PR keeps its pre-existing column. */
function releaseStageToColumn(
  stage: string | undefined,
): ShipKanbanColumn | null {
  switch (stage) {
    case "in_staging":
    case "verifying":
      return "in_staging";
    case "promoting":
      return "promoting";
    case "in_production":
      return "in_production";
    case "done":
    case "rolled_back":
      return "done";
    // assembling / merging / cancelled / unknown → no override, let the
    // age-based default decide ("merged_pre_staging" for recent, "done"
    // for old).
    default:
      return null;
  }
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

/** Risk tier rendered on the PR card. Phase 5 — backend-classified.
 *  Treat the wire string as loose; unknown values fall back to medium
 *  (the same neutral chip Phase 1's keyword heuristic produced).
 *  See server/internal/service/ship/risk.go for the classifier rules. */
export type CardRiskLevel = "low" | "medium" | "high" | "critical";

export interface CardRiskHint {
  level: CardRiskLevel;
  reasons: string[];
}

/** Read the server-classified risk off the PR row. Returns null when
 *  the level is medium AND there are no reasons — that's the "no
 *  hint" state where we don't want a chip at all so the card stays
 *  visually clean.
 *
 *  We deliberately don't render anything for "low" either: a docs PR
 *  doesn't need a green badge cluttering the card. The risk chip is
 *  for the surfaces that actually need attention. */
export function deriveRiskHint(pr: PullRequest): CardRiskHint | null {
  const raw = (pr.risk_level ?? "medium").toLowerCase();
  const reasons = Array.isArray(pr.risk_reasons) ? pr.risk_reasons : [];
  let level: CardRiskLevel;
  switch (raw) {
    case "low":
      // Drop the chip entirely for low-risk PRs; the card stays clean.
      return null;
    case "high":
      level = "high";
      break;
    case "critical":
      level = "critical";
      break;
    case "medium":
    default:
      // Only show the medium chip when the classifier had something
      // specific to say (e.g. "migration mentioned in title"). A
      // bare medium with no reasons is the default state — no chip.
      if (reasons.length === 0) return null;
      level = "medium";
      break;
  }
  return { level, reasons };
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
