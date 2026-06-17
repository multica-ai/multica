import { z } from "zod";
import type {
  KnowledgeCandidate,
  KnowledgeDetail,
  KnowledgeEffectBucket,
  KnowledgeEffectSummary,
  KnowledgeFeedback,
  KnowledgeGovernanceFinding,
  KnowledgeItem,
  KnowledgeSearchResult,
  KnowledgeSourceDetail,
  ListKnowledgeAnalyticsResponse,
  ListKnowledgeCandidatesResponse,
  ListKnowledgeEffectResponse,
  ListKnowledgeGovernanceFindingsResponse,
  ListKnowledgeResponse,
  ListKnowledgeSourcesResponse,
  SearchKnowledgeResponse,
} from "./types";

const NullableString = z.string().nullable().default(null);

export const KnowledgeItemSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  project_id: NullableString,
  agent_id: NullableString,
  title: z.string().default(""),
  type: z.string().default("lesson"),
  domain_labels: z.array(z.string()).default([]),
  problem_pattern: z.string().default(""),
  trigger_conditions: z.string().default(""),
  diagnostic_steps: z.string().default(""),
  recommended_practice: z.string().default(""),
  anti_patterns: z.string().default(""),
  applicability: z.string().default(""),
  confidence_status: z.string().default("medium"),
  lifecycle_status: z.string().default("draft"),
  created_by: NullableString,
  reviewed_by: NullableString,
  reviewed_at: NullableString,
  published_at: NullableString,
  archived_at: NullableString,
  updated_by: NullableString,
  deprecated_at: NullableString,
  stale_score: z.number().default(0),
  effectiveness_score: z.number().default(100),
  conflict_group: NullableString,
  review_reason: NullableString,
  update_suggestion: NullableString,
  review_needed_at: NullableString,
  governance_checked_at: NullableString,
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).passthrough();

const KnowledgeSourceSchema = z.object({
  id: z.string().default(""),
  knowledge_item_id: z.string().default(""),
  workspace_id: z.string().default(""),
  source_type: z.string().default(""),
  source_id: NullableString,
  source_url: NullableString,
  source_title: NullableString,
  source_excerpt: NullableString,
  created_at: z.string().default(""),
}).passthrough();

const KnowledgeSourceSummarySchema = z.object({
  count: z.number().default(0),
  types: z.array(z.string()).default([]),
  primary_source_type: z.string().default(""),
  primary_source_id: NullableString,
  primary_source_title: z.string().default(""),
}).passthrough();

const KnowledgeSourceDetailSchema = KnowledgeSourceSchema.extend({
  resolved_title: NullableString,
  resolved_url: NullableString,
  resolved_note: NullableString,
}).passthrough();

const KnowledgePublishTargetSchema = z.object({
  id: z.string().default(""),
  knowledge_item_id: z.string().default(""),
  workspace_id: z.string().default(""),
  target_type: z.string().default(""),
  target_id: NullableString,
  target_url: NullableString,
  target_title: NullableString,
  metadata: z.unknown().default({}),
  created_by: NullableString,
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).passthrough();

const KnowledgeEmbeddingMetadataSchema = z.object({
  id: z.string().default(""),
  knowledge_item_id: z.string().default(""),
  workspace_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  content_hash: z.string().default(""),
  embedded_at: z.string().default(""),
  created_at: z.string().default(""),
}).passthrough();

const KnowledgeFeedbackSummarySchema = z.object({
  value: z.string().default(""),
  count: z.number().default(0),
}).passthrough();

export const KnowledgeDetailSchema = z.object({
  item: KnowledgeItemSchema,
  sources: z.array(KnowledgeSourceSchema).default([]),
  source_summary: KnowledgeSourceSummarySchema.default({
    count: 0,
    types: [],
    primary_source_type: "",
    primary_source_id: null,
    primary_source_title: "",
  }),
  publish_targets: z.array(KnowledgePublishTargetSchema).default([]),
  embeddings: z.array(KnowledgeEmbeddingMetadataSchema).default([]),
  feedback_summary: z.array(KnowledgeFeedbackSummarySchema).default([]),
}).passthrough();

export const KnowledgeSearchResultSchema = z.object({
  item: KnowledgeItemSchema,
  text_score: z.number().default(0),
  vector_score: z.number().default(0),
  final_score: z.number().default(0),
  match_reason: z.string().default(""),
}).passthrough();

export const KnowledgeCandidateSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  issue_id: z.string().default(""),
  comment_id: NullableString,
  agent_task_id: NullableString,
  source_type: z.string().default(""),
  source_id: z.string().default(""),
  trigger_reason: z.string().default(""),
  signal_strength: z.string().default(""),
  signals: z.array(z.string()).default([]),
  score: z.number().default(0),
  status: z.string().default(""),
  dedupe_key: z.string().default(""),
  created_by: NullableString,
  metadata: z.unknown().default({}),
  evaluated_at: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).passthrough();

export const KnowledgeGovernanceFindingSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  knowledge_item_id: z.string().default(""),
  finding_type: z.string().default("stale"),
  status: z.string().default("open"),
  severity: z.number().default(0),
  reason: z.string().default(""),
  evidence: z.unknown().default({}),
  suggested_action: z.string().default(""),
  source_map: z.unknown().default({}),
  draft_knowledge_item_id: NullableString,
  resolved_by: NullableString,
  resolved_at: NullableString,
  dismissed_by: NullableString,
  dismissed_at: NullableString,
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).passthrough();

export const KnowledgeFeedbackSchema = z.object({
  id: z.string().default(""),
  knowledge_item_id: z.string().default(""),
  workspace_id: z.string().default(""),
  member_id: z.string().default(""),
  value: z.string().default("helpful"),
  note: NullableString,
  created_at: z.string().default(""),
}).passthrough();

export const KnowledgeAnalyticsRowSchema = z.object({
  knowledge_item_id: z.string().default(""),
  title: z.string().default(""),
  type: z.string().default(""),
  lifecycle_status: z.string().default(""),
  retrieval_count: z.number().default(0),
  injection_count: z.number().default(0),
  injected_task_count: z.number().default(0),
  usage_count: z.number().default(0),
  agent_reference_count: z.number().default(0),
  active_search_count: z.number().default(0),
  helpful_count: z.number().default(0),
  not_helpful_count: z.number().default(0),
  misleading_count: z.number().default(0),
  outdated_count: z.number().default(0),
  latest_negative_feedback_at: NullableString,
  successful_task_count: z.number().default(0),
  failed_task_count: z.number().default(0),
  total_task_seconds: z.number().default(0),
  total_tokens: z.number().default(0),
}).passthrough();

export const ListKnowledgeResponseSchema = z.object({
  items: z.array(KnowledgeItemSchema).default([]),
  total: z.number().default(0),
}).passthrough();

export const SearchKnowledgeResponseSchema = z.object({
  results: z.array(KnowledgeSearchResultSchema).default([]),
  total: z.number().default(0),
}).passthrough();

export const ListKnowledgeSourcesResponseSchema = z.object({
  sources: z.array(KnowledgeSourceDetailSchema).default([]),
  total: z.number().default(0),
}).passthrough();

export const ListKnowledgeCandidatesResponseSchema = z.object({
  candidates: z.array(KnowledgeCandidateSchema).default([]),
  total: z.number().default(0),
}).passthrough();

export const ListKnowledgeGovernanceFindingsResponseSchema = z.object({
  findings: z.array(KnowledgeGovernanceFindingSchema).default([]),
  total: z.number().default(0),
}).passthrough();

export const ListKnowledgeAnalyticsResponseSchema = z.object({
  items: z.array(KnowledgeAnalyticsRowSchema).default([]),
  total: z.number().default(0),
}).passthrough();

export const EMPTY_KNOWLEDGE_ITEM: KnowledgeItem = {
  id: "",
  workspace_id: "",
  project_id: null,
  agent_id: null,
  title: "",
  type: "lesson",
  domain_labels: [],
  problem_pattern: "",
  trigger_conditions: "",
  diagnostic_steps: "",
  recommended_practice: "",
  anti_patterns: "",
  applicability: "",
  confidence_status: "medium",
  lifecycle_status: "draft",
  created_by: null,
  reviewed_by: null,
  reviewed_at: null,
  published_at: null,
  archived_at: null,
  updated_by: null,
  deprecated_at: null,
  stale_score: 0,
  effectiveness_score: 100,
  conflict_group: null,
  review_reason: null,
  update_suggestion: null,
  review_needed_at: null,
  governance_checked_at: null,
  created_at: "",
  updated_at: "",
};

