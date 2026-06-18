// FIR-1412: Folders (with sub-folders) for the Skills and Autopilots
// interfaces. One cerebro-namespaced tree per workspace per surface; a
// membership row files an individual skill/autopilot into a folder.

export type EntityFolderKind = "skill" | "autopilot";

export interface EntityFolder {
  id: string;
  workspace_id: string;
  parent_id: string | null;
  kind: EntityFolderKind;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface EntityFolderItem {
  kind: EntityFolderKind;
  item_id: string;
  folder_id: string;
}

export interface CreateEntityFolderInput {
  kind: EntityFolderKind;
  name: string;
  parent_id?: string | null;
}

export interface UpdateEntityFolderInput {
  name?: string;
  parent_id?: string | null;
}

export interface SetEntityFolderItemInput {
  kind: EntityFolderKind;
  item_id: string;
  folder_id: string | null; // null = unfile (move to root)
}
