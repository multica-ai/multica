import type { Issue } from "@multica/core/types";
import { PRIORITY_ORDER } from "@multica/core/issues/config";
import type { SortField, SortDirection } from "@multica/core/issues/stores/view-store";

const PRIORITY_RANK: Record<string, number> = Object.fromEntries(
  PRIORITY_ORDER.map((p, i) => [p, i])
);

export function sortIssues(
  issues: Issue[],
  field: SortField,
  direction: SortDirection
): Issue[] {
  const sorted = issues.toSorted((a, b) => {
    switch (field) {
      case "priority":
        return (
          (PRIORITY_RANK[a.priority] ?? 99) -
          (PRIORITY_RANK[b.priority] ?? 99)
        );
      case "start_date": {
        if (!a.start_date && !b.start_date) return 0;
        if (!a.start_date) return 1;
        if (!b.start_date) return -1;
        return (
          new Date(a.start_date).getTime() - new Date(b.start_date).getTime()
        );
      }
      case "due_date": {
        if (!a.due_date && !b.due_date) return 0;
        if (!a.due_date) return 1;
        if (!b.due_date) return -1;
        return (
          new Date(a.due_date).getTime() - new Date(b.due_date).getTime()
        );
      }
      case "created_at":
        return (
          new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
        );
      case "title":
        return a.title.localeCompare(b.title);
      case "position":
      default:
        return a.position - b.position;
    }
  });
  // `position` (manual order) is directionless by contract: the page query
  // sends sort_direction=undefined for it, and the header hides the direction
  // toggle in manual mode. A stale "desc" left over from a prior field-sort
  // must not reverse the manual order, so never apply direction to position.
  if (field === "position") return sorted;
  return direction === "desc" ? sorted.reverse() : sorted;
}
