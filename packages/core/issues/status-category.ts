import type { Issue, IssueStatusCategory } from "../types";
import { isIssueStatusCategory } from "../issue-statuses";

/**
 * The category an issue's status belongs to — the bucket it occupies on the
 * board (MUL-6243).
 *
 * Pure on purpose: the cache helpers that call it run outside React and must
 * not reach for a catalog. It reads the server-provided `status_category` when
 * present and otherwise falls back to the rule that makes that field optional
 * in the first place — a BUILT-IN status key is its own category. Since custom
 * statuses only exist once an admin creates one, the fallback is exact for
 * every workspace that has none.
 *
 * Returns null when the status is a custom key this response did not resolve,
 * so callers can skip bucketing rather than guessing a wrong column.
 */
export function issueStatusCategory(issue: Pick<Issue, "status" | "status_category">): IssueStatusCategory | null {
  const fromServer = issue.status_category;
  if (fromServer && isIssueStatusCategory(fromServer)) return fromServer;
  if (isIssueStatusCategory(issue.status)) return issue.status;
  return null;
}

/**
 * Category for a bare status KEY, for render paths that hold only the string.
 *
 * Exact for the 7 built-ins, which is every status that exists until an admin
 * defines a custom one. A custom key returns `todo` so presentation lookups
 * always resolve to something renderable; surfaces that must show the real
 * status use the catalog (`useIssueStatuses`) instead. (MUL-6243)
 */
export function statusCategoryOfKey(statusKey: string): IssueStatusCategory {
  return isIssueStatusCategory(statusKey) ? statusKey : "todo";
}

/**
 * Rewrites a patch's `status_category` to match its `status`, before the patch
 * reaches any cache (MUL-6243).
 *
 * The server now sends a category on every issue, so a cached entity looks like
 * `{status: "todo", status_category: "todo"}`. An optimistic patch carries only
 * `{status: "done"}`, and a bare `{...issue, ...patch}` therefore keeps the
 * STALE `status_category: "todo"` while the card moves to the done bucket. A
 * single update self-heals when the full server response lands, but the batch
 * API returns only `{updated: n}` and does not refetch bucketed lists — so
 * without this the entity stays permanently inconsistent with the bucket it
 * sits in, and the next off-window count decrements the wrong bucket.
 *
 * A patch that does not touch `status` is returned unchanged. A custom key with
 * no authoritative category is left alone too: it is unresolvable, and
 * `patchNeedsInvalidation` routes it to a refetch rather than a guess — but the
 * stale inherited value is dropped so nothing downstream trusts it.
 */
export function normalizeStatusPatch(patch: Partial<Issue>): Partial<Issue> {
  if (patch.status === undefined) return patch;
  const category = issueStatusCategory({
    status: patch.status,
    status_category: patch.status_category,
  });
  // Undefined rather than the inherited value: an unresolvable status must not
  // silently keep the previous category.
  return { ...patch, status_category: category ?? undefined };
}
