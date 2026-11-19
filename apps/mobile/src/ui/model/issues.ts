import type { Issue, IssueStatusCategory } from "@multica/core/types";
import type { IssueRowModel } from "../components/IssueRow";
import type { ActorResolver } from "./people";
import type { StatusPresentation } from "./status";

const CATEGORY_ORDER: Record<IssueStatusCategory, number> = { started: 0, unstarted: 1, done: 2, closed: 3 };

export function issueRowModel(issue: Issue, status: (key: string) => StatusPresentation, resolveActor: ActorResolver): IssueRowModel {
  const presentation = status(issue.status);
  return {
    id: issue.id,
    identifier: issue.identifier,
    title: issue.title,
    status: { key: issue.status, color: presentation.color, icon: presentation.icon },
    assignee: issue.assignee_type && issue.assignee_id ? resolveActor(issue.assignee_type, issue.assignee_id) : null,
  };
}

/** Work in flight first, then what's next, then what's finished; stable within a phase. */
export function sortByPhase<T>(items: readonly T[], categoryOf: (item: T) => IssueStatusCategory): T[] {
  return items
    .map((item, index) => ({ item, index, rank: CATEGORY_ORDER[categoryOf(item)] ?? 1 }))
    .sort((a, b) => a.rank - b.rank || a.index - b.index)
    .map(({ item }) => item);
}
