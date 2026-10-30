/**
 * Mirrors web's exact status grouping. Category only orders sections; it never
 * merges them. Mobile groups its already-loaded rows and keeps unknown keys
 * visible while the catalog is loading.
 */
import type { Issue, IssueStatus, IssueStatusEntry } from "@multica/core/types";
import { BUILT_IN_STATUS_ORDER, BUILT_IN_STATUS_CATEGORY, STATUS_CATEGORIES, issueStatusCategory } from "./issue-status";

export interface IssueSection {
  status: IssueStatus;
  data: Issue[];
}

export function groupIssuesByStatus(issues: Issue[], entries: IssueStatusEntry[] = []): IssueSection[] {
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
  return [...byStatus].sort(([a], [b]) => {
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
