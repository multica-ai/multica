// Workspace document types — matches the server schema from PRD §5.1

export type DocumentAuthorType = "human" | "agent_foreground" | "agent_background" | "import";

export type DocumentOperation = "create" | "edit" | "rename" | "restore" | "tag" | "pin" | "archive";

export type IssueLinkType = "referenced" | "produced" | "consumed";

export interface WorkspaceDocument {
  id: string;
  workspace_id: string;
  path: string;
  title: string | null;
  description: string | null;
  content: string;
  format: string;
  tags: string[];
  pinned: boolean;
  archived_at: string | null;
  current_revision_id: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface DocumentIndexEntry {
  id: string;
  path: string;
  description: string | null;
  pinned: boolean;
}

export interface DocumentRevision {
  id: string;
  document_id: string;
  revision_number: number;
  parent_revision: string | null;
  title: string | null;
  description: string | null;
  content: string;
  tags: string[];
  author_type: DocumentAuthorType;
  author_id: string | null;
  task_id: string | null;
  operation: DocumentOperation;
  change_summary: string | null;
  created_at: string;
}

/** Compact revision entry returned by the history list endpoint. */
export interface DocumentRevisionSummary {
  id: string;
  revision_number: number;
  author_type: DocumentAuthorType;
  author_id: string | null;
  task_id: string | null;
  operation: DocumentOperation;
  change_summary: string | null;
  created_at: string;
}

export interface IssueDocumentLink {
  issue_id: string;
  document_id: string;
  link_type: IssueLinkType;
  created_at: string;
}

// Request types

export interface UpsertDocumentRequest {
  title?: string;
  description?: string;
  content: string;
  tags?: string[];
  base_revision_id?: string;
  change_summary?: string;
  force_new_revision?: boolean;
}

export interface PatchDocumentRequest {
  find: string;
  replace: string;
  change_summary?: string;
}

export interface RenameDocumentRequest {
  new_path: string;
}

export interface UpdateDocumentTagsRequest {
  add?: string[];
  remove?: string[];
}

export interface RestoreDocumentRequest {
  revision_number: number;
}

export interface LinkIssueDocumentRequest {
  document_id?: string;
  path?: string;
  link_type: IssueLinkType;
}

// List params

export interface ListDocumentsParams {
  path_prefix?: string;
  tag?: string;
  pinned?: boolean;
}
