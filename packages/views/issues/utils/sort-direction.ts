import type {
  SortDirection,
  SortField,
} from "@multica/core/issues/stores/view-store";

export type SortDirectionLabelKey =
  | "ascending_title"
  | "descending_title"
  | "newest_first"
  | "oldest_first"
  | "highest_priority_first"
  | "lowest_priority_first"
  | "earliest_first"
  | "latest_first"
  | "workflow_order"
  | "reverse_workflow_order"
  | "alphabetical_order"
  | "reverse_alphabetical_order";

/** Human meaning for a direction, rather than an unexplained arrow. */
export function sortDirectionLabelKey(
  field: SortField,
  direction: SortDirection,
): SortDirectionLabelKey {
  if (field === "created_at" || field === "updated_at") {
    return direction === "desc" ? "newest_first" : "oldest_first";
  }
  if (field === "start_date" || field === "due_date") {
    return direction === "asc" ? "earliest_first" : "latest_first";
  }
  if (field === "priority") {
    return direction === "asc"
      ? "highest_priority_first"
      : "lowest_priority_first";
  }
  if (field === "status") {
    return direction === "asc" ? "workflow_order" : "reverse_workflow_order";
  }
  if (field === "title") {
    return direction === "asc"
      ? "alphabetical_order"
      : "reverse_alphabetical_order";
  }
  return direction === "asc" ? "ascending_title" : "descending_title";
}
