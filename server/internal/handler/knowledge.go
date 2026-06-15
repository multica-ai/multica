package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
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

type KnowledgeDetailResponse struct {
	Item            KnowledgeItemResponse                `json:"item"`
	Sources         []KnowledgeSourceResponse            `json:"sources"`
	Embeddings      []KnowledgeEmbeddingMetadataResponse `json:"embeddings"`
	FeedbackSummary []KnowledgeFeedbackSummaryResponse   `json:"feedback_summary"`
}

type KnowledgeSearchResultResponse struct {
	Item        KnowledgeItemResponse `json:"item"`
	TextScore   float64               `json:"text_score"`
	VectorScore float64               `json:"vector_score"`
	FinalScore  float64               `json:"final_score"`
	MatchReason string                `json:"match_reason"`
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

type searchKnowledgeRequest struct {
	Query     string                 `json:"query"`
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
	Value string  `json:"value"`
	Note  *string `json:"note"`
}

type evaluateKnowledgeCandidateRequest struct {
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id"`
	TriggerReason string `json:"trigger_reason"`
	Manual        *bool  `json:"manual"`
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
	if _, err := h.KnowledgeService.Archive(r.Context(), wsUUID, itemID); err != nil {
		h.writeKnowledgeError(w, err, "failed to archive knowledge")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	results, err := h.KnowledgeService.Search(r.Context(), service.KnowledgeSearchParams{
		WorkspaceID: wsUUID,
		MemberID:    member.ID,
		Query:       req.Query,
		Embedding:   req.Embedding,
		Limit:       req.Limit,
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
			Item:        knowledgeItemToResponse(result.Item),
			TextScore:   result.TextScore,
			VectorScore: result.VectorScore,
			FinalScore:  result.FinalScore,
			MatchReason: result.MatchReason,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": resp, "total": len(resp)})
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
	feedback, err := h.KnowledgeService.AddFeedback(r.Context(), service.KnowledgeFeedbackParams{
		KnowledgeItemID: itemID,
		WorkspaceID:     wsUUID,
		MemberID:        member.ID,
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
		"value":             feedback.Value,
		"note":              textToPtr(feedback.Note),
		"created_at":        timestampToString(feedback.CreatedAt),
	})
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

func (h *Handler) writeKnowledgeError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, service.ErrKnowledgeValidation) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, service.ErrKnowledgeNotFound) {
		writeError(w, http.StatusNotFound, "knowledge not found")
		return
	}
	writeError(w, http.StatusInternalServerError, fallback)
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
	return KnowledgeDetailResponse{
		Item:            knowledgeItemToResponse(detail.Item),
		Sources:         sources,
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

func timestampPtr(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	value := timestampToString(ts)
	return &value
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
