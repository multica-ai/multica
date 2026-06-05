// CEREBRO-PATCH(cerebro-focus-list-types): TECH-2947 — personal focus list.
export interface FocusListItem {
  id: string;
  text: string;
  issue_id: string | null;
  snoozed_until: string | null;
  done_at: string | null;
  position: number;
  created_at: string;
  updated_at: string;
}
