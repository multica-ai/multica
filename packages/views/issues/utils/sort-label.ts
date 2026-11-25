import type { StaticSortField } from "@multica/core/issues/stores/view-store";

/** The `display.*` string that names each sort field. */
export const SORT_LABEL_KEY = {
  position: "sort_manual",
  status: "sort_status",
  priority: "sort_priority",
  start_date: "sort_start_date",
  due_date: "sort_due_date",
  created_at: "sort_created",
  updated_at: "sort_updated",
  title: "sort_title",
} as const satisfies Record<StaticSortField, string>;
