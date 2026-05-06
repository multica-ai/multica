// Wire types for the polymorphic memory_artifact substrate.
//
// Design rationale lives in server/migrations/068_memory_artifact.up.sql.
// One discriminator (`kind`) covers wiki pages, agent notes, runbooks,
// and decisions — each kind shares the same search/anchor/archive plumbing
// so the UI doesn't have to know about per-kind silos.

export type MemoryArtifactKind =
  | "wiki_page"
  | "agent_note"
  | "runbook"
  | "decision";

// Polymorphic anchor — what is this artifact about? Mirrors the
// allowedAnchorTypes set in the server handler. New anchor types must
// be registered on both sides.
export type MemoryArtifactAnchorType =
  | "issue"
  | "project"
  | "agent"
  | "channel";

export type MemoryArtifactAuthorType = "member" | "agent";

export interface MemoryArtifact {
  id: string;
  workspace_id: string;
  kind: MemoryArtifactKind;
  parent_id: string | null;
  title: string;
  content: string;
  slug: string | null;
  anchor_type: MemoryArtifactAnchorType | null;
  anchor_id: string | null;
  author_type: MemoryArtifactAuthorType;
  author_id: string;
  tags: string[];
  // Free-form JSON object — server returns "{}" when empty so callers
  // can index without null checks.
  metadata: Record<string, unknown>;
  archived_at: string | null;
  archived_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateMemoryArtifactRequest {
  kind: MemoryArtifactKind;
  title: string;
  content: string;
  parent_id?: string | null;
  slug?: string | null;
  anchor_type?: MemoryArtifactAnchorType | null;
  anchor_id?: string | null;
  tags?: string[];
  metadata?: Record<string, unknown>;
}

// PATCH semantics — every field is independently optional. `null` for
// title/content is rejected by the server; `null` for slug/parent/anchor
// clears the value. Tags replaces the whole array when present.
export interface UpdateMemoryArtifactRequest {
  title?: string;
  content?: string;
  slug?: string | null;
  parent_id?: string | null;
  anchor_type?: MemoryArtifactAnchorType | null;
  anchor_id?: string | null;
  tags?: string[];
  metadata?: Record<string, unknown>;
}

export interface ListMemoryArtifactsParams {
  kind?: MemoryArtifactKind;
  parent_id?: string;
  include_archived?: boolean;
  limit?: number;
  offset?: number;
}

export interface ListMemoryArtifactsResponse {
  memory_artifacts: MemoryArtifact[];
  total: number;
}

export interface SearchMemoryArtifactsParams {
  q: string;
  kind?: MemoryArtifactKind;
  limit?: number;
  offset?: number;
}
