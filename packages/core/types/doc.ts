/**
 * Knowledge base docs (MUL-8370).
 *
 * A doc is a Markdown or full-page HTML document in a tree of nested folders,
 * written by members and agents. Every write is a version (a full snapshot
 * with its author and an optional message). Writes are optimistic: they carry
 * the `revision` the writer read and are refused with 409 `revision_conflict`
 * when someone saved in between.
 */

export type DocFormat = "markdown" | "html";
export type DocAuthorType = "member" | "agent";
export type DocVersionAction = "create" | "edit" | "restore" | "revert";

export interface DocSummary {
  id: string;
  workspace_id: string;
  folder_id: string | null;
  format: DocFormat;
  title: string;
  /** Concurrency counter; bumps on every content write, including coalesced ones. */
  revision: number;
  /** Number of the newest version. */
  version_count: number;
  created_by_type: DocAuthorType;
  created_by_id: string;
  updated_by_type: DocAuthorType;
  updated_by_id: string;
  created_at: string;
  updated_at: string;
  /** Changed by someone else since the caller last opened it. */
  unread: boolean;
}

export interface Doc extends DocSummary {
  content: string;
  project_ids: string[];
  /** When the caller last opened the doc, or null if never. */
  last_seen_at: string | null;
}

export interface DocFolder {
  id: string;
  workspace_id: string;
  parent_id: string | null;
  title: string;
  created_by_type: DocAuthorType;
  created_by_id: string;
  created_at: string;
  updated_at: string;
}

export interface DocTree {
  folders: DocFolder[];
  docs: DocSummary[];
}

export interface DocVersion {
  doc_id: string;
  number: number;
  title: string;
  author_type: DocAuthorType;
  author_id: string;
  message: string;
  action: DocVersionAction;
  /** The version a restore copied or a revert undid. */
  action_ref: number | null;
  task_id: string | null;
  issue_id: string | null;
  lines_added: number;
  lines_removed: number;
  /** When the version (editing session) began. */
  created_at: string;
  /** Last save folded into this version. */
  updated_at: string;
  /** Present only when one version is fetched on its own. */
  content?: string;
}

export interface DocActivityItem {
  doc_id: string;
  doc_title: string;
  doc_folder_id: string | null;
  doc_format: DocFormat;
  number: number;
  author_type: DocAuthorType;
  author_id: string;
  message: string;
  action: DocVersionAction;
  action_ref: number | null;
  task_id: string | null;
  issue_id: string | null;
  lines_added: number;
  lines_removed: number;
  created_at: string;
  updated_at: string;
  unread: boolean;
}

export interface CreateDocRequest {
  title: string;
  content?: string;
  format?: DocFormat;
  folder_id?: string | null;
  message?: string;
}

export interface UpdateDocRequest {
  base_revision: number;
  title?: string;
  content?: string;
  message?: string;
  action?: "edit" | "restore" | "revert";
  action_ref?: number;
}

export interface CreateDocFolderRequest {
  title: string;
  parent_id?: string | null;
}
