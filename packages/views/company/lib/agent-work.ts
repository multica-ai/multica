import type { Issue, IssuePriority, IssueStatusCategory } from "@multica/core/types";

/** The sections of an agent's work list, in the order they are shown. */
export const WORK_GROUPS = ["active", "next", "assignable", "done"] as const;
export type WorkGroup = (typeof WORK_GROUPS)[number];

/** How many finished issues are listed; the rest are a click away in the issue list. */
export const DONE_LIMIT = 20;

const PRIORITY_RANK: Record<IssuePriority, number> = { urgent: 0, high: 1, medium: 2, low: 3, none: 4 };

const rank = (issue: Issue) => PRIORITY_RANK[issue.priority] ?? PRIORITY_RANK.none;

/** Issue order inside a section: most urgent first, then oldest first. */
const byPriority = (a: Issue, b: Issue) => rank(a) - rank(b) || a.created_at.localeCompare(b.created_at);

/** Finished issues: the most recently touched first. */
const byRecent = (a: Issue, b: Issue) => b.updated_at.localeCompare(a.updated_at);

const matches = (issue: Issue, query: string) =>
  issue.title.toLowerCase().includes(query) || issue.identifier.toLowerCase().includes(query);

/**
 * Sorts the project's issues into what an admin asks about one agent:
 * what it is doing (`active`), what is queued for it (`next`), what it could be
 * given (`assignable`: work nobody has started, unassigned first) and what it has
 * finished (`done`). Status categories decide the section, so custom statuses fall
 * where their category says.
 */
export function groupAgentWork(
  issues: readonly Issue[],
  agentId: string,
  categoryOf: (status: string) => IssueStatusCategory | undefined,
  query = "",
): Record<WorkGroup, Issue[]> {
  const needle = query.trim().toLowerCase();
  const groups: Record<WorkGroup, Issue[]> = { active: [], next: [], assignable: [], done: [] };
  for (const issue of issues) {
    if (needle && !matches(issue, needle)) continue;
    const category = categoryOf(issue.status);
    const mine = issue.assignee_type === "agent" && issue.assignee_id === agentId;
    if (mine) {
      if (category === "started") groups.active.push(issue);
      else if (category === "done" || category === "closed") groups.done.push(issue);
      else groups.next.push(issue);
    } else if (category === "unstarted" || category === undefined) {
      groups.assignable.push(issue);
    }
  }
  groups.active.sort(byPriority);
  groups.next.sort(byPriority);
  groups.assignable.sort((a, b) => Number(!!a.assignee_id) - Number(!!b.assignee_id) || byPriority(a, b));
  groups.done.sort(byRecent);
  return groups;
}
