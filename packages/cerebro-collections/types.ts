// Collections (FIR-1590 → Collections): per-folder access grants. A grant
// gives a grantee (group / member / workspace / agent / runtime) a role on a
// folder; grants cascade down to the folder's descendants. One model serves
// both folder backends, selected by `surface`:
//   'artifact' = Documents/Notes folders (artifact_folder)
//   'entity'   = Autopilots/Skills folders (cerebro_entity_folder)
// Mirrors the REST shape in server/internal/cerebro/foldergrant/handler.go.

export type GrantSurface = "artifact" | "entity";

export type GranteeType = "group" | "member" | "workspace" | "agent" | "runtime";

export type GrantRole = "viewer" | "editor" | "full_access";

// One resolved grant row. In the "Valgt her" (direct) view every row has
// is_direct=true and source_folder_id === folder_id. In the "Arvet" (effective)
// view rows may be inherited from an ancestor: is_direct=false and
// source_folder_id points at the ancestor the grant was set on.
export interface FolderGrant {
  surface: GrantSurface;
  folder_id: string;
  grantee_type: GranteeType;
  grantee_id: string | null; // null only for grantee_type 'workspace'
  role: GrantRole;
  source_folder_id: string;
  is_direct: boolean;
  depth: number;
}

export interface UpsertFolderGrantInput {
  surface: GrantSurface;
  folder_id: string;
  grantee_type: GranteeType;
  grantee_id: string | null; // null only for grantee_type 'workspace'
  role: GrantRole;
}

export interface RemoveFolderGrantInput {
  surface: GrantSurface;
  folder_id: string;
  grantee_type: GranteeType;
  grantee_id: string | null;
}

// Per-project access grants (FIR-2125). Mirrors FolderGrant but wired to the
// project tree (project_nesting) instead of folder trees.
// Backend: server/internal/cerebro/projectgrant/handler.go
export interface ProjectGrant {
  project_id: string;
  grantee_type: GranteeType;
  grantee_id: string | null;
  role: GrantRole;
  source_project_id: string;
  is_direct: boolean;
  depth: number;
}

export interface UpsertProjectGrantInput {
  project_id: string;
  grantee_type: GranteeType;
  grantee_id: string | null;
  role: GrantRole;
}

export interface RemoveProjectGrantInput {
  project_id: string;
  grantee_type: GranteeType;
  grantee_id: string | null;
}