export const EMPTY_KNOWLEDGE_DETAIL: KnowledgeDetail = {
  item: EMPTY_KNOWLEDGE_ITEM,
  sources: [],
  source_summary: {
    count: 0,
    types: [],
    primary_source_type: "",
    primary_source_id: null,
    primary_source_title: "",
  },
  publish_targets: [],
  embeddings: [],
  feedback_summary: [],
};

export const EMPTY_LIST_KNOWLEDGE_RESPONSE: ListKnowledgeResponse = {
  items: [],
  total: 0,
};

export const EMPTY_SEARCH_KNOWLEDGE_RESPONSE: SearchKnowledgeResponse = {
  results: [],
  total: 0,
};

export const EMPTY_LIST_KNOWLEDGE_SOURCES_RESPONSE: ListKnowledgeSourcesResponse = {
  sources: [],
  total: 0,
};

export const EMPTY_LIST_KNOWLEDGE_CANDIDATES_RESPONSE: ListKnowledgeCandidatesResponse = {
  candidates: [],
  total: 0,
};

export const EMPTY_LIST_KNOWLEDGE_GOVERNANCE_FINDINGS_RESPONSE: ListKnowledgeGovernanceFindingsResponse = {
  findings: [],
  total: 0,
};

export const EMPTY_LIST_KNOWLEDGE_ANALYTICS_RESPONSE: ListKnowledgeAnalyticsResponse = {
  items: [],
  total: 0,
};

export const EMPTY_KNOWLEDGE_FEEDBACK: KnowledgeFeedback = {
  id: "",
  knowledge_item_id: "",
  workspace_id: "",
  member_id: "",
  value: "helpful",
  note: null,
  created_at: "",
};

// Knowledge Effect Schemas

export const KnowledgeEffectBucketSchema = z.object({
  bucket_hour: z.string().default(""),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  project_id: NullableString,
  model: z.string().default(""),
  provider: z.string().default(""),
  task_kind: z.string().default("direct"),
  has_injection: z.boolean().default(false),
  task_count: z.number().default(0),
  successful_count: z.number().default(0),
  failed_count: z.number().default(0),
  total_duration_secs: z.number().default(0),
  duration_task_count: z.number().default(0),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  rerun_count: z.number().default(0),
  follow_up_count: z.number().default(0),
  max_attempt: z.number().default(1),
}).passthrough();

export const KnowledgeEffectSummarySchema = z.object({
  total_tasks: z.number().default(0),
  total_successful: z.number().default(0),
  total_failed: z.number().default(0),
  total_duration_secs: z.number().default(0),
  total_duration_tasks: z.number().default(0),
  total_input_tokens: z.number().default(0),
  total_output_tokens: z.number().default(0),
  total_cache_read_tokens: z.number().default(0),
  total_cache_write_tokens: z.number().default(0),
  total_reruns: z.number().default(0),
  total_follow_ups: z.number().default(0),
}).passthrough();

export const ListKnowledgeEffectResponseSchema = z.object({
  buckets: z.array(KnowledgeEffectBucketSchema).default([]),
  total: z.number().default(0),
}).passthrough();

export const EMPTY_KNOWLEDGE_EFFECT_BUCKET: KnowledgeEffectBucket = {
  bucket_hour: "",
  workspace_id: "",
  agent_id: "",
  project_id: null,
  model: "",
  provider: "",
  task_kind: "direct",
  has_injection: false,
  task_count: 0,
  successful_count: 0,
  failed_count: 0,
  total_duration_secs: 0,
  duration_task_count: 0,
  input_tokens: 0,
  output_tokens: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  rerun_count: 0,
  follow_up_count: 0,
  max_attempt: 1,
};

export const EMPTY_KNOWLEDGE_EFFECT_SUMMARY: KnowledgeEffectSummary = {
  total_tasks: 0,
  total_successful: 0,
  total_failed: 0,
  total_duration_secs: 0,
  total_duration_tasks: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_read_tokens: 0,
  total_cache_write_tokens: 0,
  total_reruns: 0,
  total_follow_ups: 0,
};

export const EMPTY_LIST_KNOWLEDGE_EFFECT_RESPONSE: ListKnowledgeEffectResponse = {
  buckets: [],
  total: 0,
};

export type {
  KnowledgeCandidate,
  KnowledgeDetail,
  KnowledgeFeedback,
  KnowledgeGovernanceFinding,
  KnowledgeItem,
  KnowledgeSearchResult,
  KnowledgeSourceDetail,
};
