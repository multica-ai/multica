export type PinnedItemType = "issue" | "project";

/**
 * Pin metadata only. Title / status / identifier / icon are NOT here —
 * consumers derive them from `issueDetailOptions` / `projectDetailOptions`
 * so the sidebar reacts to `issue:updated` / `project:updated` events
 * automatically, without needing a cross-entity invalidate on `pinKeys`.
 */
export interface PinnedItem {
  id: string;
  workspace_id: string;
  user_id: string;
  item_type: PinnedItemType;
  item_id: string;
  position: number;
  created_at: string;
}

export interface CreatePinRequest {
  item_type: PinnedItemType;
  item_id: string;
}

export interface ReorderPinsRequest {
  items: { id: string; position: number }[];
}
