import { z } from "zod";
import type {
  KnowledgeAnswerResponse,
  KnowledgeBase,
  KnowledgeBaseListResponse,
  KnowledgeChunk,
  KnowledgeCreateDocumentResponse,
  KnowledgeDocument,
  KnowledgeDocumentListResponse,
  KnowledgeEntity,
  KnowledgeEntityDetail,
  KnowledgeEntityListResponse,
  KnowledgeGraphEdit,
  KnowledgeGraphEditListResponse,
  KnowledgeGraphResponse,
  KnowledgeJob,
  KnowledgeJobListResponse,
  KnowledgeModelBinding,
  KnowledgeModelSettings,
  KnowledgePreviewCapability,
  KnowledgeProvider,
  KnowledgeProviderListResponse,
  KnowledgeRelation,
  KnowledgeRelationEvidenceResponse,
  KnowledgeSearchResponse,
  KnowledgeVersionBlocksResponse,
  KnowledgeVersionListResponse,
} from "../types/knowledge";

const recordSchema = z.record(z.string(), z.unknown()).default({});

export const KnowledgeBaseSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  creator_id: z.string(),
  name: z.string().default(""),
  description: z.string().default(""),
  visibility: z.string().default("private"),
  revision: z.number().default(0),
  acl_revision: z.number().default(0),
  corpus_revision: z.number().default(0),
  active_index_id: z.string().nullable().optional().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const KnowledgeDocumentSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  knowledge_base_id: z.string(),
  title: z.string().default(""),
  source_kind: z.string().default("file"),
  source_url: z.string().nullable().optional().default(null),
  tags: z.array(z.string()).default([]),
  current_version_id: z.string().nullable().optional().default(null),
  revision: z.number().default(0),
  status: z.string().default("processing"),
  deleted_at: z.string().nullable().optional().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const KnowledgeVersionSchema = z.object({
  id: z.string(),
  document_id: z.string(),
  version_number: z.number().default(0),
  source_object_key: z.string().optional().default(""),
  source_hash: z.string().optional().default(""),
  byte_size: z.number().optional().default(0),
  mime_type: z.string().optional().default("application/octet-stream"),
  source_metadata: recordSchema,
  parsed_object_key: z.string().nullable().optional().default(null),
  parser_version: z.string().optional().default(""),
  chunker_version: z.string().optional().default(""),
  config_snapshot: recordSchema,
  status: z.string().default("processing"),
  error_code: z.string().nullable().optional().default(null),
  created_at: z.string().default(""),
}).loose();

export const KnowledgeBlockSchema = z.object({
  block_id: z.string(),
  kind: z.string().default("paragraph"),
  text: z.string().default(""),
  heading_path: z.array(z.string()).optional().default([]),
  locator: recordSchema,
  model_derived: z.boolean().optional().default(false),
}).loose();

export const KnowledgeChunkSchema = z.object({
  id: z.string(),
  document_id: z.string(),
  version_id: z.string(),
  ordinal: z.number().default(0),
  block_refs: z.array(z.string()).default([]),
  text: z.string().default(""),
  source_locator: recordSchema,
  token_estimate: z.number().default(0),
  text_hash: z.string().default(""),
}).loose();

export const KnowledgeProviderSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string().default(""),
  preset: z.string().default("custom"),
  protocol: z.string().default("openai_compatible"),
  base_url: z.string().default(""),
  has_api_key: z.boolean().default(false),
  secret_revision: z.number().default(0),
  is_enabled: z.boolean().default(true),
  revision: z.number().default(0),
  created_by: z.string().default(""),
}).loose();

export const KnowledgeProviderModelSchema = z.object({
  id: z.string(),
  object: z.string().optional(),
  owned_by: z.string().optional(),
}).loose();

export const KnowledgeBindingSchema: z.ZodType<KnowledgeModelBinding> = z.object({
  id: z.string().optional(),
  workspace_id: z.string(),
  knowledge_base_id: z.string().nullable().optional().default(null),
  purpose: z.string().default(""),
  mode: z.string().default("inherit"),
  provider_id: z.string().nullable().optional().default(null),
  model: z.string().nullable().optional().default(null),
  options: recordSchema,
  revision: z.number().default(0),
}).loose();

