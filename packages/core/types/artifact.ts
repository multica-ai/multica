export type ArtifactKind = "report" | "plan" | "decision" | "diagram" | "note";

export type ArtifactAuthorType = "member" | "agent";

export interface Artifact {
  id: string;
  workspace_id: string;
  project_id: string | null;
  issue_id: string | null;
  kind: ArtifactKind;
  title: string;
  body: string;
  metadata: Record<string, unknown>;
  author_type: ArtifactAuthorType;
  author_id: string;
  created_at: string;
  updated_at: string;
}

export interface CreateArtifactRequest {
  kind: ArtifactKind;
  title: string;
  body: string;
  metadata?: Record<string, unknown>;
  project_id?: string;
  issue_id?: string;
}

export interface UpdateArtifactRequest {
  title?: string;
  body?: string;
  metadata?: Record<string, unknown>;
}

export interface UpdateArtifactScopeRequest {
  project_id?: string | null;
  issue_id?: string | null;
}

export type ArtifactScope = "all" | "workspace" | "project" | "issue";

export interface ListArtifactsParams {
  kind?: ArtifactKind;
  scope?: ArtifactScope;
  q?: string;
  limit?: number;
  offset?: number;
}
