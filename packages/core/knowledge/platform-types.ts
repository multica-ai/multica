/**
 * Company knowledge, the platform side: data sources, restricted-base access,
 * per-base LLM switches, the model connection test and the owner's WeKnora
 * login. The same rule as types.ts: the server adapts the engine, and `unknown`
 * stands in for a value a newer server may add.
 */

import type { KnowledgeState, KnowledgeVisibility } from "./types";

/** A squad, a floor or one agent: what a grant on a restricted base reaches. */
export type KnowledgeSubjectType = "squad" | "floor" | "agent";

export type KnowledgeAccessLevel = "read" | "write" | "unknown";

/** One sync connection (Notion, Confluence, RSS and others) feeding a base. */
export interface KnowledgeSource {
  id: string;
  /** The knowledge base key the source feeds. */
  knowledge_base: string;
  name: string;
  /** The connector, such as `notion` or `rss`. */
  type: string;
  /** `active`, `paused` or `error`; `unknown` for a value a newer server adds. */
  status: string;
  /** A cron expression; empty for manual sync. */
  sync_schedule: string;
  /** Null before the first sync. */
  last_sync_at: string | null;
  /** The last sync failure; empty when none. */
  error_message: string;
  total_items_synced: number;
}

export interface KnowledgeSources {
  state: KnowledgeState;
  /** Where a new source is created, in the knowledge service's own UI. */
  ui_url: string;
  sources: KnowledgeSource[];
}

/** The repo-docs sync setting and the outcome of its last run (plan 10.12). */
export interface KnowledgeRepoSync {
  enabled: boolean;
  repo_url: string;
  branch: string;
  docs_path: string;
  knowledge_base_key: string;
  /** Null until a run has synced the repository. */
  last_synced_at: string | null;
  /** The failures of the last run, one per line; empty when it had none. */
  last_error: string;
}

export type KnowledgeRepoSyncRequest = Omit<KnowledgeRepoSync, "last_synced_at" | "last_error">;

export interface KnowledgeGrant {
  subject_type: KnowledgeSubjectType | "unknown";
  subject_id: string;
  access: KnowledgeAccessLevel;
}

/** A base's visibility and the grants on it. */
export interface KnowledgeAccess {
  key: string;
  visibility: KnowledgeVisibility;
  grants: KnowledgeGrant[];
}

/** A base's LLM-backed switches after an update. */
export interface KnowledgeBaseSettings {
  key: string;
  wiki_enabled: boolean;
  summaries_enabled: boolean;
}

export interface KnowledgeBaseSettingsPatch {
  wiki_enabled?: boolean;
  summaries_enabled?: boolean;
}

/** One registered model and whether it answered its test. */
export interface KnowledgeModelCheck {
  /** `chat`, `rerank` or `vision`. */
  role: string;
  name: string;
  ok: boolean;
  /** WeKnora's reason when `ok` is false; empty otherwise. */
  error: string;
}

export interface KnowledgeModelChecks {
  /** False when the deployment configures no model. */
  configured: boolean;
  checks: KnowledgeModelCheck[];
}

/** The owner's WeKnora login, shown once. */
export interface KnowledgeOwnerLogin {
  email: string;
  password: string;
  ui_url: string;
}

/** A file a person uploads. The kind is required; the file is published at once. */
export interface UploadKnowledgeDocumentParams {
  kb: string;
  kind: string;
  file: File;
  idempotencyKey?: string;
}

/** Grants may be given to a squad, a floor or an agent. */
export const KNOWLEDGE_SUBJECT_TYPES = ["squad", "floor", "agent"] as const;
export const KNOWLEDGE_ACCESS_LEVELS = ["read", "write"] as const;
