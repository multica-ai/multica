/**
 * Concrete statuses remain separate, including unknown keys while the catalog
 * loads. Default ordering follows the catalog; the workspace list can select
 * the requested workflow-stage order.
 */
import type { Issue, IssueStatus, IssueStatusEntry } from "@multica/core/types";
import { BUILT_IN_STATUS_ORDER, BUILT_IN_STATUS_CATEGORY, STATUS_CATEGORIES, issueStatusCategory } from "./issue-status";

export interface IssueSection {
  status: IssueStatus;
  data: Issue[];
}

// Accept the requested stage names as catalog names/keys without renaming or
// merging concrete statuses. Other custom statuses stay in their lifecycle.
const WORKFLOW_ORDER = new Map([
  ["backlog", 0], ["plan", 0], ["todo", 1], ["inprogress", 2], ["inprocessing", 2],
  ["inreview", 3], ["inreviewing", 3], ["blocked", 4], ["done", 5], ["cancelled", 6],
]);
const normalizeStage = (value: string) => value.toLowerCase().replace(/[\s_-]+/g, "");

export function groupIssuesByStatus(
  issues: Issue[],
  entries: IssueStatusEntry[] = [],
  order: "catalog" | "workflow" = "catalog",
): IssueSection[] {
  const byStatus = new Map<IssueStatus, Issue[]>();
  for (const issue of issues) {
    const list = byStatus.get(issue.status);
    if (list) list.push(issue);
    else byStatus.set(issue.status, [issue]);
  }
  const entryByKey = new Map(entries.map((entry) => [entry.key, entry]));
  const rank = (key: string) => STATUS_CATEGORIES.indexOf(
    issueStatusCategory({ status: key, status_category: entryByKey.get(key)?.category ?? byStatus.get(key)?.[0]?.status_category }) ?? "unstarted",
  );
  const workflowRank = (key: string) =>
    WORKFLOW_ORDER.get(normalizeStage(key)) ??
    WORKFLOW_ORDER.get(normalizeStage(entryByKey.get(key)?.name ?? "")) ??
    [0.5, 2.5, 5.5, 6.5][rank(key)];
  return [...byStatus].sort(([a], [b]) => {
    if (order === "workflow") {
      const workflow = workflowRank(a) - workflowRank(b);
      if (workflow) return workflow;
    }
    const categoryRank = rank(a) - rank(b);
    if (categoryRank) return categoryRank;
    const aBuilt = BUILT_IN_STATUS_ORDER.indexOf(a as keyof typeof BUILT_IN_STATUS_CATEGORY);
    const bBuilt = BUILT_IN_STATUS_ORDER.indexOf(b as keyof typeof BUILT_IN_STATUS_CATEGORY);
    const position = (entryByKey.get(a)?.position ?? 0) - (entryByKey.get(b)?.position ?? 0);
    if (position) return position;
    if (aBuilt !== -1 || bBuilt !== -1) return (aBuilt === -1 ? 99 : aBuilt) - (bBuilt === -1 ? 99 : bBuilt);
    return a.localeCompare(b);
  }).map(([status, data]) => ({ status, data }));
}
