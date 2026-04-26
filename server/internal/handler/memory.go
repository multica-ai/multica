package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/memoryguard"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	memoryMaxTitleRunes   = 160
	memoryMaxContentRunes = 4000
	memoryDefaultLimit    = 50
	memoryMaxLimit        = 200
)

const memoryColumns = `
	id, workspace_id, scope_type, scope_id, title, content, status,
	source_issue_id, source_comment_id,
	proposed_by_type, proposed_by_id,
	reviewed_by_type, reviewed_by_id, review_note, guardrail,
	approved_at, rejected_at, created_at, updated_at
`

type memoryEntry struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	ScopeType       string
	ScopeID         pgtype.UUID
	Title           string
	Content         string
	Status          string
	SourceIssueID   pgtype.UUID
	SourceCommentID pgtype.UUID
	ProposedByType  string
	ProposedByID    pgtype.UUID
	ReviewedByType  pgtype.Text
	ReviewedByID    pgtype.UUID
	ReviewNote      pgtype.Text
	Guardrail       []byte
	ApprovedAt      pgtype.Timestamptz
	RejectedAt      pgtype.Timestamptz
	CreatedAt       pgtype.Timestamptz
	UpdatedAt       pgtype.Timestamptz
}

type MemoryResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	ScopeType       string  `json:"scope_type"`
	ScopeID         *string `json:"scope_id"`
	Title           string  `json:"title"`
	Content         string  `json:"content"`
	Status          string  `json:"status"`
	SourceIssueID   *string `json:"source_issue_id"`
	SourceCommentID *string `json:"source_comment_id"`
	ProposedByType  string  `json:"proposed_by_type"`
	ProposedByID    string  `json:"proposed_by_id"`
	ReviewedByType  *string `json:"reviewed_by_type"`
	ReviewedByID    *string `json:"reviewed_by_id"`
	ReviewNote      *string `json:"review_note"`
	Guardrail       any     `json:"guardrail"`
	ApprovedAt      *string `json:"approved_at"`
	RejectedAt      *string `json:"rejected_at"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// MemoryContextData is the approved memory subset sent to daemon runtimes.
type MemoryContextData struct {
	ID              string  `json:"id"`
	ScopeType       string  `json:"scope_type"`
	ScopeID         *string `json:"scope_id,omitempty"`
	Title           string  `json:"title"`
	Content         string  `json:"content"`
	SourceIssueID   *string `json:"source_issue_id,omitempty"`
	SourceCommentID *string `json:"source_comment_id,omitempty"`
}

type ProposeMemoryRequest struct {
	ScopeType       string  `json:"scope_type"`
	ScopeID         *string `json:"scope_id"`
	Title           string  `json:"title"`
	Content         string  `json:"content"`
	SourceIssueID   *string `json:"source_issue_id"`
	SourceCommentID *string `json:"source_comment_id"`
}

type reviewMemoryRequest struct {
	Note string `json:"note"`
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemoryEntry(s memoryScanner) (memoryEntry, error) {
	var entry memoryEntry
	err := s.Scan(
		&entry.ID,
		&entry.WorkspaceID,
		&entry.ScopeType,
		&entry.ScopeID,
		&entry.Title,
		&entry.Content,
		&entry.Status,
		&entry.SourceIssueID,
		&entry.SourceCommentID,
		&entry.ProposedByType,
		&entry.ProposedByID,
		&entry.ReviewedByType,
		&entry.ReviewedByID,
		&entry.ReviewNote,
		&entry.Guardrail,
		&entry.ApprovedAt,
		&entry.RejectedAt,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	return entry, err
}

func memoryToResponse(entry memoryEntry) MemoryResponse {
	guardrail := any(map[string]any{})
	if len(entry.Guardrail) > 0 {
		_ = json.Unmarshal(entry.Guardrail, &guardrail)
	}
	return MemoryResponse{
		ID:              uuidToString(entry.ID),
		WorkspaceID:     uuidToString(entry.WorkspaceID),
		ScopeType:       entry.ScopeType,
		ScopeID:         uuidToPtr(entry.ScopeID),
		Title:           entry.Title,
		Content:         entry.Content,
		Status:          entry.Status,
		SourceIssueID:   uuidToPtr(entry.SourceIssueID),
		SourceCommentID: uuidToPtr(entry.SourceCommentID),
		ProposedByType:  entry.ProposedByType,
		ProposedByID:    uuidToString(entry.ProposedByID),
		ReviewedByType:  textToPtr(entry.ReviewedByType),
		ReviewedByID:    uuidToPtr(entry.ReviewedByID),
		ReviewNote:      textToPtr(entry.ReviewNote),
		Guardrail:       guardrail,
		ApprovedAt:      timestampToPtr(entry.ApprovedAt),
		RejectedAt:      timestampToPtr(entry.RejectedAt),
		CreatedAt:       timestampToString(entry.CreatedAt),
		UpdatedAt:       timestampToString(entry.UpdatedAt),
	}
}

func memoryToContext(entry memoryEntry) MemoryContextData {
	return MemoryContextData{
		ID:              uuidToString(entry.ID),
		ScopeType:       entry.ScopeType,
		ScopeID:         uuidToPtr(entry.ScopeID),
		Title:           entry.Title,
		Content:         entry.Content,
		SourceIssueID:   uuidToPtr(entry.SourceIssueID),
		SourceCommentID: uuidToPtr(entry.SourceCommentID),
	}
}

func (h *Handler) ListMemories(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	query, args, err := buildMemoryListQuery(workspaceID, r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := h.queryMemories(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list memories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memories": memoryEntriesToResponses(entries),
		"total":    len(entries),
	})
}

func (h *Handler) SearchMemories(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	workspaceID := resolveWorkspaceID(r)
	query, args, err := buildMemoryListQuery(workspaceID, r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := h.queryMemories(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search memories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memories": memoryEntriesToResponses(entries),
		"total":    len(entries),
	})
}

func (h *Handler) GetMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	entry, err := h.getMemoryInWorkspace(r.Context(), chi.URLParam(r, "id"), workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}
	writeJSON(w, http.StatusOK, memoryToResponse(entry))
}

func (h *Handler) ProposeMemory(w http.ResponseWriter, r *http.Request) {
	var req ProposeMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ScopeType = strings.TrimSpace(req.ScopeType)
	if req.ScopeType == "" {
		req.ScopeType = "workspace"
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	if err := validateMemoryText(req.Title, req.Content); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	report := memoryguard.Inspect(req.Title, req.Content)
	if !report.Allowed {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":    "memory content failed guardrails",
			"findings": memoryguard.FindingTypes(report.Findings),
		})
		return
	}

	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	scopeID, ok := h.validateMemoryScope(w, r, workspaceID, req.ScopeType, req.ScopeID)
	if !ok {
		return
	}
	sourceIssueID, sourceCommentID, ok := h.validateMemorySourceRefs(w, r, workspaceID, req.SourceIssueID, req.SourceCommentID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	guardrail, _ := json.Marshal(report)

	entry, err := scanMemoryEntry(h.DB.QueryRow(r.Context(), `
		INSERT INTO memory_entry (
			workspace_id, scope_type, scope_id, title, content,
			source_issue_id, source_comment_id,
			proposed_by_type, proposed_by_id, guardrail
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+memoryColumns,
		parseUUID(workspaceID),
		req.ScopeType,
		scopeID,
		req.Title,
		req.Content,
		sourceIssueID,
		sourceCommentID,
		actorType,
		parseUUID(actorID),
		guardrail,
	))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to propose memory")
		return
	}
	writeJSON(w, http.StatusCreated, memoryToResponse(entry))
}

