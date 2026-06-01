import type { Issue, ListIssuesParams, ViewFilters } from "../types";

/**
 * Translate a saved view's stored filters into the concrete GET /api/issues
 * request(s) needed to satisfy it.
 *
 * The API is AND-only and cannot express a cross-dimension OR (e.g. "assigned
 * to me OR created by me"). A view models that with `any_of`: the client fires
 * one request per branch, ANDing the outer filters onto each, then dedupes the
 * union by issue id (see dedupeIssuesById). A flat view (no any_of) is a single
 * request. Token expansion ({me} / {my_agents} / {my_squads}) is the server's
 * job — they pass through untouched here.
 */
export function resolveViewRequests(filters: ViewFilters): ListIssuesParams[] {
  const { any_of, ...outer } = filters;
  if (!any_of || any_of.length === 0) {
    return [outer];
  }
  return any_of.map((branch) => ({ ...outer, ...branch }));
}

/**
 * Merge the per-branch result sets of an any_of view into one list, keeping the
 * first occurrence of each issue id and preserving order.
 */
export function dedupeIssuesById(issueArrays: Issue[][]): Issue[] {
  const seen = new Set<string>();
  const out: Issue[] = [];
  for (const arr of issueArrays) {
    for (const issue of arr) {
      if (seen.has(issue.id)) continue;
      seen.add(issue.id);
      out.push(issue);
    }
  }
  return out;
}
