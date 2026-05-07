// CEREBRO-PATCH(core-types-artifact): cerebro modification of upstream file
export type ArtifactKind = "report" | "plan" | "decision" | "diagram" | "note";

export type ArtifactFormat = "md" | "html" | "pdf";

export type ArtifactAuthorType = "member" | "agent";

export interface Artifact {
  id: string;
  workspace_id: string;
  project_id: string | null;
  issue_id: string | null;
  folder_id: string | null;
  origin_issue_id: string | null;
  kind: ArtifactKind;
  format: ArtifactFormat;
  title: string;
  body: string;
  file_url: string | null;
  file_size_bytes: number | null;
  metadata: Record<string, unknown>;
  author_type: ArtifactAuthorType;
  author_id: string;
  requester_user_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateArtifactRequest {
  kind: ArtifactKind;
  format?: ArtifactFormat;
  title: string;
  body: string;
  file_url?: string;
  file_size_bytes?: number;
  metadata?: Record<string, unknown>;
  project_id?: string;
  issue_id?: string;
  folder_id?: string;
  origin_issue_id?: string;
  requester_user_id?: string;
}

export interface UpdateArtifactRequest {
  title?: string;
  body?: string;
  file_url?: string | null;
  file_size_bytes?: number | null;
  metadata?: Record<string, unknown>;
}

export interface UpdateArtifactScopeRequest {
  project_id?: string | null;
  issue_id?: string | null;
}

export interface MoveArtifactToFolderRequest {
  folder_id: string | null;
}

export type ArtifactScope = "all" | "workspace" | "project" | "issue";

export interface ListArtifactsParams {
  kind?: ArtifactKind;
  scope?: ArtifactScope;
  author_type?: "all" | ArtifactAuthorType;
  author_id?: string;
  origin_issue_id?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export interface ArtifactFolder {
  id: string;
  workspace_id: string;
  parent_id: string | null;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface CreateArtifactFolderRequest {
  name: string;
  parent_id?: string | null;
}

export interface UpdateArtifactFolderRequest {
  name?: string;
  parent_id?: string | null;
}

export interface ArtifactUploadResponse {
  url: string;
  filename: string;
  content_type: string;
  size_bytes: number;
}
