/**
 * Cross-type cancelled demotion for aggregated search results (MUL-5824).
 *
 * The search API already ranks cancelled work below live work — but only
 * *within one result set*. Every client that renders issues and projects
 * together aggregates two independent responses, so a cancelled row from one
 * response still lands above a live row from the other: the command palette
 * and the mobile search screen both render the whole Projects list before the
 * whole Issues list, so a single cancelled project outranks every live issue.
 *
 * The fix has to live at the aggregation point, and it has to be a *stable*
 * partition across BOTH types: relative order inside each side is preserved, so
 * the server's ranking still decides everything except "live before cancelled".
 * Applying it before any truncation is what makes cancelled rows give up their
 * slot rather than merely their position.
 *
 * Direct hits are exempt, matching the server: an exact identifier, an exact
 * bare number, or an exact title means the user is targeting that one record
 * and demoting it would hide exactly what they asked for.
 */

import type { SearchIssueResult, SearchProjectResult } from "../types/api";

/**
 * Mirrors the server's identifier pattern (parseQueryNumber in
 * server/internal/handler/issue.go): "MUL-123" or a bare "123".
 */
const IDENTIFIER_NUMBER_RE = /^[a-z]+-(\d+)$/i;

/** Extracts the issue number a query targets, or null when it targets none. */
export function parseSearchQueryNumber(query: string): number | null {
  const q = query.trim();
  const m = IDENTIFIER_NUMBER_RE.exec(q);
  if (m) {
    const n = Number.parseInt(m[1]!, 10);
    if (Number.isInteger(n) && n > 0) return n;
  }
  if (/^\d+$/.test(q)) {
    const n = Number.parseInt(q, 10);
    if (Number.isInteger(n) && n > 0) return n;
  }
  return null;
}

function isExactTitle(title: string | null | undefined, query: string): boolean {
  if (!title) return false;
  const q = query.trim();
  if (!q) return false;
  return title.trim().toLowerCase() === q.toLowerCase();
}

/**
 * A query targets this specific issue: exact identifier / bare number, or the
 * full title.
 *
 * Every field is optional so callers holding only part of a row can share the
 * one rule — the @mention picker, for instance, carries the identifier and the
 * title but no `number`, which is then derived from the identifier.
 *
 * Note: the server compares the title against an escapeLike'd parameter, so a
 * title containing `_` or `%` never counts as a direct hit there (a pre-existing
 * tier-1 escaping quirk, see buildSearchQuery). Client-side we compare the raw
 * strings, which can only ever *keep* such an issue in the live partition —
 * never demote something the server kept up — so the two stay compatible.
 */
export function isIssueDirectHit(
  issue: {
    title?: string | null;
    number?: number | null;
    identifier?: string | null;
  },
  query: string,
): boolean {
  const targetNumber = parseSearchQueryNumber(query);
  if (targetNumber !== null) {
    const issueNumber =
      issue.number ??
      (issue.identifier ? parseSearchQueryNumber(issue.identifier) : null);
    if (issueNumber === targetNumber) return true;
  }
  if (
    issue.identifier &&
    issue.identifier.trim().toLowerCase() === query.trim().toLowerCase()
  ) {
    return true;
  }
  return isExactTitle(issue.title, query);
}

/** A query targets this specific project: its full title. */
export function isProjectDirectHit(
  project: { title?: string | null },
  query: string,
): boolean {
  return isExactTitle(project.title, query);
}

export interface CancelledPartition<T> {
  /** Everything that is not demoted, in its original relative order. */
  live: T[];
  /** Demoted cancelled rows, in their original relative order. */
  cancelled: T[];
}

/**
 * Stable partition on an arbitrary predicate. Split out so the caller decides
 * what "cancelled and not exempt" means for its own row shape, while the
 * order-preserving guarantee lives in one place.
 */
export function partitionStable<T>(
  items: readonly T[],
  demote: (item: T) => boolean,
): CancelledPartition<T> {
  const live: T[] = [];
  const cancelled: T[] = [];
  for (const item of items) {
    if (demote(item)) cancelled.push(item);
    else live.push(item);
  }
  return { live, cancelled };
}

export interface AggregatedSearchPartition {
  /** Projects to render first, minus the demoted ones. */
  liveProjects: SearchProjectResult[];
  /** Issues to render after the live projects, minus the demoted ones. */
  liveIssues: SearchIssueResult[];
  /**
   * Cancelled projects then cancelled issues — one trailing bucket, so no
   * cancelled row of either type can precede a live row of the other.
   */
  cancelledProjects: SearchProjectResult[];
  cancelledIssues: SearchIssueResult[];
  /** True when anything was demoted; lets callers skip an empty section. */
  hasCancelled: boolean;
}

/**
 * Partitions a global-search result pair (issues + projects) for rendering.
 *
 * Render order must be: live projects → live issues → cancelled (projects then
 * issues). Callers that truncate must truncate the concatenation in that order,
 * so the cancelled tail is what gets dropped.
 */
export function partitionAggregatedSearchResults({
  issues,
  projects,
  query,
}: {
  issues: readonly SearchIssueResult[];
  projects: readonly SearchProjectResult[];
  query: string;
}): AggregatedSearchPartition {
  const issueParts = partitionStable(
    issues,
    (issue) => issue.status === "cancelled" && !isIssueDirectHit(issue, query),
  );
  const projectParts = partitionStable(
    projects,
    (project) =>
      project.status === "cancelled" && !isProjectDirectHit(project, query),
  );

  return {
    liveProjects: projectParts.live,
    liveIssues: issueParts.live,
    cancelledProjects: projectParts.cancelled,
    cancelledIssues: issueParts.cancelled,
    hasCancelled:
      projectParts.cancelled.length > 0 || issueParts.cancelled.length > 0,
  };
}
