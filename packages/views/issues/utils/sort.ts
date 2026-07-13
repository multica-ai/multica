import type { Issue } from "@multica/core/types";
import { PRIORITY_ORDER } from "@multica/core/issues/config";
import type { SortField, SortDirection } from "@multica/core/issues/stores/view-store";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";

const PRIORITY_RANK: Record<string, number> = Object.fromEntries(
  PRIORITY_ORDER.map((p, i) => [p, i])
);

export function sortIssues(
  issues: Issue[],
  field: SortField,
  direction: SortDirection
): Issue[] {
  // `property:<id>` sorts by the custom-property value. Number values sort
  // numerically; date values are date-only "YYYY-MM-DD" strings, which sort
  // correctly lexically. Issues without a value always sort last regardless
  // of direction (matching start_date/due_date semantics).
  const propertyId = propertyIdFromViewKey(field);
  if (propertyId) {
    const sorted = issues.toSorted((a, b) => comparePropertyValues(a, b, propertyId));
    return direction === "desc" ? sorted.reverse() : sorted;
  }

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
  return direction === "desc" ? sorted.reverse() : sorted;
}

function comparePropertyValues(a: Issue, b: Issue, propertyId: string): number {
  const av = a.properties?.[propertyId];
  const bv = b.properties?.[propertyId];
  const aMissing = av === undefined || Array.isArray(av);
  const bMissing = bv === undefined || Array.isArray(bv);
  if (aMissing && bMissing) return 0;
  if (aMissing) return 1;
  if (bMissing) return -1;
  if (typeof av === "number" && typeof bv === "number") return av - bv;
  return String(av).localeCompare(String(bv));
}
