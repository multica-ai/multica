import type {
  Issue,
  IssueStatusCategory,
  IssueStatusBucket,
  ListIssuesCache,
} from "../types";
import { PAGINATED_CATEGORIES } from "./queries";
import { issueStatusCategory, normalizeStatusPatch } from "./status-category";

const EMPTY_BUCKET: IssueStatusBucket = { issues: [], total: 0 };

export function getBucket(
  resp: ListIssuesCache,
  status: IssueStatusCategory,
): IssueStatusBucket {
  return resp.byStatus[status] ?? EMPTY_BUCKET;
}

export function setBucket(
  resp: ListIssuesCache,
  status: IssueStatusCategory,
  bucket: IssueStatusBucket,
): ListIssuesCache {
  return { ...resp, byStatus: { ...resp.byStatus, [status]: bucket } };
}

/** Locate which status bucket holds `id`, if any. */
export function findIssueLocation(
  resp: ListIssuesCache,
  id: string,
): { status: IssueStatusCategory; issue: Issue } | null {
  for (const status of PAGINATED_CATEGORIES) {
    const bucket = resp.byStatus[status];
    const found = bucket?.issues.find((i: Issue) => i.id === id);
    if (found) return { status, issue: found };
  }
  return null;
}

/** Add an issue to its status bucket (no-op if already present). */
export function addIssueToBuckets(
  resp: ListIssuesCache,
  issue: Issue,
): ListIssuesCache {
  // An unresolvable custom status has no column to place the issue in. The
  // caller is responsible for invalidating instead — silently dropping it would
  // leave the board missing a row that exists on the server. Callers use
  // `issueStatusCategory(issue) === null` to detect this before calling.
  const category = issueStatusCategory(issue);
  if (!category) return resp;
  const bucket = getBucket(resp, category);
  if (bucket.issues.some((i: Issue) => i.id === issue.id)) return resp;
  return setBucket(resp, category, {
    issues: [...bucket.issues, issue],
    total: bucket.total + 1,
  });
}

/** Remove an issue from whichever bucket contains it. */
export function removeIssueFromBuckets(
  resp: ListIssuesCache,
  id: string,
): ListIssuesCache {
  const loc = findIssueLocation(resp, id);
  if (!loc) return resp;
  const bucket = getBucket(resp, loc.status);
  return setBucket(resp, loc.status, {
    issues: bucket.issues.filter((i) => i.id !== id),
    total: Math.max(0, bucket.total - 1),
  });
}

/**
 * Count-only reconcile for an issue BEYOND the loaded window: its status
 * changed server-side, so one unit of `total` moves between buckets while
 * the loaded arrays stay untouched. `hasMore` (loaded < total) stays
 * consistent for free.
 */
export function moveBucketTotal(
  resp: ListIssuesCache,
  from: IssueStatusCategory,
  to: IssueStatusCategory,
): ListIssuesCache {
  if (from === to) return resp;
  const fromBucket = getBucket(resp, from);
  const toBucket = getBucket(resp, to);
  let next = setBucket(resp, from, {
    ...fromBucket,
    total: Math.max(0, fromBucket.total - 1),
  });
  next = setBucket(next, to, { ...toBucket, total: toBucket.total + 1 });
  return next;
}

/**
 * Count-only reconcile for an issue BEYOND the loaded window that LEFT the
 * list (reassigned / moved project): the bucket it was counted in loses one.
 */
export function decrementBucketTotal(
  resp: ListIssuesCache,
  status: IssueStatusCategory,
): ListIssuesCache {
  const bucket = getBucket(resp, status);
  return setBucket(resp, status, {
    ...bucket,
    total: Math.max(0, bucket.total - 1),
  });
}

/**
 * Insert `issue` into `issues` at the slot implied by `position ASC` — the same
 * ordering the board renders (server `ORDER BY position ASC`). Returns a new
 * array; the input is not mutated.
 *
 * Inserting at the right slot (instead of appending to the end) is what keeps an
 * optimistic move from snapping: the card lands where it will be after the
 * server confirms, so no later cache refresh teleports it to the column tail.
 */
