package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type KnowledgeItemResponse struct {
	ID                  string   `json:"id"`
	WorkspaceID         string   `json:"workspace_id"`
	ProjectID           *string  `json:"project_id"`
	AgentID             *string  `json:"agent_id"`
	Title               string   `json:"title"`
	Type                string   `json:"type"`
	DomainLabels        []string `json:"domain_labels"`
	ProblemPattern      string   `json:"problem_pattern"`
	TriggerConditions   string   `json:"trigger_conditions"`
	DiagnosticSteps     string   `json:"diagnostic_steps"`
	RecommendedPractice string   `json:"recommended_practice"`
	AntiPatterns        string   `json:"anti_patterns"`
	Applicability       string   `json:"applicability"`
	ConfidenceStatus    string   `json:"confidence_status"`
	LifecycleStatus     string   `json:"lifecycle_status"`
	CreatedBy           *string  `json:"created_by"`
	ReviewedBy          *string  `json:"reviewed_by"`
	ReviewedAt          *string  `json:"reviewed_at"`
	PublishedAt         *string  `json:"published_at"`
	ArchivedAt          *string  `json:"archived_at"`
	UpdatedBy           *string  `json:"updated_by"`
	DeprecatedAt        *string  `json:"deprecated_at"`
	StaleScore          float64  `json:"stale_score"`
	EffectivenessScore  float64  `json:"effectiveness_score"`
	ConflictGroup       *string  `json:"conflict_group"`
	ReviewReason        *string  `json:"review_reason"`
	UpdateSuggestion    *string  `json:"update_suggestion"`
	ReviewNeededAt      *string  `json:"review_needed_at"`
	GovernanceCheckedAt *string  `json:"governance_checked_at"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
}

type KnowledgeSourceResponse struct {
	ID              string  `json:"id"`
	KnowledgeItemID string  `json:"knowledge_item_id"`
	WorkspaceID     string  `json:"workspace_id"`
	SourceType      string  `json:"source_type"`
	SourceID        *string `json:"source_id"`
	SourceURL       *string `json:"source_url"`
	SourceTitle     *string `json:"source_title"`
	SourceExcerpt   *string `json:"source_excerpt"`
	CreatedAt       string  `json:"created_at"`
}

type KnowledgeSourceSummaryResponse struct {
	Count              int      `json:"count"`
	Types              []string `json:"types"`
	PrimarySourceType  string   `json:"primary_source_type"`
	PrimarySourceID    *string  `json:"primary_source_id"`
	PrimarySourceTitle string   `json:"primary_source_title"`
}

type KnowledgeSourceDetailResponse struct {
	KnowledgeSourceResponse
	ResolvedTitle *string `json:"resolved_title"`
	ResolvedURL   *string `json:"resolved_url"`
	ResolvedNote  *string `json:"resolved_note"`
}

type KnowledgePublishTargetResponse struct {
	ID              string  `json:"id"`
	KnowledgeItemID string  `json:"knowledge_item_id"`
	WorkspaceID     string  `json:"workspace_id"`
	TargetType      string  `json:"target_type"`
	TargetID        *string `json:"target_id"`
	TargetURL       *string `json:"target_url"`
	TargetTitle     *string `json:"target_title"`
	Metadata        any     `json:"metadata"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type KnowledgeEmbeddingMetadataResponse struct {
	ID              string `json:"id"`
	KnowledgeItemID string `json:"knowledge_item_id"`
	WorkspaceID     string `json:"workspace_id"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ContentHash     string `json:"content_hash"`
	EmbeddedAt      string `json:"embedded_at"`
	CreatedAt       string `json:"created_at"`
}

type KnowledgeFeedbackSummaryResponse struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type KnowledgeAnalyticsRowResponse struct {
	KnowledgeItemID          string  `json:"knowledge_item_id"`
	Title                    string  `json:"title"`
	Type                     string  `json:"type"`
	LifecycleStatus          string  `json:"lifecycle_status"`
	RetrievalCount           int64   `json:"retrieval_count"`
	InjectionCount           int64   `json:"injection_count"`
	InjectedTaskCount        int64   `json:"injected_task_count"`
	UsageCount               int64   `json:"usage_count"`
	AgentReferenceCount      int64   `json:"agent_reference_count"`
	ActiveSearchCount        int64   `json:"active_search_count"`
	HelpfulCount             int64   `json:"helpful_count"`
	NotHelpfulCount          int64   `json:"not_helpful_count"`
	MisleadingCount          int64   `json:"misleading_count"`
	OutdatedCount            int64   `json:"outdated_count"`
	LatestNegativeFeedbackAt *string `json:"latest_negative_feedback_at"`
	SuccessfulTaskCount      int64   `json:"successful_task_count"`
	FailedTaskCount          int64   `json:"failed_task_count"`
	TotalTaskSeconds         int64   `json:"total_task_seconds"`
	TotalTokens              int64   `json:"total_tokens"`
}

type KnowledgeEffectBucketResponse struct {
	BucketHour         string  `json:"bucket_hour"`
	WorkspaceID        string  `json:"workspace_id"`
	AgentID            string  `json:"agent_id"`
	ProjectID          *string `json:"project_id"`
	Model              string  `json:"model"`
	Provider           string  `json:"provider"`
	TaskKind           string  `json:"task_kind"`
	HasInjection       bool    `json:"has_injection"`
	TaskCount          int64   `json:"task_count"`
	SuccessfulCount    int64   `json:"successful_count"`
	FailedCount        int64   `json:"failed_count"`
	TotalDurationSecs  float64 `json:"total_duration_secs"`
	DurationTaskCount  int64   `json:"duration_task_count"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CacheWriteTokens   int64   `json:"cache_write_tokens"`
	RerunCount         int64   `json:"rerun_count"`
	FollowUpCount      int64   `json:"follow_up_count"`
	MaxAttempt         int32   `json:"max_attempt"`
}

type KnowledgeEffectSummaryResponse struct {
	TotalTasks            int64   `json:"total_tasks"`
	TotalSuccessful       int64   `json:"total_successful"`
	TotalFailed           int64   `json:"total_failed"`
	TotalDurationSecs     float64 `json:"total_duration_secs"`
	TotalDurationTasks    int64   `json:"total_duration_tasks"`
	TotalInputTokens      int64   `json:"total_input_tokens"`
	TotalOutputTokens     int64   `json:"total_output_tokens"`
	TotalCacheReadTokens  int64   `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int64   `json:"total_cache_write_tokens"`
	TotalReruns           int64   `json:"total_reruns"`
	TotalFollowUps        int64   `json:"total_follow_ups"`
}

type KnowledgeCandidateResponse struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	IssueID        string   `json:"issue_id"`
	CommentID      *string  `json:"comment_id"`
	AgentTaskID    *string  `json:"agent_task_id"`
	SourceType     string   `json:"source_type"`
	SourceID       string   `json:"source_id"`
	TriggerReason  string   `json:"trigger_reason"`
	SignalStrength string   `json:"signal_strength"`
	Signals        []string `json:"signals"`
	Score          int32    `json:"score"`
	Status         string   `json:"status"`
	DedupeKey      string   `json:"dedupe_key"`
	CreatedBy      *string  `json:"created_by"`
	Metadata       any      `json:"metadata"`
	EvaluatedAt    string   `json:"evaluated_at"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

type KnowledgeGovernanceFindingResponse struct {
	ID                   string  `json:"id"`
	WorkspaceID          string  `json:"workspace_id"`
	KnowledgeItemID      string  `json:"knowledge_item_id"`
	FindingType          string  `json:"finding_type"`
	Status               string  `json:"status"`
	Severity             int32   `json:"severity"`
	Reason               string  `json:"reason"`
	Evidence             any     `json:"evidence"`
	SuggestedAction      string  `json:"suggested_action"`
	SourceMap            any     `json:"source_map"`
	DraftKnowledgeItemID *string `json:"draft_knowledge_item_id"`
	ResolvedBy           *string `json:"resolved_by"`
	ResolvedAt           *string `json:"resolved_at"`
	DismissedBy          *string `json:"dismissed_by"`
	DismissedAt          *string `json:"dismissed_at"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type KnowledgeDetailResponse struct {
	Item            KnowledgeItemResponse                `json:"item"`
	Sources         []KnowledgeSourceResponse            `json:"sources"`
	SourceSummary   KnowledgeSourceSummaryResponse       `json:"source_summary"`
	PublishTargets  []KnowledgePublishTargetResponse     `json:"publish_targets"`
	Embeddings      []KnowledgeEmbeddingMetadataResponse `json:"embeddings"`
	FeedbackSummary []KnowledgeFeedbackSummaryResponse   `json:"feedback_summary"`
}

type KnowledgeSearchResultResponse struct {
	Item          KnowledgeItemResponse          `json:"item"`
	SourceSummary KnowledgeSourceSummaryResponse `json:"source_summary"`
	TextScore     float64                        `json:"text_score"`
	VectorScore   float64                        `json:"vector_score"`
	FinalScore    float64                        `json:"final_score"`
	MatchReason   string                         `json:"match_reason"`
}

type KnowledgeInjectionDetailResponse struct {
	InjectionEventID         string  `json:"injection_event_id"`
	KnowledgeItemID          string  `json:"knowledge_item_id"`
	AgentTaskID              *string `json:"agent_task_id"`
	InjectionTarget          string  `json:"injection_target"`
	RetrievalEventID         *string `json:"retrieval_event_id"`
	Rank                     *int32  `json:"rank"`
	Score                    *float64 `json:"score"`
	InjectionReason          *string `json:"injection_reason"`
	TokenBudget              *int32  `json:"token_budget"`
	InjectedAt               string  `json:"injected_at"`
	KnowledgeTitle           string  `json:"knowledge_title"`
	KnowledgeType            string  `json:"knowledge_type"`
	KnowledgeLifecycleStatus string  `json:"knowledge_lifecycle_status"`
	WasUsed                  bool    `json:"was_used"`
	SourceIssueID            *string `json:"source_issue_id"`
}

type listKnowledgeInjectionsResponse struct {
	Injections []KnowledgeInjectionDetailResponse `json:"injections"`
}

type createKnowledgeRequest struct {
	ProjectID           *string                `json:"project_id"`
	AgentID             *string                `json:"agent_id"`
	Title               string                 `json:"title"`
	Type                string                 `json:"type"`
	DomainLabels        []string               `json:"domain_labels"`
	ProblemPattern      string                 `json:"problem_pattern"`
	TriggerConditions   string                 `json:"trigger_conditions"`
	DiagnosticSteps     string                 `json:"diagnostic_steps"`
	RecommendedPractice string                 `json:"recommended_practice"`
	AntiPatterns        string                 `json:"anti_patterns"`
	Applicability       string                 `json:"applicability"`
	ConfidenceStatus    string                 `json:"confidence_status"`
	LifecycleStatus     string                 `json:"lifecycle_status"`
	Sources             []knowledgeSourceInput `json:"sources"`
}

type updateKnowledgeRequest struct {
	ProjectID           *string   `json:"project_id"`
	AgentID             *string   `json:"agent_id"`
	Title               *string   `json:"title"`
	Type                *string   `json:"type"`
	DomainLabels        *[]string `json:"domain_labels"`
	ProblemPattern      *string   `json:"problem_pattern"`
	TriggerConditions   *string   `json:"trigger_conditions"`
	DiagnosticSteps     *string   `json:"diagnostic_steps"`
	RecommendedPractice *string   `json:"recommended_practice"`
	AntiPatterns        *string   `json:"anti_patterns"`
	Applicability       *string   `json:"applicability"`
	ConfidenceStatus    *string   `json:"confidence_status"`
	LifecycleStatus     *string   `json:"lifecycle_status"`
}

type knowledgeSourceInput struct {
	SourceType    string  `json:"source_type"`
	SourceID      *string `json:"source_id"`
	SourceURL     *string `json:"source_url"`
	SourceTitle   *string `json:"source_title"`
	SourceExcerpt *string `json:"source_excerpt"`
}

type publishKnowledgeWikiRequest struct {
	WikiPageID *string `json:"wiki_page_id"`
	ParentID   *string `json:"parent_id"`
	Title      *string `json:"title"`
	Content    *string `json:"content"`
}

type publishKnowledgeSkillRequest struct {
	SkillID          *string                   `json:"skill_id"`
	Name             *string                   `json:"name"`
	Description      *string                   `json:"description"`
	Content          *string                   `json:"content"`
	IncludeSourceMap *bool                     `json:"include_source_map"`
	Files            []knowledgeSkillFileInput `json:"files"`
}

type knowledgeSkillFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type searchKnowledgeRequest struct {
	Query     string                 `json:"query"`
	IssueID   *string                `json:"issue_id"`
	Embedding []float32              `json:"embedding"`
	Limit     int32                  `json:"limit"`
	Filters   searchKnowledgeFilters `json:"filters"`
}

type searchKnowledgeFilters struct {
	ProjectID *string  `json:"project_id"`
	AgentID   *string  `json:"agent_id"`
	Labels    []string `json:"labels"`
	Types     []string `json:"types"`
	Statuses  []string `json:"statuses"`
}

type createKnowledgeFeedbackRequest struct {
	Value       string  `json:"value"`
	Note        *string `json:"note"`
	AgentTaskID *string `json:"agent_task_id"`
}

type evaluateKnowledgeCandidateRequest struct {
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id"`
	TriggerReason string `json:"trigger_reason"`
	Manual        *bool  `json:"manual"`
}

type createKnowledgeDraftFromIssueRequest struct {
	IssueID string `json:"issue_id"`
}

type createKnowledgeDraftFromCandidateRequest struct {
	Regenerate bool `json:"regenerate"`
}

type createKnowledgeDraftFromGovernanceFindingRequest struct {
	Regenerate bool `json:"regenerate"`
}

func (h *Handler) ListKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimitQuery(w, q.Get("limit"), 50, 50)
	if !ok {
		return
	}
	offset, ok := parseOffsetQuery(w, q.Get("offset"))
	if !ok {
		return
	}
	projectID, ok := parseOptionalUUIDQuery(w, q.Get("project_id"), "project_id")
	if !ok {
		return
	}
	agentID, ok := parseOptionalUUIDQuery(w, q.Get("agent_id"), "agent_id")
	if !ok {
		return
	}
	status := textFromString(q.Get("status"))
	includeInactive := q.Get("include_inactive") == "true"
	if status.Valid && (status.String == "archived" || status.String == "deprecated") {
		includeInactive = true
	}
	labels := append(splitCSV(q.Get("label")), splitCSV(q.Get("labels"))...)
	items, err := h.KnowledgeService.List(r.Context(), db.ListKnowledgeItemsParams{
		WorkspaceID:     wsUUID,
		IncludeInactive: includeInactive,
		Type:            textFromString(q.Get("type")),
		Status:          status,
		ProjectID:       projectID,
		AgentID:         agentID,
		Labels:          labels,
		Query:           textFromString(q.Get("q")),
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to list knowledge")
		return
	}
	resp := make([]KnowledgeItemResponse, len(items))
	for i, item := range items {
		resp[i] = knowledgeItemToResponse(item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": resp, "total": len(resp)})
}

func (h *Handler) GetKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	detail, err := h.KnowledgeService.GetDetail(r.Context(), wsUUID, itemID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to get knowledge")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeDetailToResponse(detail))
}

func (h *Handler) CreateKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req createKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectID, ok := parseOptionalUUIDPtr(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	agentID, ok := parseOptionalUUIDPtr(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	sources, ok := parseKnowledgeSources(w, req.Sources)
	if !ok {
		return
	}
	detail, err := h.KnowledgeService.Create(r.Context(), service.KnowledgeCreateParams{
		WorkspaceID:         wsUUID,
		ProjectID:           projectID,
		AgentID:             agentID,
		Title:               req.Title,
		Type:                req.Type,
		DomainLabels:        req.DomainLabels,
		ProblemPattern:      req.ProblemPattern,
		TriggerConditions:   req.TriggerConditions,
		DiagnosticSteps:     req.DiagnosticSteps,
		RecommendedPractice: req.RecommendedPractice,
		AntiPatterns:        req.AntiPatterns,
		Applicability:       req.Applicability,
		ConfidenceStatus:    req.ConfidenceStatus,
		LifecycleStatus:     "draft",
		CreatedBy:           member.ID,
		Sources:             sources,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to create knowledge")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, detail.Item.ID)
	writeJSON(w, http.StatusCreated, knowledgeDetailToResponse(detail))
}

func (h *Handler) UpdateKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req updateKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectID, ok := parseOptionalUUIDPtr(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	agentID, ok := parseOptionalUUIDPtr(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	var labels []string
	labelsSet := req.DomainLabels != nil
	if labelsSet {
		labels = *req.DomainLabels
	}
	var reviewedBy pgtype.UUID
	if req.LifecycleStatus != nil && (*req.LifecycleStatus == "reviewed" || *req.LifecycleStatus == "published") {
		reviewedBy = member.ID
	}
	item, err := h.KnowledgeService.Update(r.Context(), service.KnowledgeUpdateParams{
		ID:                  itemID,
		WorkspaceID:         wsUUID,
		ProjectID:           projectID,
		AgentID:             agentID,
		Title:               textFromPtr(req.Title),
		Type:                textFromPtr(req.Type),
		DomainLabels:        labels,
		DomainLabelsSet:     labelsSet,
		ProblemPattern:      textFromPtr(req.ProblemPattern),
		TriggerConditions:   textFromPtr(req.TriggerConditions),
		DiagnosticSteps:     textFromPtr(req.DiagnosticSteps),
		RecommendedPractice: textFromPtr(req.RecommendedPractice),
		AntiPatterns:        textFromPtr(req.AntiPatterns),
		Applicability:       textFromPtr(req.Applicability),
		ConfidenceStatus:    textFromPtr(req.ConfidenceStatus),
		LifecycleStatus:     textFromPtr(req.LifecycleStatus),
		ReviewedBy:          reviewedBy,
		UpdatedBy:           member.ID,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to update knowledge")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeItemToResponse(item))
}

func (h *Handler) DeleteKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	if _, err := h.KnowledgeService.Archive(r.Context(), wsUUID, itemID, member.ID); err != nil {
		h.writeKnowledgeError(w, err, "failed to archive knowledge")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ReviewKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	item, err := h.KnowledgeService.Review(r.Context(), wsUUID, itemID, member.ID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to review knowledge")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, itemID)
	writeJSON(w, http.StatusOK, knowledgeItemToResponse(item))
}

func (h *Handler) PublishKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	detail, err := h.KnowledgeService.Publish(r.Context(), wsUUID, itemID, member.ID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to publish knowledge")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, itemID)
	writeJSON(w, http.StatusOK, knowledgeDetailToResponse(detail))
}

func (h *Handler) ArchiveKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	item, err := h.KnowledgeService.Archive(r.Context(), wsUUID, itemID, member.ID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to archive knowledge")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeItemToResponse(item))
}

func (h *Handler) DeprecateKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	item, err := h.KnowledgeService.Deprecate(r.Context(), wsUUID, itemID, member.ID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to deprecate knowledge")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeItemToResponse(item))
}

func (h *Handler) RestoreKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	item, err := h.KnowledgeService.Restore(r.Context(), wsUUID, itemID, member.ID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to restore knowledge")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, itemID)
	writeJSON(w, http.StatusOK, knowledgeItemToResponse(item))
}

func (h *Handler) DismissKnowledgeGovernance(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	item, err := h.KnowledgeService.DismissGovernance(r.Context(), wsUUID, itemID, member.ID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to dismiss knowledge governance finding")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeItemToResponse(item))
}

func (h *Handler) PublishKnowledgeToWiki(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req publishKnowledgeWikiRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	wikiPageID, ok := parseOptionalUUIDPtr(w, req.WikiPageID, "wiki_page_id")
	if !ok {
		return
	}
	parentID, ok := parseOptionalUUIDPtr(w, req.ParentID, "parent_id")
	if !ok {
		return
	}
	detail, err := h.KnowledgeService.PublishToWiki(r.Context(), service.KnowledgePublishWikiParams{
		WorkspaceID: wsUUID,
		ItemID:      itemID,
		ActorID:     member.ID,
		ActorUserID: member.UserID,
		WikiPageID:  wikiPageID,
		ParentID:    parentID,
		Title:       stringFromPtr(req.Title),
		Content:     stringFromPtr(req.Content),
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to publish knowledge to wiki")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, itemID)
	writeJSON(w, http.StatusOK, knowledgeDetailToResponse(detail))
}

func (h *Handler) PublishKnowledgeToSkill(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req publishKnowledgeSkillRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	skillID, ok := parseOptionalUUIDPtr(w, req.SkillID, "skill_id")
	if !ok {
		return
	}
	files := make([]service.KnowledgeSkillFileInput, 0, len(req.Files))
	for _, file := range req.Files {
		files = append(files, service.KnowledgeSkillFileInput{Path: file.Path, Content: file.Content})
	}
	includeSourceMap := true
	if req.IncludeSourceMap != nil {
		includeSourceMap = *req.IncludeSourceMap
	}
	detail, err := h.KnowledgeService.PublishToSkill(r.Context(), service.KnowledgePublishSkillParams{
		WorkspaceID:      wsUUID,
		ItemID:           itemID,
		ActorID:          member.ID,
		ActorUserID:      member.UserID,
		SkillID:          skillID,
		Name:             stringFromPtr(req.Name),
		Description:      stringFromPtr(req.Description),
		Content:          stringFromPtr(req.Content),
		IncludeSourceMap: includeSourceMap,
		SupportingFiles:  files,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to publish knowledge to skill")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, itemID)
	writeJSON(w, http.StatusOK, knowledgeDetailToResponse(detail))
}

func (h *Handler) GetKnowledgeSources(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	sources, err := h.KnowledgeService.GetSourceDetails(r.Context(), wsUUID, itemID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to get knowledge sources")
		return
	}
	resp := make([]KnowledgeSourceDetailResponse, len(sources))
	for i, source := range sources {
		resp[i] = knowledgeSourceDetailToResponse(source)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": resp, "total": len(resp)})
}

func (h *Handler) SearchKnowledge(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req searchKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectID, ok := parseOptionalUUIDPtr(w, req.Filters.ProjectID, "project_id")
	if !ok {
		return
	}
	agentID, ok := parseOptionalUUIDPtr(w, req.Filters.AgentID, "agent_id")
	if !ok {
		return
	}
	var issue *db.Issue
	if req.IssueID != nil && strings.TrimSpace(*req.IssueID) != "" {
		loaded, ok := h.loadIssueForUser(w, r, strings.TrimSpace(*req.IssueID))
		if !ok {
			return
		}
		issue = &loaded
	}
	agentTaskID := pgtype.UUID{}
	querySource := "interactive"
	actorType, _ := h.resolveActor(r, uuidToString(member.UserID), h.resolveWorkspaceID(r))
	if actorType == "agent" {
		if taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID")); err == nil {
			agentTaskID = taskID
			querySource = "agent_search"
		}
	}
	results, err := h.KnowledgeService.Search(r.Context(), service.KnowledgeSearchParams{
		WorkspaceID: wsUUID,
		MemberID:    member.ID,
		AgentTaskID: agentTaskID,
		Query:       req.Query,
		QuerySource: querySource,
		Embedding:   req.Embedding,
		Limit:       req.Limit,
		Issue:       issue,
		Filters: service.KnowledgeSearchFilters{
			ProjectID: projectID,
			AgentID:   agentID,
			Labels:    req.Filters.Labels,
			Types:     req.Filters.Types,
			Statuses:  req.Filters.Statuses,
		},
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to search knowledge")
		return
	}
	resp := make([]KnowledgeSearchResultResponse, len(results))
	for i, result := range results {
		resp[i] = KnowledgeSearchResultResponse{
			Item:          knowledgeItemToResponse(result.Item),
			SourceSummary: knowledgeSourceSummaryToResponse(result.SourceSummary),
			TextScore:     result.TextScore,
			VectorScore:   result.VectorScore,
			FinalScore:    result.FinalScore,
			MatchReason:   result.MatchReason,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": resp, "total": len(resp)})
}

func (h *Handler) GetKnowledgeAnalytics(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r)); !ok {
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimitQuery(w, q.Get("limit"), 50, 100)
	if !ok {
		return
	}
	offset, ok := parseOffsetQuery(w, q.Get("offset"))
	if !ok {
		return
	}
	projectID, ok := parseOptionalUUIDQuery(w, q.Get("project_id"), "project_id")
	if !ok {
		return
	}
	agentID, ok := parseOptionalUUIDQuery(w, q.Get("agent_id"), "agent_id")
	if !ok {
		return
	}
	itemID := pgtype.UUID{}
	if rawID := strings.TrimSpace(chi.URLParam(r, "id")); rawID != "" {
		itemID, ok = parseUUIDOrBadRequest(w, rawID, "knowledge id")
		if !ok {
			return
		}
	} else if rawID := strings.TrimSpace(q.Get("knowledge_item_id")); rawID != "" {
		itemID, ok = parseUUIDOrBadRequest(w, rawID, "knowledge_item_id")
		if !ok {
			return
		}
	}
	since, ok := parseKnowledgeTimeQuery(w, q.Get("since"), time.Now().AddDate(0, 0, -30))
	if !ok {
		return
	}
	until, ok := parseKnowledgeTimeQuery(w, q.Get("until"), time.Now().Add(24*time.Hour))
	if !ok {
		return
	}
	includeZero := q.Get("include_zero") == "true" || itemID.Valid
	rows, err := h.KnowledgeService.ListAnalytics(r.Context(), service.KnowledgeAnalyticsParams{
		WorkspaceID:     wsUUID,
		KnowledgeItemID: itemID,
		ProjectID:       projectID,
		AgentID:         agentID,
		Since:           since,
		Until:           until,
		IncludeZero:     includeZero,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to get knowledge analytics")
		return
	}
	resp := make([]KnowledgeAnalyticsRowResponse, len(rows))
	for i, row := range rows {
		resp[i] = knowledgeAnalyticsRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": resp, "total": len(resp)})
}

func (h *Handler) GetKnowledgeEffect(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r)); !ok {
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimitQuery(w, q.Get("limit"), 100, 500)
	if !ok {
		return
	}
	offset, ok := parseOffsetQuery(w, q.Get("offset"))
	if !ok {
		return
	}
	agentID, ok := parseOptionalUUIDQuery(w, q.Get("agent_id"), "agent_id")
	if !ok {
		return
	}
	projectID, ok := parseOptionalUUIDQuery(w, q.Get("project_id"), "project_id")
	if !ok {
		return
	}
	hasInjection := pgtype.Bool{}
	if v := q.Get("has_injection"); v == "true" {
		hasInjection = pgtype.Bool{Bool: true, Valid: true}
	} else if v == "false" {
		hasInjection = pgtype.Bool{Bool: false, Valid: true}
	}
	taskKind := pgtype.Text{}
	if v := strings.TrimSpace(q.Get("task_kind")); v != "" {
		taskKind = pgtype.Text{String: v, Valid: true}
	}
	model := pgtype.Text{}
	if v := strings.TrimSpace(q.Get("model")); v != "" {
		model = pgtype.Text{String: v, Valid: true}
	}
	since, ok := parseKnowledgeTimeQuery(w, q.Get("since"), time.Now().AddDate(0, 0, -30))
	if !ok {
		return
	}
	until, ok := parseKnowledgeTimeQuery(w, q.Get("until"), time.Now().Add(24*time.Hour))
	if !ok {
		return
	}
	rows, err := h.KnowledgeService.ListKnowledgeEffect(r.Context(), service.KnowledgeEffectParams{
		WorkspaceID:  wsUUID,
		AgentID:      agentID,
		ProjectID:    projectID,
		TaskKind:     taskKind,
		HasInjection: hasInjection,
		Model:        model,
		Since:        since,
		Until:        until,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to list knowledge effect")
		return
	}
	resp := make([]KnowledgeEffectBucketResponse, len(rows))
	for i, row := range rows {
		resp[i] = knowledgeEffectBucketToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": resp, "total": len(resp)})
}

func (h *Handler) GetKnowledgeEffectSummary(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r)); !ok {
		return
	}
	q := r.URL.Query()
	agentID, ok := parseOptionalUUIDQuery(w, q.Get("agent_id"), "agent_id")
	if !ok {
		return
	}
	projectID, ok := parseOptionalUUIDQuery(w, q.Get("project_id"), "project_id")
	if !ok {
		return
	}
	hasInjection := pgtype.Bool{}
	if v := q.Get("has_injection"); v == "true" {
		hasInjection = pgtype.Bool{Bool: true, Valid: true}
	} else if v == "false" {
		hasInjection = pgtype.Bool{Bool: false, Valid: true}
	}
	taskKind := pgtype.Text{}
	if v := strings.TrimSpace(q.Get("task_kind")); v != "" {
		taskKind = pgtype.Text{String: v, Valid: true}
	}
	model := pgtype.Text{}
	if v := strings.TrimSpace(q.Get("model")); v != "" {
		model = pgtype.Text{String: v, Valid: true}
	}
	since, ok := parseKnowledgeTimeQuery(w, q.Get("since"), time.Now().AddDate(0, 0, -30))
	if !ok {
		return
	}
	until, ok := parseKnowledgeTimeQuery(w, q.Get("until"), time.Now().Add(24*time.Hour))
	if !ok {
		return
	}
	summary, err := h.KnowledgeService.GetKnowledgeEffectSummary(r.Context(), service.KnowledgeEffectSummaryParams{
		WorkspaceID:  wsUUID,
		AgentID:      agentID,
		ProjectID:    projectID,
		TaskKind:     taskKind,
		HasInjection: hasInjection,
		Model:        model,
		Since:        since,
		Until:        until,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to get knowledge effect summary")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeEffectSummaryToResponse(summary))
}

func (h *Handler) ListKnowledgeCandidates(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimitQuery(w, q.Get("limit"), 50, 100)
	if !ok {
		return
	}
	offset, ok := parseOffsetQuery(w, q.Get("offset"))
	if !ok {
		return
	}
	issueID, ok := parseOptionalUUIDQuery(w, q.Get("issue_id"), "issue_id")
	if !ok {
		return
	}
	candidates, err := h.KnowledgeService.ListCandidates(r.Context(), db.ListKnowledgeCandidatesParams{
		WorkspaceID: wsUUID,
		Status:      textFromString(q.Get("status")),
		SourceType:  textFromString(q.Get("source_type")),
		IssueID:     issueID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to list knowledge candidates")
		return
	}
	resp := make([]KnowledgeCandidateResponse, len(candidates))
	for i, candidate := range candidates {
		resp[i] = knowledgeCandidateToResponse(candidate)
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": resp, "total": len(resp)})
}

func (h *Handler) ListKnowledgeGovernanceFindings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r)); !ok {
		return
	}
	q := r.URL.Query()
	limit, ok := parseLimitQuery(w, q.Get("limit"), 50, 100)
	if !ok {
		return
	}
	offset, ok := parseOffsetQuery(w, q.Get("offset"))
	if !ok {
		return
	}
	itemID, ok := parseOptionalUUIDQuery(w, q.Get("knowledge_item_id"), "knowledge_item_id")
	if !ok {
		return
	}
	findings, err := h.KnowledgeService.ListGovernanceFindings(r.Context(), service.KnowledgeGovernanceFindingListParams{
		WorkspaceID:     wsUUID,
		Status:          q.Get("status"),
		FindingType:     q.Get("finding_type"),
		KnowledgeItemID: itemID,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to list knowledge governance findings")
		return
	}
	resp := make([]KnowledgeGovernanceFindingResponse, len(findings))
	for i, finding := range findings {
		resp[i] = knowledgeGovernanceFindingToResponse(finding)
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": resp, "total": len(resp)})
}

func (h *Handler) EvaluateKnowledgeCandidate(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req evaluateKnowledgeCandidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sourceID, ok := parseUUIDOrBadRequest(w, req.SourceID, "source_id")
	if !ok {
		return
	}
	manual := true
	if req.Manual != nil {
		manual = *req.Manual
	}
	candidate, err := h.KnowledgeService.EvaluateCandidate(r.Context(), service.KnowledgeCandidateEvaluateParams{
		WorkspaceID:    wsUUID,
		SourceType:     req.SourceType,
		SourceID:       sourceID,
		TriggerReason:  req.TriggerReason,
		Manual:         manual,
		CreatedBy:      member.ID,
		AdditionalMeta: map[string]any{"entrypoint": "api"},
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to evaluate knowledge candidate")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeCandidateToResponse(candidate))
}

func (h *Handler) CreateKnowledgeDraftFromIssue(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req createKnowledgeDraftFromIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	detail, err := h.KnowledgeCurator.GenerateDraftFromIssue(r.Context(), service.CuratorIssueDraftParams{
		WorkspaceID: wsUUID,
		IssueID:     issueID,
		CreatedBy:   member.ID,
	})
	if err != nil {
		if errors.Is(err, service.ErrCuratorDraftDispatched) {
			writeJSON(w, http.StatusAccepted, curatorDraftDispatchedResponse())
			return
		}
		h.writeKnowledgeError(w, err, "failed to create knowledge draft")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, detail.Item.ID)
	writeJSON(w, http.StatusCreated, knowledgeDetailToResponse(detail))
}

func (h *Handler) CreateKnowledgeDraftFromCandidate(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	candidateID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "candidate id")
	if !ok {
		return
	}
	var req createKnowledgeDraftFromCandidateRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if !errors.Is(err, io.EOF) {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		}
	}
	detail, err := h.KnowledgeCurator.GenerateDraftFromCandidate(r.Context(), service.CuratorCandidateDraftParams{
		WorkspaceID: wsUUID,
		CandidateID: candidateID,
		CreatedBy:   member.ID,
		Regenerate:  req.Regenerate,
	})
	if err != nil {
		if errors.Is(err, service.ErrCuratorDraftDispatched) {
			writeJSON(w, http.StatusAccepted, curatorDraftDispatchedResponse())
			return
		}
		h.writeKnowledgeError(w, err, "failed to create knowledge draft")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, detail.Item.ID)
	writeJSON(w, http.StatusCreated, knowledgeDetailToResponse(detail))
}

func (h *Handler) CreateKnowledgeDraftFromGovernanceFinding(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	findingID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "governance finding id")
	if !ok {
		return
	}
	var req createKnowledgeDraftFromGovernanceFindingRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	detail, err := h.KnowledgeCurator.GenerateDraftFromGovernanceFinding(r.Context(), service.CuratorGovernanceDraftParams{
		WorkspaceID: wsUUID,
		FindingID:   findingID,
		CreatedBy:   member.ID,
		Regenerate:  req.Regenerate,
	})
	if err != nil {
		if errors.Is(err, service.ErrCuratorDraftDispatched) {
			writeJSON(w, http.StatusAccepted, curatorDraftDispatchedResponse())
			return
		}
		h.writeKnowledgeError(w, err, "failed to create governance update draft")
		return
	}
	h.maybeEnsureKnowledgeEmbedding(r.Context(), wsUUID, detail.Item.ID)
	writeJSON(w, http.StatusCreated, knowledgeDetailToResponse(detail))
}

func (h *Handler) ResolveKnowledgeGovernanceFinding(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	findingID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "governance finding id")
	if !ok {
		return
	}
	action := chi.URLParam(r, "action")
	finding, err := h.KnowledgeService.ResolveGovernanceFinding(r.Context(), service.KnowledgeGovernanceFindingActionParams{
		WorkspaceID: wsUUID,
		FindingID:   findingID,
		ActorID:     member.ID,
		Action:      action,
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to update knowledge governance finding")
		return
	}
	writeJSON(w, http.StatusOK, knowledgeGovernanceFindingToResponse(finding))
}

func (h *Handler) CreateKnowledgeFeedback(w http.ResponseWriter, r *http.Request) {
	wsUUID, itemID, ok := h.parseKnowledgePath(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}
	var req createKnowledgeFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentTaskID, ok := parseOptionalUUIDPtr(w, req.AgentTaskID, "agent_task_id")
	if !ok {
		return
	}
	feedback, err := h.KnowledgeService.AddFeedback(r.Context(), service.KnowledgeFeedbackParams{
		KnowledgeItemID: itemID,
		WorkspaceID:     wsUUID,
		MemberID:        member.ID,
		AgentTaskID:     agentTaskID,
		Value:           req.Value,
		Note:            textFromPtr(req.Note),
	})
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to create feedback")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                uuidToString(feedback.ID),
		"knowledge_item_id": uuidToString(feedback.KnowledgeItemID),
		"workspace_id":      uuidToString(feedback.WorkspaceID),
		"member_id":         uuidToString(feedback.MemberID),
		"agent_task_id":     uuidToPtr(feedback.AgentTaskID),
		"value":             feedback.Value,
		"note":              textToPtr(feedback.Note),
		"created_at":        timestampToString(feedback.CreatedAt),
	})
}

// ListKnowledgeInjectionsByIssue returns all non-discarded knowledge injection
// events for a given issue, ordered by rank then score.
func (h *Handler) ListKnowledgeInjectionsByIssue(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue id")
	if !ok {
		return
	}
	// Verify the issue exists and the user has access.
	if _, ok := h.loadIssueForUser(w, r, util.UUIDToString(issueID)); !ok {
		return
	}
	rows, err := h.KnowledgeService.ListKnowledgeInjectionsByIssue(r.Context(), wsUUID, issueID)
	if err != nil {
		h.writeKnowledgeError(w, err, "failed to list knowledge injections")
		return
	}
	injections := make([]KnowledgeInjectionDetailResponse, 0, len(rows))
	for _, row := range rows {
		injections = append(injections, KnowledgeInjectionDetailResponse{
			InjectionEventID:         uuidToString(row.InjectionEventID),
			KnowledgeItemID:          uuidToString(row.KnowledgeItemID),
			AgentTaskID:              uuidToPtr(row.AgentTaskID),
			InjectionTarget:          row.InjectionTarget,
			RetrievalEventID:         uuidToPtr(row.RetrievalEventID),
			Rank:                     int4ToPtr(row.Rank),
			Score:                    float8ToPtr(row.Score),
			InjectionReason:          textToPtr(row.InjectionReason),
			TokenBudget:              int4ToPtr(row.TokenBudget),
			InjectedAt:               timestampToString(row.InjectedAt),
			KnowledgeTitle:           row.KnowledgeTitle,
			KnowledgeType:            row.KnowledgeType,
			KnowledgeLifecycleStatus: row.KnowledgeLifecycleStatus,
			WasUsed:                  row.WasUsed,
			SourceIssueID:            uuidToPtr(row.SourceIssueID),
		})
	}
	writeJSON(w, http.StatusOK, listKnowledgeInjectionsResponse{Injections: injections})
}

func (h *Handler) parseKnowledgePath(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	itemID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "knowledge id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsUUID, itemID, true
}

func curatorDraftDispatchedResponse() map[string]any {
	return map[string]any{
		"status":  "queued",
		"message": "Knowledge curator draft dispatched to local runtime. Poll GET /api/knowledge/curator-drafts/{taskId} for status.",
	}
}

func (h *Handler) writeKnowledgeError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, service.ErrKnowledgeValidation) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, service.ErrKnowledgeNotFound) {
		writeError(w, http.StatusNotFound, "knowledge not found")
		return
	}
	if errors.Is(err, service.ErrCuratorEngineUnavailable) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if errors.Is(err, service.ErrCuratorLocalRuntimeUnavailable) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, fallback)
}

func (h *Handler) maybeEnsureKnowledgeEmbedding(ctx context.Context, workspaceID, itemID pgtype.UUID) {
	if h.KnowledgeCurator == nil {
		return
	}
	go func() {
		if _, err := h.KnowledgeCurator.EnsureKnowledgeEmbedding(context.WithoutCancel(ctx), workspaceID, itemID); err != nil && !errors.Is(err, service.ErrCuratorEngineUnavailable) {
			// Best-effort: search still works through text ranking if embeddings fail.
		}
	}()
}

func knowledgeDetailToResponse(detail service.KnowledgeDetail) KnowledgeDetailResponse {
	sources := make([]KnowledgeSourceResponse, len(detail.Sources))
	for i, source := range detail.Sources {
		sources[i] = knowledgeSourceToResponse(source)
	}
	embeddings := make([]KnowledgeEmbeddingMetadataResponse, len(detail.Embeddings))
	for i, embedding := range detail.Embeddings {
		embeddings[i] = knowledgeEmbeddingMetadataToResponse(embedding)
	}
	feedback := make([]KnowledgeFeedbackSummaryResponse, len(detail.FeedbackSummary))
	for i, row := range detail.FeedbackSummary {
		feedback[i] = KnowledgeFeedbackSummaryResponse{Value: row.Value, Count: row.Count}
	}
	targets := make([]KnowledgePublishTargetResponse, len(detail.PublishTargets))
	for i, target := range detail.PublishTargets {
		targets[i] = knowledgePublishTargetToResponse(target)
	}
	return KnowledgeDetailResponse{
		Item:            knowledgeItemToResponse(detail.Item),
		Sources:         sources,
		SourceSummary:   knowledgeSourceSummaryToResponse(detail.SourceSummary),
		PublishTargets:  targets,
		Embeddings:      embeddings,
		FeedbackSummary: feedback,
	}
}

func knowledgeItemToResponse(item db.KnowledgeItem) KnowledgeItemResponse {
	return KnowledgeItemResponse{
		ID:                  uuidToString(item.ID),
		WorkspaceID:         uuidToString(item.WorkspaceID),
		ProjectID:           uuidToPtr(item.ProjectID),
		AgentID:             uuidToPtr(item.AgentID),
		Title:               item.Title,
		Type:                item.Type,
		DomainLabels:        item.DomainLabels,
		ProblemPattern:      item.ProblemPattern,
		TriggerConditions:   item.TriggerConditions,
		DiagnosticSteps:     item.DiagnosticSteps,
		RecommendedPractice: item.RecommendedPractice,
		AntiPatterns:        item.AntiPatterns,
		Applicability:       item.Applicability,
		ConfidenceStatus:    item.ConfidenceStatus,
		LifecycleStatus:     item.LifecycleStatus,
		CreatedBy:           uuidToPtr(item.CreatedBy),
		ReviewedBy:          uuidToPtr(item.ReviewedBy),
		ReviewedAt:          timestampPtr(item.ReviewedAt),
		PublishedAt:         timestampPtr(item.PublishedAt),
		ArchivedAt:          timestampPtr(item.ArchivedAt),
		UpdatedBy:           uuidToPtr(item.UpdatedBy),
		DeprecatedAt:        timestampPtr(item.DeprecatedAt),
		StaleScore:          numericToFloat(item.StaleScore, 0),
		EffectivenessScore:  numericToFloat(item.EffectivenessScore, 100),
		ConflictGroup:       textToPtr(item.ConflictGroup),
		ReviewReason:        textToPtr(item.ReviewReason),
		UpdateSuggestion:    textToPtr(item.UpdateSuggestion),
		ReviewNeededAt:      timestampPtr(item.ReviewNeededAt),
		GovernanceCheckedAt: timestampPtr(item.GovernanceCheckedAt),
		CreatedAt:           timestampToString(item.CreatedAt),
		UpdatedAt:           timestampToString(item.UpdatedAt),
	}
}

func knowledgeSourceToResponse(source db.KnowledgeSource) KnowledgeSourceResponse {
	return KnowledgeSourceResponse{
		ID:              uuidToString(source.ID),
		KnowledgeItemID: uuidToString(source.KnowledgeItemID),
		WorkspaceID:     uuidToString(source.WorkspaceID),
		SourceType:      source.SourceType,
		SourceID:        uuidToPtr(source.SourceID),
		SourceURL:       textToPtr(source.SourceUrl),
		SourceTitle:     textToPtr(source.SourceTitle),
		SourceExcerpt:   textToPtr(source.SourceExcerpt),
		CreatedAt:       timestampToString(source.CreatedAt),
	}
}

func knowledgeSourceSummaryToResponse(summary service.KnowledgeSourceSummary) KnowledgeSourceSummaryResponse {
	return KnowledgeSourceSummaryResponse{
		Count:              summary.Count,
		Types:              summary.Types,
		PrimarySourceType:  summary.PrimarySourceType,
		PrimarySourceID:    uuidToPtr(summary.PrimarySourceID),
		PrimarySourceTitle: summary.PrimarySourceTitle,
	}
}

func knowledgeSourceDetailToResponse(detail service.KnowledgeSourceDetail) KnowledgeSourceDetailResponse {
	return KnowledgeSourceDetailResponse{
		KnowledgeSourceResponse: knowledgeSourceToResponse(detail.Source),
		ResolvedTitle:           textToPtr(detail.ResolvedTitle),
		ResolvedURL:             textToPtr(detail.ResolvedURL),
		ResolvedNote:            textToPtr(detail.ResolvedNote),
	}
}

func knowledgePublishTargetToResponse(target db.KnowledgePublishTarget) KnowledgePublishTargetResponse {
	var metadata any = map[string]any{}
	if len(target.Metadata) > 0 {
		if err := json.Unmarshal(target.Metadata, &metadata); err != nil {
			metadata = map[string]any{}
		}
	}
	return KnowledgePublishTargetResponse{
		ID:              uuidToString(target.ID),
		KnowledgeItemID: uuidToString(target.KnowledgeItemID),
		WorkspaceID:     uuidToString(target.WorkspaceID),
		TargetType:      target.TargetType,
		TargetID:        uuidToPtr(target.TargetID),
		TargetURL:       textToPtr(target.TargetUrl),
		TargetTitle:     textToPtr(target.TargetTitle),
		Metadata:        metadata,
		CreatedBy:       uuidToPtr(target.CreatedBy),
		CreatedAt:       timestampToString(target.CreatedAt),
		UpdatedAt:       timestampToString(target.UpdatedAt),
	}
}

func knowledgeEmbeddingMetadataToResponse(embedding db.ListKnowledgeEmbeddingMetadataRow) KnowledgeEmbeddingMetadataResponse {
	return KnowledgeEmbeddingMetadataResponse{
		ID:              uuidToString(embedding.ID),
		KnowledgeItemID: uuidToString(embedding.KnowledgeItemID),
		WorkspaceID:     uuidToString(embedding.WorkspaceID),
		Provider:        embedding.Provider,
		Model:           embedding.Model,
		ContentHash:     embedding.ContentHash,
		EmbeddedAt:      timestampToString(embedding.EmbeddedAt),
		CreatedAt:       timestampToString(embedding.CreatedAt),
	}
}

func knowledgeCandidateToResponse(candidate db.KnowledgeCandidate) KnowledgeCandidateResponse {
	var metadata any = map[string]any{}
	if len(candidate.Metadata) > 0 {
		if err := json.Unmarshal(candidate.Metadata, &metadata); err != nil {
			metadata = map[string]any{}
		}
	}
	return KnowledgeCandidateResponse{
		ID:             uuidToString(candidate.ID),
		WorkspaceID:    uuidToString(candidate.WorkspaceID),
		IssueID:        uuidToString(candidate.IssueID),
		CommentID:      uuidToPtr(candidate.CommentID),
		AgentTaskID:    uuidToPtr(candidate.AgentTaskID),
		SourceType:     candidate.SourceType,
		SourceID:       uuidToString(candidate.SourceID),
		TriggerReason:  candidate.TriggerReason,
		SignalStrength: candidate.SignalStrength,
		Signals:        candidate.Signals,
		Score:          candidate.Score,
		Status:         candidate.Status,
		DedupeKey:      candidate.DedupeKey,
		CreatedBy:      uuidToPtr(candidate.CreatedBy),
		Metadata:       metadata,
		EvaluatedAt:    timestampToString(candidate.EvaluatedAt),
		CreatedAt:      timestampToString(candidate.CreatedAt),
		UpdatedAt:      timestampToString(candidate.UpdatedAt),
	}
}

func knowledgeGovernanceFindingToResponse(finding db.KnowledgeGovernanceFinding) KnowledgeGovernanceFindingResponse {
	return KnowledgeGovernanceFindingResponse{
		ID:                   uuidToString(finding.ID),
		WorkspaceID:          uuidToString(finding.WorkspaceID),
		KnowledgeItemID:      uuidToString(finding.KnowledgeItemID),
		FindingType:          finding.FindingType,
		Status:               finding.Status,
		Severity:             finding.Severity,
		Reason:               finding.Reason,
		Evidence:             jsonObjectOrEmpty(finding.Evidence),
		SuggestedAction:      finding.SuggestedAction,
		SourceMap:            jsonObjectOrEmpty(finding.SourceMap),
		DraftKnowledgeItemID: uuidToPtr(finding.DraftKnowledgeItemID),
		ResolvedBy:           uuidToPtr(finding.ResolvedBy),
		ResolvedAt:           timestampPtr(finding.ResolvedAt),
		DismissedBy:          uuidToPtr(finding.DismissedBy),
		DismissedAt:          timestampPtr(finding.DismissedAt),
		CreatedAt:            timestampToString(finding.CreatedAt),
		UpdatedAt:            timestampToString(finding.UpdatedAt),
	}
}

func knowledgeAnalyticsRowToResponse(row db.ListKnowledgeAnalyticsRow) KnowledgeAnalyticsRowResponse {
	return KnowledgeAnalyticsRowResponse{
		KnowledgeItemID:          uuidToString(row.KnowledgeItemID),
		Title:                    row.Title,
		Type:                     row.Type,
		LifecycleStatus:          row.LifecycleStatus,
		RetrievalCount:           row.RetrievalCount,
		InjectionCount:           row.InjectionCount,
		InjectedTaskCount:        row.InjectedTaskCount,
		UsageCount:               row.UsageCount,
		AgentReferenceCount:      row.AgentReferenceCount,
		ActiveSearchCount:        row.ActiveSearchCount,
		HelpfulCount:             row.HelpfulCount,
		NotHelpfulCount:          row.NotHelpfulCount,
		MisleadingCount:          row.MisleadingCount,
		OutdatedCount:            row.OutdatedCount,
		LatestNegativeFeedbackAt: timestampPtr(row.LatestNegativeFeedbackAt),
		SuccessfulTaskCount:      row.SuccessfulTaskCount,
		FailedTaskCount:          row.FailedTaskCount,
		TotalTaskSeconds:         row.TotalTaskSeconds,
		TotalTokens:              row.TotalTokens,
	}
}

func knowledgeEffectBucketToResponse(row db.ListKnowledgeEffectHourlyRow) KnowledgeEffectBucketResponse {
	return KnowledgeEffectBucketResponse{
		BucketHour:        timestampToString(row.BucketHour),
		WorkspaceID:       uuidToString(row.WorkspaceID),
		AgentID:           uuidToString(row.AgentID),
		ProjectID:         uuidToPtr(row.ProjectID),
		Model:             row.Model,
		Provider:          row.Provider,
		TaskKind:          row.TaskKind,
		HasInjection:      row.HasInjection,
		TaskCount:         row.TaskCount,
		SuccessfulCount:   row.SuccessfulCount,
		FailedCount:       row.FailedCount,
		TotalDurationSecs: row.TotalDurationSecs,
		DurationTaskCount: row.DurationTaskCount,
		InputTokens:       row.InputTokens,
		OutputTokens:      row.OutputTokens,
		CacheReadTokens:   row.CacheReadTokens,
		CacheWriteTokens:  row.CacheWriteTokens,
		RerunCount:        row.RerunCount,
		FollowUpCount:     row.FollowUpCount,
		MaxAttempt:        row.MaxAttempt,
	}
}

func knowledgeEffectSummaryToResponse(row db.GetKnowledgeEffectSummaryRow) KnowledgeEffectSummaryResponse {
	return KnowledgeEffectSummaryResponse{
		TotalTasks:            row.TotalTasks,
		TotalSuccessful:       row.TotalSuccessful,
		TotalFailed:           row.TotalFailed,
		TotalDurationSecs:     row.TotalDurationSecs,
		TotalDurationTasks:    row.TotalDurationTasks,
		TotalInputTokens:      row.TotalInputTokens,
		TotalOutputTokens:     row.TotalOutputTokens,
		TotalCacheReadTokens:  row.TotalCacheReadTokens,
		TotalCacheWriteTokens: row.TotalCacheWriteTokens,
		TotalReruns:           row.TotalReruns,
		TotalFollowUps:        row.TotalFollowUps,
	}
}

func jsonObjectOrEmpty(raw []byte) any {
	var out any = map[string]any{}
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func parseKnowledgeSources(w http.ResponseWriter, inputs []knowledgeSourceInput) ([]service.KnowledgeSourceInput, bool) {
	out := make([]service.KnowledgeSourceInput, 0, len(inputs))
	for _, input := range inputs {
		sourceID, ok := parseOptionalUUIDPtr(w, input.SourceID, "source_id")
		if !ok {
			return nil, false
		}
		out = append(out, service.KnowledgeSourceInput{
			SourceType:    input.SourceType,
			SourceID:      sourceID,
			SourceURL:     textFromPtr(input.SourceURL),
			SourceTitle:   textFromPtr(input.SourceTitle),
			SourceExcerpt: textFromPtr(input.SourceExcerpt),
		})
	}
	return out, true
}

func parseOptionalUUIDQuery(w http.ResponseWriter, value, fieldName string) (pgtype.UUID, bool) {
	if strings.TrimSpace(value) == "" {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, value, fieldName)
}

func parseOptionalUUIDPtr(w http.ResponseWriter, value *string, fieldName string) (pgtype.UUID, bool) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, *value, fieldName)
}

func parseLimitQuery(w http.ResponseWriter, raw string, fallback, max int32) (int32, bool) {
	if strings.TrimSpace(raw) == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return 0, false
	}
	if n == 0 {
		return fallback, true
	}
	if int32(n) > max {
		return max, true
	}
	return int32(n), true
}

func parseOffsetQuery(w http.ResponseWriter, raw string) (int32, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid offset")
		return 0, false
	}
	return int32(n), true
}

func parseKnowledgeTimeQuery(w http.ResponseWriter, raw string, fallback time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback.UTC(), true
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), true
	}
	if date, err := time.Parse("2006-01-02", raw); err == nil {
		return date.UTC(), true
	}
	writeError(w, http.StatusBadRequest, "invalid time")
	return time.Time{}, false
}

func textFromString(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func textFromPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: strings.TrimSpace(*value), Valid: true}
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timestampPtr(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	value := timestampToString(ts)
	return &value
}

func numericToFloat(n pgtype.Numeric, fallback float64) float64 {
	if !n.Valid {
		return fallback
	}
	value, err := n.Float64Value()
	if err != nil || !value.Valid {
		return fallback
	}
	return value.Float64
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
