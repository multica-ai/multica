// CEREBRO-PATCH(core-types-pin): cerebro modification of upstream file
export type PinnedItemType = "issue" | "project" | "chat_session";

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
