import type { IssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import type { Issue, IssueStatusCategory } from "@multica/core/types";
import type { IssueGroupModel } from "../components/IssueGroups";
import { issueRowModel } from "./issues";
import type { ActorResolver } from "./people";
import type { StatusPresentation } from "./status";

const CATEGORY_RANK: Record<IssueStatusCategory, number> = { started: 0, unstarted: 1, done: 2, closed: 3 };

/**
 * One group per status, in the order work moves: started, then unstarted, then
 * finished; the catalog's order within a category. Issues keep the order the
 * server listed them in.
 */
export function groupIssuesByStatus(
  issues: readonly Issue[],
  catalog: Pick<IssueStatusCatalog, "statuses" | "categoryOf">,
  status: (key: string) => StatusPresentation,
  resolveActor: ActorResolver,
): IssueGroupModel[] {
  const byStatus = new Map<string, Issue[]>();
  for (const issue of issues) {
    const list = byStatus.get(issue.status);
    if (list) list.push(issue);
    else byStatus.set(issue.status, [issue]);
  }
  const position = new Map(catalog.statuses.map((entry, index) => [entry.key, index]));
  return [...byStatus.entries()]
    .map(([key, list]) => {
      const category = catalog.categoryOf(key);
      const presentation = status(key);
      return {
        key,
        label: presentation.label,
        color: presentation.color,
        icon: presentation.icon,
        finished: category === "done" || category === "closed",
        rank: CATEGORY_RANK[category] ?? 1,
        position: position.get(key) ?? Number.MAX_SAFE_INTEGER,
        issues: list.map((issue) => issueRowModel(issue, status, resolveActor)),
      };
    })
    .sort((a, b) => a.rank - b.rank || a.position - b.position)
    .map(({ rank: _rank, position: _position, ...group }) => group);
}

/** Counts by phase for a project's progress bar. */
export function phaseCounts(
  issues: readonly Pick<Issue, "status">[],
  categoryOf: (key: string) => IssueStatusCategory,
): { done: number; started: number; unstarted: number } {
  const counts = { done: 0, started: 0, unstarted: 0 };
  for (const issue of issues) {
    const category = categoryOf(issue.status);
    if (category === "done") counts.done += 1;
    else if (category === "started") counts.started += 1;
    else if (category === "unstarted") counts.unstarted += 1;
  }
  return counts;
}
