/**
 * Row model for the workspace search screen's single FlatList.
 *
 * Extracted from app/(app)/[workspace]/search.tsx so the ordering rules — in
 * particular the cross-type cancelled demotion (MUL-5824) — are unit-testable
 * without mounting the screen.
 */
import type {
  Issue,
  SearchIssueResult,
  SearchProjectResult,
} from "@multica/core/types";
import { partitionAggregatedSearchResults } from "@multica/core/search/cancelled-rank";

export type RowItem =
  | { kind: "header"; key: string; title: string }
  | { kind: "issue"; key: string; issue: SearchIssueResult; query: string }
  | { kind: "project"; key: string; project: SearchProjectResult; query: string }
  | { kind: "recent"; key: string; issue: Issue };

/**
 * Builds the flat row list. Empty query → the Recent section; otherwise the
 * search results in cancelled-partition order:
 *
 *   Projects (live) → Issues (live) → Cancelled (projects then issues)
 *
 * Projects and issues come from two independently ranked responses, and this
 * screen renders every project before every issue — so per-type ranking alone
 * let a single cancelled project be the first row of the list. One trailing
 * Cancelled section is the only arrangement in which no cancelled row of either
 * type can precede a live row of the other. Direct hits (exact identifier,
 * number, or title) stay in their live section.
 */
export function buildSearchRows({
  query,
  issues,
  projects,
  recentIssues,
}: {
  query: string;
  issues: SearchIssueResult[];
  projects: SearchProjectResult[];
  recentIssues: Issue[];
}): RowItem[] {
  const trimmedQuery = query.trim();

  if (!trimmedQuery) {
    if (recentIssues.length === 0) return [];
    return [
      { kind: "header", key: "h-recent", title: "Recent" },
      ...recentIssues.map<RowItem>((issue) => ({
        kind: "recent",
        key: `r-${issue.id}`,
        issue,
      })),
    ];
  }

  const parts = partitionAggregatedSearchResults({
    issues,
    projects,
    query: trimmedQuery,
  });

  const rows: RowItem[] = [];
  if (parts.liveProjects.length > 0) {
    rows.push({ kind: "header", key: "h-projects", title: "Projects" });
    for (const project of parts.liveProjects) {
      rows.push({ kind: "project", key: `p-${project.id}`, project, query: trimmedQuery });
    }
  }
  if (parts.liveIssues.length > 0) {
    rows.push({ kind: "header", key: "h-issues", title: "Issues" });
    for (const issue of parts.liveIssues) {
      rows.push({ kind: "issue", key: `i-${issue.id}`, issue, query: trimmedQuery });
    }
  }
  if (parts.hasCancelled) {
    rows.push({ kind: "header", key: "h-cancelled", title: "Cancelled" });
    for (const project of parts.cancelledProjects) {
      rows.push({ kind: "project", key: `p-${project.id}`, project, query: trimmedQuery });
    }
    for (const issue of parts.cancelledIssues) {
      rows.push({ kind: "issue", key: `i-${issue.id}`, issue, query: trimmedQuery });
    }
  }
  return rows;
}
