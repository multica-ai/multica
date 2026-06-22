export type KnowledgeType = "lesson" | "playbook" | "reference";
export type KnowledgeLifecycleStatus =
  | "draft"
  | "reviewed"
  | "published"
  | "archived"
  | "deprecated";
export type KnowledgeConfidenceStatus = "low" | "medium" | "high";
export type KnowledgeFeedbackValue = "helpful" | "not_helpful" | "misleading" | "outdated";

export interface ProbeKnowledgeCuratorRequest {
  base_url: string;
  api_key?: string;
  model?: string;
  embedding_model?: string;
}

export interface ProbeKnowledgeCuratorResponse {
  provider: string;
  model: string;
  embedding_model: string;
  chat_supported: boolean;
  embedding_supported: boolean;
  warnings: string[];
}

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
  stale_score: number;
  effectiveness_score: number;
  conflict_group: string | null;
  review_reason: string | null;
  update_suggestion: string | null;
  review_needed_at: string | null;
  governance_checked_at: string | null;
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
  evidence: CandidateEvidence | null;
  evaluated_at: string;
  created_at: string;
  updated_at: string;
}

export interface CandidateEvidence {
  skip_check?: SkipCheck;
  retry_chain?: RetryChainEvidence;
  correction_rounds?: CorrectionRound[];
  pr_evidence?: PREvidenceItem[];
  similarity?: SimilarityEvidence;
}

export interface SkipCheck {
  evaluated: boolean;
  matched_rule: string | null;
}

export interface RetryFailure {
  attempt: number;
  status: string;
  error_summary: string;
}

export interface RetrySuccess {
  attempt: number;
  task_id: string;
  status: string;
}

export interface RetryChainEvidence {
  total_attempts: number;
  has_clear_error: boolean;
  failures: RetryFailure[];
  final_success: RetrySuccess | null;
}

export interface CorrectionRound {
  comment_id: string;
  comment_text: string;
  had_follow_up: boolean;
}

export interface PREvidenceItem {
  number: number;
  title: string;
  repo_owner: string;
  repo_name: string;
  state: string;
  merged_at: string;
  additions: number;
  deletions: number;
  changed_files: number;
}

export interface SimilarityMatch {
  knowledge_item_id: string;
  title: string;
  vector_score: number;
}

export interface SimilarityEvidence {
  top_matches: SimilarityMatch[];
  max_similarity: number;
}

export type KnowledgeGovernanceFindingType =
  | "stale"
  | "conflict"
  | "low_effectiveness"
  | "misleading"
  | "outdated";

export type KnowledgeGovernanceFindingStatus =
  | "open"
  | "drafted"
  | "accepted"
  | "rejected"
  | "dismissed"
  | "archived"
  | "deprecated";

export interface KnowledgeGovernanceFinding {
  id: string;
  workspace_id: string;
  knowledge_item_id: string;
  finding_type: KnowledgeGovernanceFindingType | string;
  status: KnowledgeGovernanceFindingStatus | string;
  severity: number;
  reason: string;
  evidence: unknown;
  suggested_action: string;
  source_map: unknown;
  draft_knowledge_item_id: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  dismissed_by: string | null;
  dismissed_at: string | null;
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
  source_type?: string | null;
  source_id?: string | null;
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

export interface KnowledgeAnalyticsRow {
  knowledge_item_id: string;
  title: string;
  type: string;
  lifecycle_status: string;
  retrieval_count: number;
  injection_count: number;
  injected_task_count: number;
  usage_count: number;
  agent_reference_count: number;
  active_search_count: number;
  helpful_count: number;
  not_helpful_count: number;
  misleading_count: number;
  outdated_count: number;
  latest_negative_feedback_at: string | null;
  successful_task_count: number;
  failed_task_count: number;
  total_task_seconds: number;
  total_tokens: number;
}

export interface ListKnowledgeAnalyticsParams {
  knowledge_item_id?: string | null;
  project_id?: string | null;
  agent_id?: string | null;
  since?: string | null;
  until?: string | null;
  include_zero?: boolean;
  limit?: number;
  offset?: number;
}

export interface ListKnowledgeAnalyticsResponse {
  items: KnowledgeAnalyticsRow[];
  total: number;
}

export interface KnowledgeEffectBucket {
  bucket_hour: string;
  workspace_id: string;
  agent_id: string;
  project_id: string | null;
  model: string;
  provider: string;
  task_kind: string;
  has_injection: boolean;
  task_count: number;
  successful_count: number;
  failed_count: number;
  total_duration_secs: number;
  duration_task_count: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  rerun_count: number;
  follow_up_count: number;
  max_attempt: number;
}

export interface KnowledgeEffectSummary {
  total_tasks: number;
  total_successful: number;
  total_failed: number;
  total_duration_secs: number;
  total_duration_tasks: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_read_tokens: number;
  total_cache_write_tokens: number;
  total_reruns: number;
  total_follow_ups: number;
}

export interface ListKnowledgeEffectParams {
  agent_id?: string | null;
  project_id?: string | null;
  task_kind?: string | null;
  has_injection?: boolean | null;
  model?: string | null;
  since?: string | null;
  until?: string | null;
  limit?: number;
  offset?: number;
}

export interface ListKnowledgeEffectResponse {
  buckets: KnowledgeEffectBucket[];
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

export interface ListKnowledgeGovernanceFindingsParams {
  knowledge_item_id?: string | null;
  status?: KnowledgeGovernanceFindingStatus | string;
  finding_type?: KnowledgeGovernanceFindingType | string;
  limit?: number;
  offset?: number;
}

export interface ListKnowledgeGovernanceFindingsResponse {
  findings: KnowledgeGovernanceFinding[];
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

export interface PublishKnowledgeToWikiRequest {
  wiki_page_id?: string | null;
  parent_id?: string | null;
  title?: string | null;
  content?: string | null;
}

export interface KnowledgeSkillPublishFile {
  path: string;
  content: string;
}

export interface PublishKnowledgeToSkillRequest {
  skill_id?: string | null;
  name?: string | null;
  description?: string | null;
  content?: string | null;
  include_source_map?: boolean;
  files?: KnowledgeSkillPublishFile[];
}

export interface CreateKnowledgeFeedbackRequest {
  value: KnowledgeFeedbackValue;
  note?: string | null;
  agent_task_id?: string | null;
}

export interface KnowledgeInjectionDetail {
  injection_event_id: string;
  knowledge_item_id: string;
  agent_task_id: string | null;
  injection_target: string;
  retrieval_event_id: string | null;
  rank: number | null;
  score: number | null;
  injection_reason: string | null;
  token_budget: number | null;
  injected_at: string;
  knowledge_title: string;
  knowledge_type: string;
  knowledge_lifecycle_status: string;
  was_used: boolean;
  source_issue_id: string | null;
}

export interface ListKnowledgeInjectionsResponse {
  injections: KnowledgeInjectionDetail[];
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

export interface CreateKnowledgeDraftFromGovernanceFindingRequest {
  finding_id: string;
  regenerate?: boolean;
}

export interface KnowledgeDraftDispatched {
  status: "queued";
  task_id: string;
  message: string;
}

export interface CuratorDraftTask {
  id: string;
  status: "queued" | "running" | "completed" | "failed";
  draft_kind: string;
  result?: unknown;
  error?: string;
}
