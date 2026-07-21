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

/**
 * Resolve a stored selection into concrete catalog ids. Entries that resolve to
 * nothing (a token whose built-in was archived, or an id from another workspace)
 * are dropped, so a stale selection narrows rather than erroring.
 */
export function resolveStatusFilterIds(
  selection: string[],
  catalog: IssueStatusDefinition[],
): string[] {
  if (selection.length === 0 || catalog.length === 0) return [];
  const ids = new Set<string>();
  for (const entry of selection) {
    if (isCatalogId(entry, catalog)) {
      ids.add(entry);
      continue;
    }
    const builtIn = catalog.find((s) => s.system_key === entry && !s.archived);
    if (builtIn) ids.add(builtIn.id);
  }
  return [...ids];
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