func (h *Handler) ApproveMemory(w http.ResponseWriter, r *http.Request) {
	h.reviewMemory(w, r, "approved")
}

func (h *Handler) RejectMemory(w http.ResponseWriter, r *http.Request) {
	h.reviewMemory(w, r, "rejected")
}

func (h *Handler) reviewMemory(w http.ResponseWriter, r *http.Request, status string) {
	if r.Header.Get("X-Agent-ID") != "" {
		writeError(w, http.StatusForbidden, "memory review requires a human owner or admin")
		return
	}
	workspaceID := resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	req, ok := decodeReviewMemoryRequest(w, r)
	if !ok {
		return
	}
	current, err := h.getMemoryInWorkspace(r.Context(), chi.URLParam(r, "id"), workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}
	report := memoryguard.Inspect(current.Title, current.Content)
	if status == "approved" && !report.Allowed {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":    "memory content failed guardrails",
			"findings": memoryguard.FindingTypes(report.Findings),
		})
		return
	}
	guardrail, _ := json.Marshal(report)

	var query string
	if status == "approved" {
		query = `
			UPDATE memory_entry
			SET status = 'approved',
				reviewed_by_type = 'member',
				reviewed_by_id = $3,
				review_note = $4,
				guardrail = $5,
				approved_at = now(),
				rejected_at = NULL,
				updated_at = now()
			WHERE id = $1 AND workspace_id = $2
			RETURNING ` + memoryColumns
	} else {
		query = `
			UPDATE memory_entry
			SET status = 'rejected',
				reviewed_by_type = 'member',
				reviewed_by_id = $3,
				review_note = $4,
				guardrail = $5,
				approved_at = NULL,
				rejected_at = now(),
				updated_at = now()
			WHERE id = $1 AND workspace_id = $2
			RETURNING ` + memoryColumns
	}

	entry, err := scanMemoryEntry(h.DB.QueryRow(
		r.Context(),
		query,
		parseUUID(chi.URLParam(r, "id")),
		parseUUID(workspaceID),
		member.UserID,
		strToText(strings.TrimSpace(req.Note)),
		guardrail,
	))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review memory")
		return
	}
	writeJSON(w, http.StatusOK, memoryToResponse(entry))
}

