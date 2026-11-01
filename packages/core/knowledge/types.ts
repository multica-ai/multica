/**
 * Company knowledge: the Knowledge API's own shapes. The server adapts the
 * engine behind it, so nothing here mentions the engine.
 *
 * `unknown` stands in for a value a newer server may add; views treat it as
 * read-only and never as success.
 */

/** The workspace's knowledge state. `offline` means enabled but not answering. */
export type KnowledgeState =
  | "disabled"
  | "provisioning"
  | "ready"
  | "failed"
  | "offline"
  | "unknown";

/** `draft` is stored but not searchable until an owner or admin publishes it. */
export type KnowledgeDocumentState = "draft" | "published" | "failed" | "unknown";

export type KnowledgeVisibility = "workspace" | "restricted" | "unknown";

export interface KnowledgeBase {
  id: string;
  /** Stable identifier used in requests: `company`, `market`, `decisions`... */
  key: string;
  name: string;
  visibility: KnowledgeVisibility;
  position: number;
}

export interface KnowledgeBases {
  /** False when the deployment has no knowledge service at all. */
  offered: boolean;
  state: KnowledgeState;
  knowledge_bases: KnowledgeBase[];
}

export interface KnowledgeDocument {
  /** The record id used by publish, discard and delete. Empty for a document
   *  that exists only in the knowledge service (added there directly). */
  id: string;
  document_id: string;
  /** The knowledge base key. */
  knowledge_base: string;
  title: string;
  state: KnowledgeDocumentState;
  /** `manual`, `url`, `file`, or a newer kind. */
  kind: string;
  /** The page a URL document imports from; empty otherwise. */
  url: string;
  /** `issue`, `comment`, `proposal`, `brief`, `project`, `upload`, `url`, `sync`. */
  source_type: string;
  source_id: string | null;
  /** `agent`, `member` or `system`. */
  created_by_type: string;
  created_by_id: string | null;
  created_at: string;
  published_at: string | null;
  /** Why the last attempt failed; empty otherwise. */
  error: string;
}

export interface KnowledgeDocumentList {
  state: KnowledgeState;
  documents: KnowledgeDocument[];
}

export interface KnowledgeHit {
  document_id: string;
  knowledge_base: string;
  title: string;
  /** The retrieved passage. */
  content: string;
  /** The page a URL document came from; empty otherwise. */
  source: string;
  /** Retrieval always returns the closest matches however weak: the score is
   *  how a caller judges relevance. */
  score: number;
}

export interface KnowledgeSearchResult {
  state: KnowledgeState;
  hits: KnowledgeHit[];
}

export interface ListKnowledgeDocumentsParams {
  kb?: string;
  state?: Exclude<KnowledgeDocumentState, "unknown">;
}

export interface SearchKnowledgeParams {
  q: string;
  kb?: string;
  limit?: number;
}

/** Send `content` or `url`, not both. `publish` is honored for a member only. */
export interface CreateKnowledgeDocumentRequest {
  kb: string;
  title: string;
  content?: string;
  url?: string;
  source_type?: string;
  source_id?: string;
  idempotency_key?: string;
  publish?: boolean;
}
