import type {
  Issue,
  IssueStatus,
  IssueStatusBucket,
  ListIssuesCache,
} from "../types";
import { PAGINATED_STATUSES } from "./queries";

const EMPTY_BUCKET: IssueStatusBucket = { issues: [], total: 0 };

export function getBucket(
  resp: ListIssuesCache,
  status: IssueStatus,
): IssueStatusBucket {
  return resp.byStatus[status] ?? EMPTY_BUCKET;
}

export function setBucket(
  resp: ListIssuesCache,
  status: IssueStatus,
  bucket: IssueStatusBucket,
): ListIssuesCache {
  return { ...resp, byStatus: { ...resp.byStatus, [status]: bucket } };
}

/** Locate which status bucket holds `id`, if any. */
export function findIssueLocation(
  resp: ListIssuesCache,
  id: string,
): { status: IssueStatus; issue: Issue } | null {
  for (const status of PAGINATED_STATUSES) {
    const bucket = resp.byStatus[status];
    const found = bucket?.issues.find((i) => i.id === id);
    if (found) return { status, issue: found };
  }
  return null;
}

/** Add an issue to its status bucket (no-op if already present). */
export function addIssueToBuckets(
  resp: ListIssuesCache,
  issue: Issue,
): ListIssuesCache {
  const bucket = getBucket(resp, issue.status);
  if (bucket.issues.some((i) => i.id === issue.id)) return resp;
  return setBucket(resp, issue.status, {
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
 * Merge `patch` into the issue with `id`. If `patch.status` differs from the
 * current bucket, the issue moves to the new bucket and both buckets' totals
 * are adjusted.
 */
export function patchIssueInBuckets(
  resp: ListIssuesCache,
  id: string,
  patch: Partial<Issue>,
): ListIssuesCache {
  const loc = findIssueLocation(resp, id);
  if (!loc) return resp;
  const merged: Issue = { ...loc.issue, ...patch };
  const nextStatus = patch.status ?? loc.status;

  if (nextStatus === loc.status) {
    const bucket = getBucket(resp, loc.status);
    return setBucket(resp, loc.status, {
      ...bucket,
      issues: bucket.issues.map((i) => (i.id === id ? merged : i)),
    });
  }

  const fromBucket = getBucket(resp, loc.status);
  const toBucket = getBucket(resp, nextStatus);
  let next = setBucket(resp, loc.status, {
    issues: fromBucket.issues.filter((i) => i.id !== id),
    total: Math.max(0, fromBucket.total - 1),
  });
  next = setBucket(next, nextStatus, {
    issues: [...toBucket.issues, merged],
    total: toBucket.total + 1,
  });
  return next;
}
