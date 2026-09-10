import type { Issue, IssueStatus, IssueStatusCategory } from "../types";
import {
  compareIssueStatusEntries,
  isBuiltInIssueStatus,
  normalizeIssueStatusCategory,
} from "../issue-statuses";
import type { IssueStatusCatalog } from "../issue-statuses";
import { ALL_STATUSES, BUILT_IN_STATUS_CATEGORY, BUILT_IN_STATUS_ORDER } from "./config";

/**
 * The internal lifecycle category used for behavior and cache predicates.
 * User-facing status columns are keyed by issue.status, not this value.
 *
 * Pure on purpose: the cache helpers that call it run outside React and must
 * not reach for a catalog. It reads the server-provided `status_category` when
 * present and otherwise falls back to the fixed built-in key mapping.
 *
 * Returns null when the status is a custom key this response did not resolve,
 * so callers do not infer an unsupported lifecycle.
 */
export function issueStatusCategory(
  issue: Pick<Issue, "status" | "status_category">,
): IssueStatusCategory | null {
  const fromServer = issue.status_category;
  const normalized = fromServer ? normalizeIssueStatusCategory(fromServer) : null;
  if (normalized) return normalized;
  if (isBuiltInIssueStatus(issue.status)) return BUILT_IN_STATUS_CATEGORY[issue.status];
  return null;
}

/**
 * Category for a bare status KEY, for render paths that hold only the string.
 *
 * Exact for the seven built-ins and the four categories. A custom key returns
 * `unstarted` so presentation lookups always resolve to something renderable;
 * surfaces that must show the real status use the catalog
 * (`useIssueStatuses`) instead. (MUL-6243)
 */
export function statusCategoryOfKey(statusKey: string): IssueStatusCategory {
  if (isBuiltInIssueStatus(statusKey)) return BUILT_IN_STATUS_CATEGORY[statusKey];
  return normalizeIssueStatusCategory(statusKey) ?? "unstarted";
}

/** Lifecycle fallback for legacy category queries and presentation tokens.
 * Do not use this as the key of a user-facing status column. */
export function issueColumnCategory(
  issue: Pick<Issue, "status" | "status_category">,
): IssueStatusCategory {
  return issueStatusCategory(issue) ?? statusCategoryOfKey(issue.status);
}

/**
 * Rewrites a patch's `status_category` to match its `status`, before the patch
 * reaches any cache (MUL-6243).
 *
 * The server now sends a category on every issue, so a cached entity looks like
 * `{status: "todo", status_category: "unstarted"}`. An optimistic patch carries only
 * `{status: "done"}`, and a bare `{...issue, ...patch}` therefore keeps the
 * stale category while the card moves to the completed bucket. A
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
 * Three states, not two. Built-in keys resolve with no catalog at all. A CUSTOM
 * key does not — and `categoryOf` answers `unstarted` for anything it has not
 * loaded, which is indistinguishable from a real value. Routing on that guess
 * paged the unstarted column for a saved `qa` filter while the query still
 * restricted `status=qa`.
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
  | { state: "resolved"; columns: Set<IssueStatus> }
  | { state: "pending" }
  | { state: "error" };

export function statusFilterColumns(
  statusFilters: readonly string[],
  catalog: Pick<IssueStatusCatalog, "entryOf" | "isLoaded" | "isPending" | "isError">,
): StatusFilterColumnsResult {
  const columns = new Set<IssueStatus>();
  for (const key of statusFilters) {
    if (isBuiltInIssueStatus(key)) {
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
    const normalized = category ? normalizeIssueStatusCategory(category) : null;
    if (normalized) columns.add(key);
  }
  return { state: "resolved", columns };
}

/** Ordered concrete columns. Archived statuses remain readable for existing work. */
export function statusColumnKeys(
  catalog: Pick<IssueStatusCatalog, "statuses">,
): IssueStatus[] {
  const entries = [...catalog.statuses].sort(compareIssueStatusEntries);
  return ALL_STATUSES.flatMap((category) => [
    ...BUILT_IN_STATUS_ORDER.filter((key) => BUILT_IN_STATUS_CATEGORY[key] === category),
    ...entries.filter((entry) =>
      !isBuiltInIssueStatus(entry.key) &&
      normalizeIssueStatusCategory(entry.category) === category,
    ).map((entry) => entry.key),
  ]);
}

/** Exact-key filters override hidden-column preferences, never merge siblings. */
export function visibleStatusKeys(
  statusFilters: readonly string[],
  hiddenStatuses: readonly IssueStatus[],
  catalog: Pick<IssueStatusCatalog, "statuses" | "entryOf" | "isLoaded" | "isPending" | "isError">,
): IssueStatus[] {
  const resolved = statusFilters.length > 0 ? statusFilterColumns(statusFilters, catalog) : null;
  const selected = resolved?.state === "resolved" ? resolved.columns : null;
  return statusColumnKeys(catalog).filter((key) =>
    selected !== null ? selected.has(key) : !hiddenStatuses.includes(key),
  );
}

/**
 * Whether an issue belongs to a given lifecycle category (MUL-6243).
 *
 * Comparing `issue.status` to a built-in key answers lifecycle questions only
 * for a workspace with no custom statuses: a custom status in `done` is
 * completed, and code that checks `status === "done"` silently disagrees.
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
