export type KnowledgeType = "lesson" | "playbook" | "reference";
export type KnowledgeLifecycleStatus =
  | "draft"
  | "reviewed"
  | "published"
  | "archived"
  | "deprecated";
export type KnowledgeConfidenceStatus = "low" | "medium" | "high";
export type KnowledgeFeedbackValue = "helpful" | "not_helpful" | "misleading" | "outdated";

export interface KnowledgeItem {
  id: string;
  workspace_id: string;
  project_id: string | null;
  agent_id: string | null;
  title: string;
  type: KnowledgeType;
  domain_labels: string[];
  problem_pattern: string;
  trigger_conditions: string;
  diagnostic_steps: string;
  recommended_practice: string;
  anti_patterns: string;
  applicability: string;
  confidence_status: KnowledgeConfidenceStatus;
  lifecycle_status: KnowledgeLifecycleStatus;
  created_by: string | null;
  reviewed_by: string | null;
  reviewed_at: string | null;
  published_at: string | null;
  archived_at: string | null;
  updated_by: string | null;
  deprecated_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface KnowledgeSource {
  id: string;
  knowledge_item_id: string;
  workspace_id: string;
  source_type: string;
  source_id: string | null;
  source_url: string | null;
  source_title: string | null;
  source_excerpt: string | null;
  created_at: string;
}

export interface KnowledgeSourceSummary {
  count: number;
  types: string[];
  primary_source_type: string;
  primary_source_id: string | null;
  primary_source_title: string;
}

export interface KnowledgeSourceDetail extends KnowledgeSource {
  resolved_title: string | null;
  resolved_url: string | null;
  resolved_note: string | null;
}

export interface KnowledgePublishTarget {
  id: string;
  knowledge_item_id: string;
  workspace_id: string;
  target_type: string;
  target_id: string | null;
  target_url: string | null;
  target_title: string | null;
  metadata: unknown;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface KnowledgeEmbeddingMetadata {
  id: string;
  knowledge_item_id: string;
  workspace_id: string;
  provider: string;
  model: string;
  content_hash: string;
  embedded_at: string;
  created_at: string;
}

export interface KnowledgeFeedbackSummary {
  value: string;
  count: number;
}

export interface KnowledgeFeedback {
  id: string;
  knowledge_item_id: string;
  workspace_id: string;
  member_id: string;
  value: KnowledgeFeedbackValue;
  note: string | null;
  created_at: string;
}

export interface KnowledgeDetail {
  item: KnowledgeItem;
  sources: KnowledgeSource[];
  source_summary: KnowledgeSourceSummary;
  publish_targets: KnowledgePublishTarget[];
  embeddings: KnowledgeEmbeddingMetadata[];
  feedback_summary: KnowledgeFeedbackSummary[];
}

export interface KnowledgeSearchResult {
  item: KnowledgeItem;
  text_score: number;
  vector_score: number;
  final_score: number;
  match_reason: string;
}

export interface KnowledgeCandidate {
  id: string;
  workspace_id: string;
  issue_id: string;
  comment_id: string | null;
  agent_task_id: string | null;
  source_type: string;
  source_id: string;
  trigger_reason: string;
  signal_strength: string;
  signals: string[];
  score: number;
  status: string;
  dedupe_key: string;
  created_by: string | null;
  metadata: unknown;
  evaluated_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListKnowledgeParams {
  q?: string;
  status?: KnowledgeLifecycleStatus | string;
  type?: KnowledgeType | string;
  labels?: string[];
  project_id?: string | null;
  agent_id?: string | null;
  include_inactive?: boolean;
  limit?: number;
  offset?: number;
}

export interface ListKnowledgeResponse {
  items: KnowledgeItem[];
  total: number;
}

export interface ListKnowledgeSourcesResponse {
  sources: KnowledgeSourceDetail[];
  total: number;
}

export interface SearchKnowledgeRequest {
  query: string;
  embedding?: number[];
  limit?: number;
  filters?: {
    project_id?: string | null;
    agent_id?: string | null;
    labels?: string[];
    types?: string[];
    statuses?: string[];
  };
}

export interface SearchKnowledgeResponse {
  results: KnowledgeSearchResult[];
  total: number;
}

export interface ListKnowledgeCandidatesParams {
  issue_id?: string | null;
  status?: string;
  source_type?: string;
  limit?: number;
  offset?: number;
}

export interface ListKnowledgeCandidatesResponse {
  candidates: KnowledgeCandidate[];
  total: number;
}

export interface UpdateKnowledgeRequest {
  project_id?: string | null;
  agent_id?: string | null;
  title?: string;
  type?: KnowledgeType | string;
  domain_labels?: string[];
  problem_pattern?: string;
  trigger_conditions?: string;
  diagnostic_steps?: string;
  recommended_practice?: string;
  anti_patterns?: string;
  applicability?: string;
  confidence_status?: KnowledgeConfidenceStatus | string;
  lifecycle_status?: KnowledgeLifecycleStatus | string;
}

export interface CreateKnowledgeFeedbackRequest {
  value: KnowledgeFeedbackValue;
  note?: string | null;
}

export interface EvaluateKnowledgeCandidateRequest {
  source_type: string;
  source_id: string;
  trigger_reason: string;
  manual?: boolean;
}

export interface CreateKnowledgeDraftFromIssueRequest {
  issue_id: string;
}

export interface CreateKnowledgeDraftFromCandidateRequest {
  candidate_id: string;
  regenerate?: boolean;
}