export function insertByPosition(issues: Issue[], issue: Issue): Issue[] {
  const idx = issues.findIndex((i) => i.position > issue.position);
  if (idx === -1) return [...issues, issue];
  return [...issues.slice(0, idx), issue, ...issues.slice(idx)];
}

/**
 * Merge `patch` into the issue with `id`. If `patch.status` differs from the
 * current bucket, the issue moves to the new bucket and both buckets' totals
 * are adjusted. The moved card — and a same-column card whose `position`
 * changed — is re-inserted at its `position`-sorted slot rather than appended,
 * so the cache order stays consistent with what the board renders. A plain
 * field update (no status/position change) keeps the card in place.
 */
export function patchIssueInBuckets(
  resp: ListIssuesCache,
  id: string,
  rawPatch: Partial<Issue>,
): ListIssuesCache {
  const loc = findIssueLocation(resp, id);
  if (!loc) return resp;
  // Normalized so the merged entity's status_category matches the bucket it
  // lands in; a bare spread would keep the previous category. (MUL-6243)
  const patch = normalizeStatusPatch(rawPatch);
  const merged: Issue = { ...loc.issue, ...patch };

  // Resolve the DESTINATION category before comparing anything. `loc.status` is
  // a CATEGORY (the bucket key) while `patch.status` is a status KEY, so
  // comparing them directly treats a same-category key change — `in_review` to
  // a custom `human_review` — as a cross-bucket move. That path then deletes
  // from and inserts into the same bucket using a pre-delete snapshot, leaving
  // the card duplicated and the total one too high.
  //
  // Resolved from `patch` alone, never from `merged`: merged inherits the
  // PREVIOUS issue's status_category, which would silently keep the old column
  // after a real move.
  const nextCategory =
    patch.status === undefined
      ? loc.status
      : issueStatusCategory({ status: patch.status, status_category: patch.status_category });

  // Unresolvable custom status: no bucket to move it to. Leave the cache alone
  // and let the caller invalidate — see patchNeedsInvalidation.
  if (!nextCategory) return resp;

  if (nextCategory === loc.status) {
    const bucket = getBucket(resp, loc.status);
    const positionChanged =
      patch.position !== undefined && patch.position !== loc.issue.position;
    if (!positionChanged) {
      // Plain field update (labels, metadata, title, a same-category status
      // key change …): keep the slot so a remote edit never reorders an
      // otherwise-untouched column, and never change the total.
      return setBucket(resp, loc.status, {
        ...bucket,
        issues: bucket.issues.map((i: Issue) => (i.id === id ? merged : i)),
      });
    }
    // Same-column reorder: lift the card out and re-insert at its new slot.
    return setBucket(resp, loc.status, {
      ...bucket,
      issues: insertByPosition(
        bucket.issues.filter((i: Issue) => i.id !== id),
        merged,
      ),
    });
  }

  const fromBucket = getBucket(resp, loc.status);
  const toBucket = getBucket(resp, nextCategory);
  let next = setBucket(resp, loc.status, {
    issues: fromBucket.issues.filter((i: Issue) => i.id !== id),
    total: Math.max(0, fromBucket.total - 1),
  });
  next = setBucket(next, nextCategory, {
    issues: insertByPosition(toBucket.issues, merged),
    total: toBucket.total + 1,
  });
  return next;
}

/**
 * True when a status patch names a status this client cannot resolve to a
 * category, so `patchIssueInBuckets` will no-op. Callers must invalidate rather
 * than treat that as "nothing to do" — the row moved on the server, and leaving
 * the cache untouched drifts the column and its total permanently.
 */
export function patchNeedsInvalidation(patch: Partial<Issue>): boolean {
  if (patch.status === undefined) return false;
  return (
    issueStatusCategory({ status: patch.status, status_category: patch.status_category }) === null
  );
}
