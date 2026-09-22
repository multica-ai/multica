export type KnowledgeVisibility = "private" | "workspace";
export type KnowledgeSourceKind = "file" | "url";

export interface KnowledgeBase {
  id: string;
  workspace_id: string;
  creator_id: string;
  name: string;
  description: string;
  visibility: KnowledgeVisibility | string;
  revision: number;
  acl_revision: number;
  corpus_revision: number;
  active_index_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface KnowledgeBaseListResponse {
  bases: KnowledgeBase[];
  next_cursor?: string;
}

export interface KnowledgeDocument {
  id: string;
  workspace_id: string;
  knowledge_base_id: string;
  title: string;
  source_kind: KnowledgeSourceKind | string;
  source_url?: string | null;
  tags: string[];
  current_version_id?: string | null;
  revision: number;
  status: string;
  deleted_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface KnowledgeDocumentListResponse {
  documents: KnowledgeDocument[];
  next_cursor?: string;
}

export interface KnowledgeDocumentVersion {
  id: string;
  document_id: string;
  version_number: number;
  source_object_key?: string;
  source_hash?: string;
  byte_size?: number;
  mime_type?: string;
  source_metadata?: Record<string, unknown>;
  parsed_object_key?: string | null;
  parser_version?: string;
  chunker_version?: string;
  config_snapshot?: Record<string, unknown>;
  status: string;
  error_code?: string | null;
  created_at: string;
}

export interface KnowledgeVersionListResponse {
  versions: KnowledgeDocumentVersion[];
  next_cursor?: string;
}

export interface ReplaceKnowledgeVersionRequest {
  expected_revision: number;
  url?: string;
  metadata?: Record<string, unknown>;
}

export interface KnowledgeDocumentBlock {
  block_id: string;
  kind: string;
  text: string;
  heading_path?: string[];
  locator: Record<string, unknown>;
  model_derived?: boolean;
}

export interface KnowledgeVersionBlocksResponse {
  version_id: string;
  blocks: KnowledgeDocumentBlock[];
  next_cursor?: string;
}

export interface KnowledgePreviewCapability {
  version_id: string;
  document_id: string;
  expires_at: string;
  token: string;
  preview_url: string;
}

export interface ConfirmKnowledgeBlocksRequest {
  version_id?: string;
  expected_revision: number;
  blocks: KnowledgeDocumentBlock[];
}

export interface KnowledgeChunk {
  id: string;
  document_id: string;
  version_id: string;
  ordinal: number;
  block_refs: string[];
  text: string;
  source_locator: Record<string, unknown>;
  token_estimate: number;
  text_hash: string;
}

export interface KnowledgeProvider {
  id: string;
  workspace_id: string;
  name: string;
  preset: string;
  protocol: string;
  base_url: string;
  has_api_key: boolean;
  secret_revision: number;
  is_enabled: boolean;
  revision: number;
  created_by: string;
}

export interface KnowledgeProviderListResponse {
  providers: KnowledgeProvider[];
  next_cursor?: string;
}

export interface KnowledgeProviderModel {
  id: string;
  object?: string;
  owned_by?: string;
}

export interface CreateKnowledgeProviderRequest {
  name: string;
  preset?: string;
  protocol?: string;
  base_url: string;
  api_key: string;
}

export interface UpdateKnowledgeProviderRequest {
  name?: string;
  api_key?: string;
  is_enabled?: boolean;
  base_url?: string;
  protocol?: string;
  expected_revision: number;
}

export interface KnowledgeModelBinding {
  id?: string;
  workspace_id: string;
  knowledge_base_id?: string | null;
  purpose: string;
  mode: string;
  provider_id?: string | null;
  model?: string | null;
  options?: Record<string, unknown>;
  revision: number;
}

export interface KnowledgeCapability {
  provider_id: string;
  secret_revision: number;
  model: string;
  capability: string;
  status: string;
  dimension?: number;
  details?: Record<string, unknown>;
  tested_at?: string | null;
}

export interface KnowledgeModelSettings {
  workspace_id: string;
  knowledge_base_id?: string | null;
  revision: number;
  main?: KnowledgeModelBinding | null;
  purposes: Record<string, KnowledgeModelBinding | null>;
  capabilities: KnowledgeCapability[];
  affected_bases?: string[];
}

export interface KnowledgeModelBindingRequest {
  mode: string;
  provider_id?: string;
  model?: string;
  options?: Record<string, unknown>;
}

export interface KnowledgeModelSettingsRequest {
  expected_revision: number;
  main?: KnowledgeModelBindingRequest;
  purposes?: Record<string, KnowledgeModelBindingRequest>;
}

export interface KnowledgeSearchResult {
  chunk_id: string;
  document_id: string;
  version_id: string;
  title: string;
  text: string;
  source_locator: Record<string, unknown>;
  retrieval_channels: string[];
  rank: number;
  citation: {
    id: string;
    locator: Record<string, unknown>;
    source_url?: string | null;
    document_path: string;
  };
}

export interface KnowledgeSearchResponse {
  query_id: string;
  knowledge_base_id: string;
  mode_requested: string;
  mode_effective: string;
  rerank_applied: boolean;
  index_id?: string | null;
  warnings: string[];
  results: KnowledgeSearchResult[];
}

export interface KnowledgeAnswerResponse {
  query_id: string;
  question: string;
  answer: string;
  citations: Array<{ id: string; paragraph?: number; answer_paragraph?: number }>;
  insufficient_evidence: boolean;
  context_truncated: boolean;
  generation_error?: string | null;
  search: KnowledgeSearchResponse;
}

export interface KnowledgeEntity {
  id: string;
  knowledge_base_id: string;
  type: string;
  canonical_name: string;
  normalized_name: string;
  review_status: string;
  revision: number;
  evidence_count: number;
}

export interface KnowledgeEntityListResponse {
  entities: KnowledgeEntity[];
  next_cursor?: string;
}

export interface KnowledgeEvidence {
  id: string;
  subject_type: string;
  subject_id: string;
  version_id: string;
  chunk_id: string;
  source_locator: Record<string, unknown>;
  quote: string;
  start_offset?: number | null;
  end_offset?: number | null;
  extraction_run_id?: string | null;
}

export interface KnowledgeEntityDetail {
  entity: KnowledgeEntity;
  evidence: KnowledgeEvidence[];
}

export interface KnowledgeRelation {
  id: string;
  knowledge_base_id: string;
  source_entity_id: string;
  target_entity_id: string;
  predicate: string;
  qualifier: Record<string, unknown>;
  review_status: string;
  revision: number;
  evidence_count: number;
}

export interface KnowledgeRelationEvidenceResponse {
  evidence: KnowledgeEvidence[];
}

export interface KnowledgeGraphResponse {
  nodes: KnowledgeEntity[];
  relations: KnowledgeRelation[];
  truncated: boolean;
  node_count: number;
  edge_count: number;
}

export interface KnowledgeJob {
  id: string;
  knowledge_base_id: string;
  document_version_id?: string | null;
  index_id?: string | null;
  stage: string;
  status: string;
  attempt: number;
  available_at: string;
  progress: Record<string, unknown>;
  error_code?: string | null;
  created_at: string;
}

export interface KnowledgeJobListResponse {
  jobs: KnowledgeJob[];
  next_cursor?: string;
}

export interface KnowledgeGraphEdit {
  id: string;
  operation: string;
  target_id: string;
  payload: Record<string, unknown>;
  previous_state: Record<string, unknown>;
  actor_id: string;
  expected_revision: number;
  created_at: string;
  reverted_at?: string | null;
}

export interface KnowledgeGraphEditListResponse {
  edits: KnowledgeGraphEdit[];
  next_cursor?: string;
}

export interface CreateKnowledgeBaseRequest {
  name: string;
  description?: string;
  visibility?: KnowledgeVisibility;
}

export interface UpdateKnowledgeBaseRequest {
  name?: string;
  description?: string;
  visibility?: KnowledgeVisibility;
  expected_revision: number;
}

export interface SearchKnowledgeRequest {
  query: string;
  limit?: number;
  mode?: "hybrid" | "keyword" | "semantic" | string;
  filters?: {
    document_ids?: string[];
    tags?: string[];
    source_kind?: string;
  };
}

export interface AnswerKnowledgeRequest {
  question: string;
  query?: string;
  limit?: number;
  mode?: string;
  filters?: SearchKnowledgeRequest["filters"];
}

export interface KnowledgeCreateDocumentResponse {
  document: KnowledgeDocument;
  job?: KnowledgeJob;
}
