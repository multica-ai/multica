export type DocAuthorType = "member" | "agent";
export interface DocSummary {
  id: string;
  workspaceId: string;
  folderId: string | null;
  title: string;
  revision: number;
  versionCount: number;
  createdByType: DocAuthorType;
  createdById: string;
  updatedByType: DocAuthorType;
  updatedById: string;
  createdAt: string;
  updatedAt: string;
}
/**
 * A comment in a discussion on a doc's text. The discussion's first comment
 * (parentId null) carries the anchor: the quoted text and up to 64 characters
 * of context on each side, re-located by text in the rendered doc.
 */
export interface DocComment {
  id: string;
  docId: string;
  parentId: string | null;
  authorType: DocAuthorType;
  authorId: string;
  content: string;
  anchorQuote: string;
  anchorPrefix: string;
  anchorSuffix: string;
  resolvedAt: string | null;
  resolvedByType: DocAuthorType | null;
  resolvedById: string | null;
  createdAt: string;
  updatedAt: string;
}
export interface CreateDocCommentRequest {
  content: string;
  parentId?: string;
  anchorQuote?: string;
  anchorPrefix?: string;
  anchorSuffix?: string;
}
/** A doc search hit: where the query matched, with an excerpt for content hits. */
export interface SearchDocResult extends DocSummary {
  matchSource: "title" | "content";
  matchedSnippet: string | null;
}
export interface SearchDocsResponse {
  docs: SearchDocResult[];
}
export interface Doc extends DocSummary {
  content: string;
}
export interface DocFolder {
  id: string;
  workspaceId: string;
  parentId: string | null;
  title: string;
  createdByType: DocAuthorType;
  createdById: string;
  createdAt: string;
  updatedAt: string;
}
export interface DocTree {
  folders: DocFolder[];
  docs: DocSummary[];
}
/** An agent's run on a doc discussion, as the discussion shows it. */
export interface DocTaskRun {
  id: string;
  agentId: string;
  docThreadId: string;
  /** Open wire value; the task statuses (queued, running, failed, ...). */
  status: string;
  failureReason: string;
  /** Only for viewers who can see the agent. */
  error: string | null;
  cancelledBy?: { type: string; id?: string; name?: string };
  createdAt: string;
  completedAt: string | null;
  /** The viewer can see the agent and the run is still active. */
  canCancel: boolean;
}
export interface DocVersion {
  docId: string;
  number: number;
  title: string;
  authorType: DocAuthorType;
  authorId: string;
  message: string;
  action: "create" | "edit" | "restore";
  actionRef: number | null;
  taskId: string | null;
  issueId: string | null;
  /** The discussion an agent was answering when it saved this version, with
   *  the text that discussion is about; null otherwise or once deleted. */
  docThreadId: string | null;
  docThreadQuote: string | null;
  /** Everyone whose edits the version carries. Empty for versions saved
   *  before live co-editing; the author is the one then. */
  contributors: { type: DocAuthorType; id: string }[];
  createdAt: string;
  updatedAt: string;
  content?: string;
}
/** Lets an editor join a doc's live session; `enabled: false` when the
 *  server runs without the collaboration service. */
export type DocCollabTicket =
  | {
      enabled: true;
      token: string;
      document: string;
      expiresAt: string;
      /** Base64 Yjs state saved with the doc's current revision, when asked
       *  for and the live session would load it unchanged. */
      state?: string;
    }
  | { enabled: false };
export interface CreateDocRequest {
  title: string;
  content?: string;
  folderId?: string | null;
  message?: string;
}
export interface UpdateDocRequest {
  baseRevision: number;
  title?: string;
  content?: string;
  message?: string;
  action?: "edit" | "restore";
  actionRef?: number;
}
export interface CreateDocFolderRequest {
  title: string;
  parentId?: string | null;
}