export const KnowledgeCapabilitySchema = z.object({
  provider_id: z.string(),
  secret_revision: z.number().default(0),
  model: z.string().default(""),
  capability: z.string().default(""),
  status: z.string().default("not_tested"),
  dimension: z.number().optional(),
  details: recordSchema,
  tested_at: z.string().nullable().optional().default(null),
}).loose();

export const KnowledgeModelSettingsSchema: z.ZodType<KnowledgeModelSettings> = z.object({
  workspace_id: z.string(),
  knowledge_base_id: z.string().nullable().optional().default(null),
  revision: z.number().default(0),
  main: KnowledgeBindingSchema.nullable().optional().default(null),
  purposes: z.record(z.string(), KnowledgeBindingSchema.nullable()).default({}),
  capabilities: z.array(KnowledgeCapabilitySchema).default([]),
  affected_bases: z.array(z.string()).optional().default([]),
}).loose();

export const KnowledgeSearchResultSchema = z.object({
  chunk_id: z.string(),
  document_id: z.string(),
  version_id: z.string(),
  title: z.string().default(""),
  text: z.string().default(""),
  source_locator: recordSchema,
  retrieval_channels: z.array(z.string()).default([]),
  rank: z.number().default(0),
  citation: z.object({
    id: z.string(),
    locator: recordSchema,
    source_url: z.string().nullable().optional().default(null),
    document_path: z.string().default(""),
  }).loose(),
}).loose();

export const KnowledgeSearchSchema: z.ZodType<KnowledgeSearchResponse> = z.object({
  query_id: z.string(),
  knowledge_base_id: z.string(),
  mode_requested: z.string().default("hybrid"),
  mode_effective: z.string().default("keyword"),
  rerank_applied: z.boolean().default(false),
  index_id: z.string().nullable().optional().default(null),
  warnings: z.array(z.string()).default([]),
  results: z.array(KnowledgeSearchResultSchema).default([]),
}).loose();

export const KnowledgeAnswerSchema: z.ZodType<KnowledgeAnswerResponse> = z.object({
  query_id: z.string(),
  question: z.string().default(""),
  answer: z.string().default(""),
  citations: z.array(z.object({ id: z.string(), paragraph: z.number().optional(), answer_paragraph: z.number().optional() }).loose()).default([]),
  insufficient_evidence: z.boolean().default(true),
  context_truncated: z.boolean().default(false),
  generation_error: z.string().nullable().optional().default(null),
  search: KnowledgeSearchSchema,
}).loose();

export const KnowledgeEntitySchema: z.ZodType<KnowledgeEntity> = z.object({
  id: z.string(),
  knowledge_base_id: z.string(),
  type: z.string().default("concept"),
  canonical_name: z.string().default(""),
  normalized_name: z.string().default(""),
  review_status: z.string().default("automatic"),
  revision: z.number().default(0),
  evidence_count: z.number().default(0),
}).loose();

export const KnowledgeEvidenceSchema = z.object({
  id: z.string(),
  subject_type: z.string().default("entity"),
  subject_id: z.string(),
  version_id: z.string(),
  chunk_id: z.string(),
  source_locator: recordSchema,
  quote: z.string().default(""),
  start_offset: z.number().nullable().optional().default(null),
  end_offset: z.number().nullable().optional().default(null),
  extraction_run_id: z.string().nullable().optional().default(null),
}).loose();

export const KnowledgeRelationSchema: z.ZodType<KnowledgeRelation> = z.object({
  id: z.string(),
  knowledge_base_id: z.string(),
  source_entity_id: z.string(),
  target_entity_id: z.string(),
  predicate: z.string().default("related_to"),
  qualifier: recordSchema,
  review_status: z.string().default("automatic"),
  revision: z.number().default(0),
  evidence_count: z.number().default(0),
}).loose();

