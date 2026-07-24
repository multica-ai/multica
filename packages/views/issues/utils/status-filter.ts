import type { Issue, IssueStatusDefinition } from "@multica/core/types";

/**
 * Status filter selections are persisted (localStorage, via the view store), and
 * they used to hold the 7 legacy status tokens. Catalog-driven filtering selects
 * by catalog id instead, so a stored selection can contain BOTH shapes:
 *
 *   - a catalog id, written by this build;
 *   - a legacy token ("todo", "in_review", …), left by an older build.
 *
 * Rather than rewriting storage — which would silently reinterpret a user's
 * saved view — both are accepted and resolved at query time (MUL-4809). A legacy
 * token maps to the BUILT-IN status carrying that system_key, i.e. exactly the
 * lane the user had selected before; custom statuses are not folded in, because
 * the user never chose them.
 */

/** True when the entry is a catalog id rather than a legacy status token. */
function isCatalogId(entry: string, catalog: IssueStatusDefinition[]): boolean {
  return catalog.some((s) => s.id === entry);
}

/** Shape test used only when the catalog has not loaded yet (cold start). */
const CATALOG_ID_SHAPE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Resolve a stored selection into concrete catalog ids. Entries that resolve to
 * nothing (a token whose built-in was archived, or an id from another workspace)
 * are dropped, so a stale selection narrows rather than erroring.
 */
export function resolveStatusFilterIds(
  selection: string[],
  catalog: IssueStatusDefinition[],
): string[] {
  // NB: an empty catalog must NOT short-circuit — the cold-start passthrough
  // below is exactly the case where the catalog has not loaded yet.
  if (selection.length === 0) return [];
  const ids = new Set<string>();
  for (const entry of selection) {
    if (isCatalogId(entry, catalog)) {
      ids.add(entry);
      continue;
    }
    const builtIn = catalog.find((s) => s.system_key === entry && !s.archived);
    if (builtIn) {
      ids.add(builtIn.id);
      continue;
    }
    // Cold start: the catalog query has not settled yet, so nothing above can
    // match. A catalog-id-shaped entry is already what the server wants — pass
    // it straight through as `status_ids`. Without this the caller falls back
    // to the legacy `statuses` facet and the first request after a refresh
    // sends UUIDs into a 7-token enum, which the server rejects with 400.
    if (CATALOG_ID_SHAPE.test(entry)) ids.add(entry);
  }
  return [...ids];
}

/**
 * Project a stored selection onto the 7 legacy status tokens — the lanes the
 * List and status-grouped Board fetch as server branches (MUL-4809).
 *
 * The filter menu writes a catalog id for BOTH built-in and custom statuses, so
 * testing the raw selection against the legacy tokens matches nothing and the
 * surface renders zero rows. A catalog id maps to the token its status projects
 * to (system_key for a built-in, Category for a custom status); an entry that is
 * already a legacy token is kept. Entries that resolve to neither — a catalog id
 * while the catalog is still loading — are dropped, and the caller then falls
 * back to every lane: the branch queries carry `status_ids`, so the rows stay
 * correct and only some lanes render empty until the catalog settles.
 */
export function resolveStatusFilterTokens(
  selection: string[],
  catalog: IssueStatusDefinition[],
): string[] {
  if (selection.length === 0) return [];
  const tokens = new Set<string>();
  for (const entry of selection) {
    const status = catalog.find((s) => s.id === entry);
    if (status) {
      tokens.add(status.system_key ?? status.category);
      continue;
    }
    if (!CATALOG_ID_SHAPE.test(entry)) tokens.add(entry);
  }
  return [...tokens];
}

/**
 * Client-side predicate mirroring the server's `status_ids` facet. Falls back to
 * the legacy token comparison for issues that have no `status_id` yet (an
 * unseeded workspace, or a server predating the catalog).
 */
export function issueMatchesStatusFilter(
  issue: Pick<Issue, "status" | "status_id">,
  selection: string[],
  catalog: IssueStatusDefinition[],
): boolean {
  if (selection.length === 0) return true;
  if (issue.status_id) {
    const ids = resolveStatusFilterIds(selection, catalog);
    // An unloaded catalog resolves to nothing; don't hide everything while it
    // is in flight — fall through to the legacy comparison below.
    if (ids.length > 0) return ids.includes(issue.status_id);
  }
  return selection.includes(issue.status);
}