func (h *Handler) loadApprovedMemoryContext(ctx context.Context, workspaceID string, agentID pgtype.UUID, issue *db.Issue) []MemoryContextData {
	if workspaceID == "" || h.DB == nil {
		return nil
	}

	args := []any{parseUUID(workspaceID), agentID}
	conditions := []string{
		"(scope_type = 'workspace' AND scope_id IS NULL)",
		"(scope_type = 'agent' AND scope_id = $2)",
	}
	if issue != nil {
		args = append(args, issue.ID)
		conditions = append(conditions, fmt.Sprintf("(scope_type = 'issue' AND scope_id = $%d)", len(args)))
		if issue.ProjectID.Valid {
			args = append(args, issue.ProjectID)
			conditions = append(conditions, fmt.Sprintf("(scope_type = 'project' AND scope_id = $%d)", len(args)))
		}
	}

	query := `
		SELECT ` + memoryColumns + `
		FROM memory_entry
		WHERE workspace_id = $1
		  AND status = 'approved'
		  AND (` + strings.Join(conditions, " OR ") + `)
		ORDER BY
			CASE scope_type
				WHEN 'issue' THEN 1
				WHEN 'project' THEN 2
				WHEN 'agent' THEN 3
				ELSE 4
			END,
			updated_at DESC
		LIMIT 20`

	entries, err := h.queryMemories(ctx, query, args...)
	if err != nil {
		return nil
	}
	memories := make([]MemoryContextData, 0, len(entries))
	for _, entry := range entries {
		if !memoryguard.Inspect(entry.Title, entry.Content).Allowed {
			continue
		}
		memories = append(memories, memoryToContext(entry))
	}
	return memories
}

func (h *Handler) queryMemories(ctx context.Context, query string, args ...any) ([]memoryEntry, error) {
	rows, err := h.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []memoryEntry
	for rows.Next() {
		entry, err := scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (h *Handler) getMemoryInWorkspace(ctx context.Context, memoryID, workspaceID string) (memoryEntry, error) {
	entry, err := scanMemoryEntry(h.DB.QueryRow(ctx, `
		SELECT `+memoryColumns+`
		FROM memory_entry
		WHERE id = $1 AND workspace_id = $2`,
		parseUUID(memoryID),
		parseUUID(workspaceID),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return memoryEntry{}, err
	}
	return entry, err
}

func memoryEntriesToResponses(entries []memoryEntry) []MemoryResponse {
	resp := make([]MemoryResponse, len(entries))
	for i, entry := range entries {
		resp[i] = memoryToResponse(entry)
	}
	return resp
}

func buildMemoryListQuery(workspaceID string, r *http.Request, includeSearch bool) (string, []any, error) {
	args := []any{parseUUID(workspaceID)}
	clauses := []string{"workspace_id = $1"}

	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !validMemoryStatus(status) {
			return "", nil, fmt.Errorf("invalid status %q", status)
		}
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if scopeType := strings.TrimSpace(r.URL.Query().Get("scope_type")); scopeType != "" {
		if !validMemoryScopeType(scopeType) {
			return "", nil, fmt.Errorf("invalid scope_type %q", scopeType)
		}
		args = append(args, scopeType)
		clauses = append(clauses, fmt.Sprintf("scope_type = $%d", len(args)))
	}
	if scopeID := strings.TrimSpace(r.URL.Query().Get("scope_id")); scopeID != "" {
		args = append(args, parseUUID(scopeID))
		clauses = append(clauses, fmt.Sprintf("scope_id = $%d", len(args)))
	}
	if includeSearch {
		pattern := "%" + escapeLike(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))) + "%"
		args = append(args, pattern)
		clauses = append(clauses, fmt.Sprintf("(LOWER(title) LIKE $%d ESCAPE '\\' OR LOWER(content) LIKE $%d ESCAPE '\\')", len(args), len(args)))
	}

	limit, err := intQueryParam(r, "limit", memoryDefaultLimit, memoryMaxLimit)
	if err != nil {
		return "", nil, err
	}
	offset, err := intQueryParam(r, "offset", 0, 100000)
	if err != nil {
		return "", nil, err
	}
	args = append(args, limit)
	limitRef := fmt.Sprintf("$%d", len(args))
	args = append(args, offset)
	offsetRef := fmt.Sprintf("$%d", len(args))

	query := `
		SELECT ` + memoryColumns + `
		FROM memory_entry
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY updated_at DESC
		LIMIT ` + limitRef + ` OFFSET ` + offsetRef
	return query, args, nil
}

func intQueryParam(r *http.Request, name string, defaultValue, maxValue int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	if value > maxValue {
		return maxValue, nil
	}
	return value, nil
}

func validateMemoryText(title, content string) error {
	if title == "" {
		return fmt.Errorf("title is required")
	}
	if content == "" {
		return fmt.Errorf("content is required")
	}
	if utf8.RuneCountInString(title) > memoryMaxTitleRunes {
		return fmt.Errorf("title must be at most %d characters", memoryMaxTitleRunes)
	}
	if utf8.RuneCountInString(content) > memoryMaxContentRunes {
		return fmt.Errorf("content must be at most %d characters", memoryMaxContentRunes)
	}
	return nil
}

func (h *Handler) validateMemoryScope(w http.ResponseWriter, r *http.Request, workspaceID, scopeType string, scopeID *string) (pgtype.UUID, bool) {
	if !validMemoryScopeType(scopeType) {
		writeError(w, http.StatusBadRequest, "invalid scope_type")
		return pgtype.UUID{}, false
	}
	if scopeType == "workspace" {
		return pgtype.UUID{}, true
	}
	if scopeID == nil || strings.TrimSpace(*scopeID) == "" {
		writeError(w, http.StatusBadRequest, "scope_id is required for "+scopeType+" memory")
		return pgtype.UUID{}, false
	}

	id := strings.TrimSpace(*scopeID)
	switch scopeType {
	case "project":
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID)}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return pgtype.UUID{}, false
		}
	case "agent":
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID)}); err != nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return pgtype.UUID{}, false
		}
	case "issue":
		issue, ok := h.findIssueRefInWorkspace(r.Context(), id, workspaceID)
		if !ok {
			writeError(w, http.StatusNotFound, "issue not found")
			return pgtype.UUID{}, false
		}
		return issue.ID, true
	}
	return parseUUID(id), true
}

