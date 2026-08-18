import type { Issue, IssueStatusCategory } from "../types";
import { isIssueStatusCategory } from "../issue-statuses";
import type { IssueStatusCatalog } from "../issue-statuses";
import { ALL_STATUSES } from "./config";

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

/**
 * How an exact status-key filter resolves to board/list COLUMNS (MUL-6243).
 *
 * Three states, not two. Built-in keys resolve with no catalog at all, because
 * a built-in key IS its own category. A CUSTOM key does not — and `categoryOf`
 * answers `todo` for anything it has not loaded, which is indistinguishable
 * from a real `todo`. Routing on that guess paged the todo column for a saved
 * `qa` filter while the query still restricted `status=qa`.
 *
 * Returning an empty column set for that case is equally wrong: the caller
 * cannot tell "narrow to nothing" from "cannot answer yet", so it fetched no
 * branches and rendered an empty board with no spinner and no error. Hence the
 * explicit state:
 *
 * - `resolved` — every key answered; `columns` is authoritative.
 * - `pending`  — a custom key needs a catalog that is still in flight. Hold the
 *                surface's loading state; do not fetch and do not render empty.
 * - `error`    — the catalog request failed. Show a retryable error; a custom
 *                filter cannot be honoured without it.
 */
export type StatusFilterColumnsResult =
  | { state: "resolved"; columns: Set<IssueStatusCategory> }
  | { state: "pending" }
  | { state: "error" };

export function statusFilterColumns(
  statusFilters: readonly string[],
  catalog: Pick<IssueStatusCatalog, "entryOf" | "isLoaded" | "isPending" | "isError">,
): StatusFilterColumnsResult {
  const columns = new Set<IssueStatusCategory>();
  for (const key of statusFilters) {
    if (isIssueStatusCategory(key)) {
      columns.add(key);
      continue;
    }
    // A custom key. Without an authoritative catalog there is no honest answer.
    if (catalog.isError) return { state: "error" };
    if (!catalog.isLoaded) return { state: "pending" };
    const category = catalog.entryOf(key)?.category;
    // A LOADED catalog that does not know the key is authoritative too: the
    // status was deleted, or belongs to another workspace. Contributing no
    // column is the resolved answer, not a pending one.
    if (category && ALL_STATUSES.includes(category)) columns.add(category);
  }
  return { state: "resolved", columns };
}

/**
 * Whether an issue BEHAVES as a given category (MUL-6243).
 *
 * The one question every status-coupled product rule actually asks. Comparing
 * `issue.status` to a built-in key answers it only for a workspace with no
 * custom statuses: a custom status in the `done` category is done, and code
 * that checks `status === "done"` silently disagrees.
 *
 * An unresolved custom key answers `false`, and that direction is deliberate —
 * every caller of this fails safe that way. "Is it done/cancelled?" false keeps
 * a row VISIBLE rather than hiding it; "is it backlog?" false keeps the
 * agent-run confirmation rather than skipping it. Guessing the other way would
 * hide work or start an agent without asking.
 */
export function issueBehavesAs(
  issue: Pick<Issue, "status" | "status_category">,
  category: IssueStatusCategory,
): boolean {
  return issueStatusCategory(issue) === category;
}

/** True when the issue behaves as any of the given categories. */
export function issueBehavesAsAny(
  issue: Pick<Issue, "status" | "status_category">,
  categories: readonly IssueStatusCategory[],
): boolean {
  const resolved = issueStatusCategory(issue);
  return resolved !== null && categories.includes(resolved);
}
