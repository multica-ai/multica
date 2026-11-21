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
  createdAt: string;
  updatedAt: string;
  content?: string;
}
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
