import type { Issue } from "@multica/core/types";
import { issueColumnCategory } from "@multica/core/issues";
import { PRIORITY_ORDER, STATUS_ORDER } from "@multica/core/issues/config";
import type { SortField, SortDirection } from "@multica/core/issues/stores/view-store";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";

const PRIORITY_RANK: Record<string, number> = Object.fromEntries(
  PRIORITY_ORDER.map((p, i) => [p, i])
);
const STATUS_RANK: Record<string, number> = Object.fromEntries(
  STATUS_ORDER.map((status, index) => [status, index]),
);

function compareOptionalDate(
  a: string | null | undefined,
  b: string | null | undefined,
  direction: SortDirection,
): number {
  if (!a && !b) return 0;
  if (!a) return 1;
  if (!b) return -1;
  const dir = direction === "desc" ? -1 : 1;
  return dir * (new Date(a).getTime() - new Date(b).getTime());
}

export function sortIssues(
  issues: Issue[],
  field: SortField,
  direction: SortDirection
): Issue[] {
  // `property:<id>` sorts by the custom-property value. Number values sort
  // numerically; date values are date-only "YYYY-MM-DD" strings, which sort
  // correctly lexically. Direction applies to the VALUE comparison only —
  // issues without a value sort last in both directions (a whole-array
  // reverse would flip them to the front on desc).
  const propertyId = propertyIdFromViewKey(field);
  if (propertyId) {
    const dir = direction === "desc" ? -1 : 1;
    return issues.toSorted((a, b) => {
      const av = a.properties?.[propertyId];
      const bv = b.properties?.[propertyId];
      const aMissing = av === undefined || Array.isArray(av);
      const bMissing = bv === undefined || Array.isArray(bv);
      if (aMissing && bMissing) return 0;
      if (aMissing) return 1;
      if (bMissing) return -1;
      if (typeof av === "number" && typeof bv === "number") return dir * (av - bv);
      return dir * String(av).localeCompare(String(bv));
    });
  }

  const dir = direction === "desc" ? -1 : 1;
  return issues.toSorted((a, b) => {
    switch (field) {
      case "priority":
        return dir * (
          (PRIORITY_RANK[a.priority] ?? 99) -
          (PRIORITY_RANK[b.priority] ?? 99)
        );
      case "status":
        return dir * (
          (STATUS_RANK[issueColumnCategory(a)] ?? STATUS_ORDER.length) -
          (STATUS_RANK[issueColumnCategory(b)] ?? STATUS_ORDER.length)
        );
      case "start_date":
        return compareOptionalDate(a.start_date, b.start_date, direction);
      case "due_date":
        return compareOptionalDate(a.due_date, b.due_date, direction);
      case "created_at":
        return dir * (
          new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
        );
      case "updated_at":
        return dir * (
          new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime()
        );
      case "title":
        return dir * a.title.localeCompare(b.title);
      case "position":
      default:
        // Manual order is user-defined and always ascending. The server also
        // ignores direction for this field.
        return a.position - b.position;
    }
  });
}