func (h *Handler) validateMemorySourceRefs(w http.ResponseWriter, r *http.Request, workspaceID string, sourceIssueRef, sourceCommentRef *string) (pgtype.UUID, pgtype.UUID, bool) {
	var sourceIssueID pgtype.UUID
	var sourceCommentID pgtype.UUID

	if sourceIssueRef != nil && strings.TrimSpace(*sourceIssueRef) != "" {
		issue, ok := h.findIssueRefInWorkspace(r.Context(), strings.TrimSpace(*sourceIssueRef), workspaceID)
		if !ok {
			writeError(w, http.StatusNotFound, "source issue not found")
			return pgtype.UUID{}, pgtype.UUID{}, false
		}
		sourceIssueID = issue.ID
	}

	if sourceCommentRef != nil && strings.TrimSpace(*sourceCommentRef) != "" {
		comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          parseUUID(strings.TrimSpace(*sourceCommentRef)),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "source comment not found")
			return pgtype.UUID{}, pgtype.UUID{}, false
		}
		if sourceIssueID.Valid && uuidToString(sourceIssueID) != uuidToString(comment.IssueID) {
			writeError(w, http.StatusBadRequest, "source_comment_id does not belong to source_issue_id")
			return pgtype.UUID{}, pgtype.UUID{}, false
		}
		sourceIssueID = comment.IssueID
		sourceCommentID = comment.ID
	}

	return sourceIssueID, sourceCommentID, true
}

func (h *Handler) findIssueRefInWorkspace(ctx context.Context, ref, workspaceID string) (db.Issue, bool) {
	if issue, ok := h.resolveIssueByIdentifier(ctx, ref, workspaceID); ok {
		return issue, true
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          parseUUID(ref),
		WorkspaceID: parseUUID(workspaceID),
	})
	return issue, err == nil
}

func decodeReviewMemoryRequest(w http.ResponseWriter, r *http.Request) (reviewMemoryRequest, bool) {
	var req reviewMemoryRequest
	if r.Body == nil {
		return req, true
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err == nil || errors.Is(err, io.EOF) {
		return req, true
	}
	writeError(w, http.StatusBadRequest, "invalid request body")
	return reviewMemoryRequest{}, false
}

func validMemoryScopeType(scopeType string) bool {
	switch scopeType {
	case "workspace", "project", "agent", "issue":
		return true
	default:
		return false
	}
}

func validMemoryStatus(status string) bool {
	switch status {
	case "pending", "approved", "rejected":
		return true
	default:
		return false
	}
}