export const KnowledgeJobSchema: z.ZodType<KnowledgeJob> = z.object({
  id: z.string(),
  knowledge_base_id: z.string(),
  document_version_id: z.string().nullable().optional().default(null),
  index_id: z.string().nullable().optional().default(null),
  stage: z.string().default("parse"),
  status: z.string().default("queued"),
  attempt: z.number().default(0),
  available_at: z.string().default(""),
  progress: recordSchema,
  error_code: z.string().nullable().optional().default(null),
  created_at: z.string().default(""),
}).loose();

export const KnowledgeGraphEditSchema: z.ZodType<KnowledgeGraphEdit> = z.object({
  id: z.string(),
  operation: z.string().default("confirm"),
  target_id: z.string(),
  payload: recordSchema,
  previous_state: recordSchema,
  actor_id: z.string(),
  expected_revision: z.number().default(0),
  created_at: z.string().default(""),
  reverted_at: z.string().nullable().optional().default(null),
}).loose();

export const KnowledgeBaseListSchema: z.ZodType<KnowledgeBaseListResponse> = z.object({
  bases: z.array(KnowledgeBaseSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeDocumentListSchema: z.ZodType<KnowledgeDocumentListResponse> = z.object({
  documents: z.array(KnowledgeDocumentSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeVersionListSchema: z.ZodType<KnowledgeVersionListResponse> = z.object({
  versions: z.array(KnowledgeVersionSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeBlocksSchema: z.ZodType<KnowledgeVersionBlocksResponse> = z.object({
  version_id: z.string(),
  blocks: z.array(KnowledgeBlockSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeProviderListSchema: z.ZodType<KnowledgeProviderListResponse> = z.object({
  providers: z.array(KnowledgeProviderSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeJobListSchema: z.ZodType<KnowledgeJobListResponse> = z.object({
  jobs: z.array(KnowledgeJobSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeEntityListSchema: z.ZodType<KnowledgeEntityListResponse> = z.object({
  entities: z.array(KnowledgeEntitySchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeGraphEditListSchema: z.ZodType<KnowledgeGraphEditListResponse> = z.object({
  edits: z.array(KnowledgeGraphEditSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const KnowledgeEntityDetailSchema: z.ZodType<KnowledgeEntityDetail> = z.object({
  entity: KnowledgeEntitySchema,
  evidence: z.array(KnowledgeEvidenceSchema).default([]),
}).loose();

export const KnowledgeRelationEvidenceSchema: z.ZodType<KnowledgeRelationEvidenceResponse> = z.object({
  evidence: z.array(KnowledgeEvidenceSchema).default([]),
}).loose();

export const KnowledgeGraphSchema: z.ZodType<KnowledgeGraphResponse> = z.object({
  nodes: z.array(KnowledgeEntitySchema).default([]),
  relations: z.array(KnowledgeRelationSchema).default([]),
  truncated: z.boolean().default(false),
  node_count: z.number().default(0),
  edge_count: z.number().default(0),
}).loose();

export const KnowledgeCreateDocumentSchema: z.ZodType<KnowledgeCreateDocumentResponse> = z.object({
  document: KnowledgeDocumentSchema,
  job: KnowledgeJobSchema.optional(),
}).loose();

export const KnowledgePreviewCapabilitySchema: z.ZodType<KnowledgePreviewCapability> = z.object({
  version_id: z.string(),
  document_id: z.string(),
  expires_at: z.string(),
  token: z.string(),
  preview_url: z.string(),
}).loose();

export const KnowledgeProviderModelsSchema = z.object({
  models: z.array(KnowledgeProviderModelSchema).default([]),
}).loose();

export const KnowledgeProviderTestSchema = z.object({
  status: z.string().default("failed"),
  capability: z.string().default(""),
  model: z.string().default(""),
  dimension: z.number().optional(),
  details: recordSchema,
}).loose();

export const KnowledgeIndexSchema = z.object({
  index_id: z.string(),
  job: KnowledgeJobSchema.optional(),
  dimension: z.number().default(0),
}).loose();

export const EMPTY_KNOWLEDGE_BASE: KnowledgeBase = {
  id: "",
  workspace_id: "",
  creator_id: "",
  name: "",
  description: "",
  visibility: "private",
  revision: 0,
  acl_revision: 0,
  corpus_revision: 0,
  active_index_id: null,
  created_at: "",
  updated_at: "",
};

export const EMPTY_KNOWLEDGE_DOCUMENT: KnowledgeDocument = {
  id: "",
  workspace_id: "",
  knowledge_base_id: "",
  title: "",
  source_kind: "file",
  source_url: null,
  tags: [],
  current_version_id: null,
  revision: 0,
  status: "processing",
  created_at: "",
  updated_at: "",
};

export const EMPTY_KNOWLEDGE_JOB: KnowledgeJob = {
  id: "",
  knowledge_base_id: "",
  document_version_id: null,
  index_id: null,
  stage: "parse",
  status: "failed",
  attempt: 0,
  available_at: "",
  progress: {},
  error_code: null,
  created_at: "",
};

export const EMPTY_KNOWLEDGE_CHUNK: KnowledgeChunk = {
  id: "",
  document_id: "",
  version_id: "",
  ordinal: 0,
  block_refs: [],
  text: "",
  source_locator: {},
  token_estimate: 0,
  text_hash: "",
};

export const EMPTY_KNOWLEDGE_SETTINGS: KnowledgeModelSettings = {
  workspace_id: "",
  knowledge_base_id: null,
  revision: 0,
  main: null,
  purposes: {},
  capabilities: [],
  affected_bases: [],
};

export const EMPTY_KNOWLEDGE_PROVIDER: KnowledgeProvider = {
  id: "",
  workspace_id: "",
  name: "",
  preset: "custom",
  protocol: "openai_compatible",
  base_url: "",
  has_api_key: false,
  secret_revision: 0,
  is_enabled: false,
  revision: 0,
  created_by: "",
};

export const EMPTY_KNOWLEDGE_SEARCH: KnowledgeSearchResponse = {
  query_id: "",
  knowledge_base_id: "",
  mode_requested: "hybrid",
  mode_effective: "keyword",
  rerank_applied: false,
  index_id: null,
  warnings: ["invalid_response"],
  results: [],
};

export const EMPTY_KNOWLEDGE_ANSWER: KnowledgeAnswerResponse = {
  query_id: "",
  question: "",
  answer: "",
  citations: [],
  insufficient_evidence: true,
  context_truncated: false,
  generation_error: "invalid_response",
  search: EMPTY_KNOWLEDGE_SEARCH,
};

export const EMPTY_KNOWLEDGE_ENTITY: KnowledgeEntity = {
  id: "",
  knowledge_base_id: "",
  type: "concept",
  canonical_name: "",
  normalized_name: "",
  review_status: "automatic",
  revision: 0,
  evidence_count: 0,
};

export const EMPTY_KNOWLEDGE_RELATION: KnowledgeRelation = {
  id: "",
  knowledge_base_id: "",
  source_entity_id: "",
  target_entity_id: "",
  predicate: "related_to",
  qualifier: {},
  review_status: "automatic",
  revision: 0,
  evidence_count: 0,
};

export const EMPTY_KNOWLEDGE_EDIT: KnowledgeGraphEdit = {
  id: "",
  operation: "confirm",
  target_id: "",
  payload: {},
  previous_state: {},
  actor_id: "",
  expected_revision: 0,
  created_at: "",
  reverted_at: null,
};

export const EMPTY_KNOWLEDGE_VERSION_BLOCKS: KnowledgeVersionBlocksResponse = {
  version_id: "",
  blocks: [],
};

export const EMPTY_KNOWLEDGE_CREATE_DOCUMENT: KnowledgeCreateDocumentResponse = {
  document: EMPTY_KNOWLEDGE_DOCUMENT,
};

export const EMPTY_KNOWLEDGE_PREVIEW_CAPABILITY: KnowledgePreviewCapability = {
  version_id: "",
  document_id: "",
  expires_at: "",
  token: "",
  preview_url: "",
};
