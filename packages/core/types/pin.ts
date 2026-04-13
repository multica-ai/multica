export type PinnedItemType = "issue" | "project";

export interface PinnedItem {
  id: string;
  workspace_id: string;
  user_id: string;
  item_type: PinnedItemType;
  item_id: string;
  position: number;
  created_at: string;
  title: string;
  identifier?: string;
  icon?: string;
  status?: string;
}

export interface CreatePinRequest {
  item_type: PinnedItemType;
  item_id: string;
}

export interface ReorderPinsRequest {
  items: { id: string; position: number }[];
}
