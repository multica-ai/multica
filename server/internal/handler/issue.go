package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IssueResponse is the JSON response for an issue.
type IssueResponse struct {
	ID            string                         `json:"id"`
	WorkspaceID   string                         `json:"workspace_id"`
	Number        int32                          `json:"number"`
	Identifier    string                         `json:"identifier"`
	Title         string                         `json:"title"`
	Description   *string                        `json:"description"`
	Status        string                         `json:"status"`
	Priority      string                         `json:"priority"`
	AssigneeType  *string                        `json:"assignee_type"`
	AssigneeID    *string                        `json:"assignee_id"`
	CreatorType   string                         `json:"creator_type"`
	CreatorID     string                         `json:"creator_id"`
	ParentIssueID *string                        `json:"parent_issue_id"`
	ProjectID     *string                        `json:"project_id"`
	IssueTypeID   *string                        `json:"issue_type_id"`
	Position      float64                        `json:"position"`
	DueDate       *string                        `json:"due_date"`
	StartDate     *string                        `json:"start_date"`
	EndDate       *string                        `json:"end_date"`
	ArchivedAt    *string                        `json:"archived_at"`
	ArchivedBy    *string                        `json:"archived_by"`
	CreatedAt     string                         `json:"created_at"`
	UpdatedAt     string                         `json:"updated_at"`
	ParentIssue   *IssueReferenceResponse        `json:"parent_issue,omitempty"`
	ChildIssues   []IssueReferenceResponse       `json:"child_issues,omitempty"`
	Labels        []IssueLabelResponse           `json:"labels,omitempty"`
	Dependencies  *IssueDependencyGroupsResponse `json:"dependencies,omitempty"`
	Reactions     []IssueReactionResponse        `json:"reactions,omitempty"`
	Attachments   []AttachmentResponse           `json:"attachments,omitempty"`
}

type IssueReferenceResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Number        int32   `json:"number"`
	Identifier    string  `json:"identifier"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	Priority      string  `json:"priority"`
	ParentIssueID *string `json:"parent_issue_id"`
}

type IssueLabelResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
}

type IssueDependencyEntryResponse struct {
	ID    string                 `json:"id"`
	Type  string                 `json:"type"`
	Issue IssueReferenceResponse `json:"issue"`
}

type IssueDependencyGroupsResponse struct {
	Blocks    []IssueDependencyEntryResponse `json:"blocks,omitempty"`
	BlockedBy []IssueDependencyEntryResponse `json:"blocked_by,omitempty"`
	Related   []IssueDependencyEntryResponse `json:"related,omitempty"`
	Copy      []IssueDependencyEntryResponse `json:"copy,omitempty"`
}

type issueListView string

const (
	issueListViewBacklog  issueListView = "backlog"
	issueListViewToday    issueListView = "today"
	issueListViewUpcoming issueListView = "upcoming"
	labelMatchModeAny     string        = "any"
	labelMatchModeAll     string        = "all"
)

func parseIssueListView(value string) (pgtype.Text, error) {
	if value == "" {
		return pgtype.Text{}, nil
	}

	switch issueListView(value) {
	case issueListViewBacklog, issueListViewToday, issueListViewUpcoming:
		return pgtype.Text{String: value, Valid: true}, nil
	default:
		return pgtype.Text{}, fmt.Errorf("invalid view, expected one of: backlog, today, upcoming")
	}
}

func parseIssueListSearch(value string) (pgtype.Text, pgtype.UUID, pgtype.Int4) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return pgtype.Text{}, pgtype.UUID{}, pgtype.Int4{}
	}

	searchText := pgtype.Text{String: trimmed, Valid: true}
	searchUUID := parseUUID(trimmed)
	searchNumber := pgtype.Int4{}

	if number, err := strconv.Atoi(trimmed); err == nil {
		searchNumber = pgtype.Int4{Int32: int32(number), Valid: true}
	} else if parts := splitIdentifier(trimmed); parts != nil {
		searchNumber = pgtype.Int4{Int32: parts.number, Valid: true}
	}

	return searchText, searchUUID, searchNumber
}

func parseIssueListDate(value string, fieldName string) (pgtype.Date, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return pgtype.Date{}, nil
	}

	parsed, err := time.Parse(time.DateOnly, trimmed)
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("invalid %s format, expected YYYY-MM-DD", fieldName)
	}

	return pgtype.Date{Time: parsed, Valid: true}, nil
}

// parseIssueListLabelMatchMode enforces a shared label match-mode contract across frontend and backend.
func parseIssueListLabelMatchMode(value string) (pgtype.Text, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return pgtype.Text{String: labelMatchModeAny, Valid: true}, nil
	}

	switch normalized {
	case labelMatchModeAny, labelMatchModeAll:
		return pgtype.Text{String: normalized, Valid: true}, nil
	default:
		return pgtype.Text{}, fmt.Errorf("invalid label_match_mode, expected one of: any, all")
	}
}

// parseIssueListLabelIDs validates and deduplicates label IDs to keep all-match semantics stable.
func parseIssueListLabelIDs(values []string) ([]pgtype.UUID, error) {
	parsed := make([]pgtype.UUID, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}

		parsedID := parseUUID(trimmed)
		if !parsedID.Valid {
			return nil, fmt.Errorf("invalid label_ids value: %s", trimmed)
		}

		seen[trimmed] = struct{}{}
		parsed = append(parsed, parsedID)
	}

	return parsed, nil
}

type agentTriggerSnapshot struct {
	Type    string         `json:"type"`
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config"`
}

// defaultAgentTriggers returns the default trigger config for new agents:
// all three triggers explicitly enabled.
func defaultAgentTriggers() []byte {
	b, _ := json.Marshal([]agentTriggerSnapshot{
		{Type: "on_assign", Enabled: true},
		{Type: "on_comment", Enabled: true},
		{Type: "on_mention", Enabled: true},
	})
	return b
}

func parseOptionalRFC3339Timestamp(value *string, fieldName string) (pgtype.Timestamptz, error) {
	if value == nil || *value == "" {
		return pgtype.Timestamptz{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("invalid %s format, expected RFC3339", fieldName)
	}

	return pgtype.Timestamptz{Time: parsed, Valid: true}, nil
}

func validateScheduleWindow(startDate, endDate pgtype.Timestamptz) error {
	if startDate.Valid && endDate.Valid && startDate.Time.After(endDate.Time) {
		return fmt.Errorf("start_date must be on or before end_date")
	}

	return nil
}

func stringPtrChanged(prev, next *string) bool {
	switch {
	case prev == nil && next == nil:
		return false
	case prev == nil || next == nil:
		return true
	default:
		return *prev != *next
	}
}

func hasRawField(rawFields map[string]json.RawMessage, field string) bool {
	_, ok := rawFields[field]
	return ok
}

func buildIssueUpdateEventPayload(prevIssue db.Issue, issue db.Issue, resp IssueResponse, rawFields map[string]json.RawMessage) (map[string]any, bool) {
	prevDescription := textToPtr(prevIssue.Description)
	prevDueDate := timestampToPtr(prevIssue.DueDate)
	prevStartDate := timestampToPtr(prevIssue.StartDate)
	prevEndDate := timestampToPtr(prevIssue.EndDate)

	assigneeChanged := (hasRawField(rawFields, "assignee_type") || hasRawField(rawFields, "assignee_id")) &&
		(prevIssue.AssigneeType.String != issue.AssigneeType.String || uuidToString(prevIssue.AssigneeID) != uuidToString(issue.AssigneeID))

	return map[string]any{
		"issue":               resp,
		"assignee_changed":    assigneeChanged,
		"status_changed":      hasRawField(rawFields, "status") && prevIssue.Status != issue.Status,
		"priority_changed":    hasRawField(rawFields, "priority") && prevIssue.Priority != issue.Priority,
		"due_date_changed":    hasRawField(rawFields, "due_date") && stringPtrChanged(prevDueDate, resp.DueDate),
		"start_date_changed":  hasRawField(rawFields, "start_date") && stringPtrChanged(prevStartDate, resp.StartDate),
		"end_date_changed":    hasRawField(rawFields, "end_date") && stringPtrChanged(prevEndDate, resp.EndDate),
		"description_changed": hasRawField(rawFields, "description") && stringPtrChanged(prevDescription, resp.Description),
		"title_changed":       hasRawField(rawFields, "title") && prevIssue.Title != issue.Title,
		"prev_title":          prevIssue.Title,
		"prev_assignee_type":  textToPtr(prevIssue.AssigneeType),
		"prev_assignee_id":    uuidToPtr(prevIssue.AssigneeID),
		"prev_status":         prevIssue.Status,
		"prev_priority":       prevIssue.Priority,
		"prev_due_date":       prevDueDate,
		"prev_start_date":     prevStartDate,
		"prev_end_date":       prevEndDate,
		"prev_description":    prevDescription,
		"creator_type":        prevIssue.CreatorType,
		"creator_id":          uuidToString(prevIssue.CreatorID),
	}, assigneeChanged
}

func issueToResponse(i db.Issue, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Description:   textToPtr(i.Description),
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		IssueTypeID:   uuidToPtr(i.IssueTypeID),
		Position:      i.Position,
		DueDate:       timestampToPtr(i.DueDate),
		StartDate:     timestampToPtr(i.StartDate),
		EndDate:       timestampToPtr(i.EndDate),
		ArchivedAt:    timestampToPtr(i.ArchivedAt),
		ArchivedBy:    uuidToPtr(i.ArchivedBy),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
	}
}

func (h *Handler) defaultIssueTypeID(ctx context.Context, workspaceID string) (pgtype.UUID, error) {
	workspaceUUID := parseUUID(workspaceID)
	if err := h.Queries.EnsureDefaultIssueTypes(ctx, workspaceUUID); err != nil {
		return pgtype.UUID{}, err
	}
	issueType, err := h.Queries.GetIssueTypeByKey(ctx, db.GetIssueTypeByKeyParams{
		WorkspaceID: workspaceUUID,
		Key:         "task",
	})
	if err != nil {
		return pgtype.UUID{}, err
	}
	return issueType.ID, nil
}

func (h *Handler) validateIssueTypeID(ctx context.Context, workspaceID string, raw *string) (pgtype.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return h.defaultIssueTypeID(ctx, workspaceID)
	}
	issueTypeID := parseUUID(*raw)
	if !issueTypeID.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid issue_type_id")
	}
	if _, err := h.Queries.GetIssueType(ctx, db.GetIssueTypeParams{
		ID:          issueTypeID,
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("issue_type_id not found")
	}
	return issueTypeID, nil
}

func issueToReferenceResponse(i db.Issue, issuePrefix string) IssueReferenceResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueReferenceResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Status:        i.Status,
		Priority:      i.Priority,
		ParentIssueID: uuidToPtr(i.ParentIssueID),
	}
}

func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workspaceID := resolveWorkspaceID(r)

	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v >= 0 && v <= math.MaxInt32 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 && v <= math.MaxInt32 {
			offset = v
		}
	}

	// Parse optional filter params
	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	var priorityFilter pgtype.Text
	if p := r.URL.Query().Get("priority"); p != "" {
		priorityFilter = pgtype.Text{String: p, Valid: true}
	}
	var issueTypeFilter pgtype.UUID
	if issueTypeID := r.URL.Query().Get("issue_type_id"); issueTypeID != "" {
		issueTypeFilter = parseUUID(issueTypeID)
	}
	var assigneeFilter pgtype.UUID
	if a := r.URL.Query().Get("assignee_id"); a != "" {
		assigneeFilter = parseUUID(a)
	}
	var assigneeTypeFilter pgtype.Text
	if at := r.URL.Query().Get("assignee_type"); at != "" {
		assigneeTypeFilter = pgtype.Text{String: at, Valid: true}
	}
	var creatorFilter pgtype.UUID
	if creatorID := r.URL.Query().Get("creator_id"); creatorID != "" {
		creatorFilter = parseUUID(creatorID)
	}
	var projectFilter pgtype.UUID
	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		projectFilter = parseUUID(projectID)
	}
	var creatorTypeFilter pgtype.Text
	if creatorType := r.URL.Query().Get("creator_type"); creatorType != "" {
		creatorTypeFilter = pgtype.Text{String: creatorType, Valid: true}
	}
	searchText, searchUUID, searchNumber := parseIssueListSearch(r.URL.Query().Get("search"))
	dueFrom, err := parseIssueListDate(r.URL.Query().Get("due_from"), "due_from")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dueTo, err := parseIssueListDate(r.URL.Query().Get("due_to"), "due_to")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	startFrom, err := parseIssueListDate(r.URL.Query().Get("start_from"), "start_from")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	startTo, err := parseIssueListDate(r.URL.Query().Get("start_to"), "start_to")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	endFrom, err := parseIssueListDate(r.URL.Query().Get("end_from"), "end_from")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	endTo, err := parseIssueListDate(r.URL.Query().Get("end_to"), "end_to")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	labelIDs, err := parseIssueListLabelIDs(r.URL.Query()["label_ids"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	labelMatchMode := pgtype.Text{}
	if len(labelIDs) > 0 {
		labelMatchMode, err = parseIssueListLabelMatchMode(r.URL.Query().Get("label_match_mode"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	viewFilter, err := parseIssueListView(r.URL.Query().Get("view"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	includeArchived := pgtype.Bool{Bool: strings.EqualFold(r.URL.Query().Get("include_archived"), "true"), Valid: true}
	archivedOnly := pgtype.Bool{Bool: strings.EqualFold(r.URL.Query().Get("archived"), "true") || strings.EqualFold(r.URL.Query().Get("archived_only"), "true"), Valid: true}

	issues, err := h.Queries.ListIssues(ctx, db.ListIssuesParams{
		WorkspaceID:     parseUUID(workspaceID),
		Limit:           int32(limit),
		Offset:          int32(offset),
		IncludeArchived: includeArchived,
		ArchivedOnly:    archivedOnly,
		Status:          statusFilter,
		Priority:        priorityFilter,
		IssueTypeID:     issueTypeFilter,
		AssigneeID:      assigneeFilter,
		AssigneeType:    assigneeTypeFilter,
		CreatorID:       creatorFilter,
		ProjectID:       projectFilter,
		CreatorType:     creatorTypeFilter,
		SearchText:      searchText,
		SearchUuid:      searchUUID,
		SearchNumber:    searchNumber,
		DueFrom:         dueFrom,
		DueTo:           dueTo,
		StartFrom:       startFrom,
		StartTo:         startTo,
		EndFrom:         endFrom,
		EndTo:           endTo,
		LabelIds:        labelIDs,
		LabelMatchMode:  labelMatchMode,
		View:            viewFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}

	total, err := h.Queries.CountListedIssues(ctx, db.CountListedIssuesParams{
		WorkspaceID:     parseUUID(workspaceID),
		IncludeArchived: includeArchived,
		ArchivedOnly:    archivedOnly,
		Status:          statusFilter,
		Priority:        priorityFilter,
		IssueTypeID:     issueTypeFilter,
		AssigneeID:      assigneeFilter,
		AssigneeType:    assigneeTypeFilter,
		CreatorID:       creatorFilter,
		ProjectID:       projectFilter,
		CreatorType:     creatorTypeFilter,
		SearchText:      searchText,
		SearchUuid:      searchUUID,
		SearchNumber:    searchNumber,
		DueFrom:         dueFrom,
		DueTo:           dueTo,
		StartFrom:       startFrom,
		StartTo:         startTo,
		EndFrom:         endFrom,
		EndTo:           endTo,
		LabelIds:        labelIDs,
		LabelMatchMode:  labelMatchMode,
		View:            viewFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count issues")
		return
	}

	prefix := h.getIssuePrefix(ctx, parseUUID(workspaceID))
	resp := make([]IssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = issueToResponse(issue, prefix)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
		"total":  total,
	})
}

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	resp, err := h.buildIssueDetailResponse(r.Context(), issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateIssueRequest struct {
	Title         string  `json:"title"`
	Description   *string `json:"description"`
	Status        string  `json:"status"`
	Priority      string  `json:"priority"`
	AssigneeType  *string `json:"assignee_type"`
	AssigneeID    *string `json:"assignee_id"`
	ParentIssueID *string `json:"parent_issue_id"`
	ProjectID     *string `json:"project_id"`
	IssueTypeID   *string `json:"issue_type_id"`
	DueDate       *string `json:"due_date"`
	StartDate     *string `json:"start_date"`
	EndDate       *string `json:"end_date"`
}

func (h *Handler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	var req CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	workspaceID := resolveWorkspaceID(r)

	// Get creator from context (set by auth middleware)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	status := req.Status
	if status == "" {
		status = "backlog"
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}

	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if req.AssigneeType != nil {
		assigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
	}
	if req.AssigneeID != nil {
		assigneeID = parseUUID(*req.AssigneeID)
	}

	// Enforce agent visibility: private agents can only be assigned by owner/admin.
	if req.AssigneeType != nil && *req.AssigneeType == "agent" && req.AssigneeID != nil {
		if ok, msg := h.canAssignAgent(r.Context(), r, *req.AssigneeID, workspaceID); !ok {
			writeError(w, http.StatusForbidden, msg)
			return
		}
	}

	var parentIssueID pgtype.UUID
	var err error
	if req.ParentIssueID != nil {
		parentIssueID, err = h.validateParentIssue(r.Context(), workspaceID, nil, req.ParentIssueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	projectID, err := h.validateIssueProject(r.Context(), workspaceID, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	issueTypeID, err := h.validateIssueTypeID(r.Context(), workspaceID, req.IssueTypeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dueDate, err := parseOptionalRFC3339Timestamp(req.DueDate, "due_date")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	startDate, err := parseOptionalRFC3339Timestamp(req.StartDate, "start_date")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	endDate, err := parseOptionalRFC3339Timestamp(req.EndDate, "end_date")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateScheduleWindow(startDate, endDate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Use a transaction to atomically increment the workspace issue counter
	// and create the issue with the assigned number.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("increment issue counter failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Determine creator identity: agent (via X-Agent-ID header) or member.
	creatorType, actualCreatorID := h.resolveActor(r, creatorID, workspaceID)

	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID:   parseUUID(workspaceID),
		Title:         req.Title,
		Description:   ptrToText(req.Description),
		Status:        status,
		Priority:      priority,
		AssigneeType:  assigneeType,
		AssigneeID:    assigneeID,
		CreatorType:   creatorType,
		CreatorID:     parseUUID(actualCreatorID),
		ParentIssueID: parentIssueID,
		ProjectID:     projectID,
		IssueTypeID:   issueTypeID,
		Position:      0,
		DueDate:       dueDate,
		StartDate:     startDate,
		EndDate:       endDate,
		Number:        issueNumber,
	})
	if err != nil {
		slog.Warn("create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue: "+err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	resp, err := h.buildIssueDetailResponse(r.Context(), issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load created issue")
		return
	}
	slog.Info("issue created", append(logger.RequestAttrs(r), "issue_id", uuidToString(issue.ID), "title", issue.Title, "status", issue.Status, "workspace_id", workspaceID)...)
	h.publish(protocol.EventIssueCreated, workspaceID, creatorType, actualCreatorID, map[string]any{"issue": resp})

	// Only ready issues in todo are enqueued for agents.
	if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

type UpdateIssueRequest struct {
	Title         *string  `json:"title"`
	Description   *string  `json:"description"`
	Status        *string  `json:"status"`
	Priority      *string  `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	ParentIssueID *string  `json:"parent_issue_id"`
	Position      *float64 `json:"position"`
	ProjectID     *string  `json:"project_id"`
	IssueTypeID   *string  `json:"issue_type_id"`
	DueDate       *string  `json:"due_date"`
	StartDate     *string  `json:"start_date"`
	EndDate       *string  `json:"end_date"`
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prevIssue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID := requestUserID(r)
	workspaceID := uuidToString(prevIssue.WorkspaceID)

	// Read body as raw bytes so we can detect which fields were explicitly sent.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req UpdateIssueRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Track which fields were explicitly present in JSON (even if null)
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	// Pre-fill nullable fields (bare sqlc.narg) with current values
	params := db.UpdateIssueParams{
		ID:            prevIssue.ID,
		AssigneeType:  prevIssue.AssigneeType,
		AssigneeID:    prevIssue.AssigneeID,
		ParentIssueID: prevIssue.ParentIssueID,
		ProjectID:     prevIssue.ProjectID,
		IssueTypeID:   prevIssue.IssueTypeID,
		DueDate:       prevIssue.DueDate,
		StartDate:     prevIssue.StartDate,
		EndDate:       prevIssue.EndDate,
	}

	// COALESCE fields — only set when explicitly provided
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}
	// Nullable fields — only override when explicitly present in JSON
	if _, ok := rawFields["assignee_type"]; ok {
		if req.AssigneeType != nil {
			params.AssigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
		} else {
			params.AssigneeType = pgtype.Text{Valid: false} // explicit null = unassign
		}
	}
	if _, ok := rawFields["assignee_id"]; ok {
		if req.AssigneeID != nil {
			params.AssigneeID = parseUUID(*req.AssigneeID)
		} else {
			params.AssigneeID = pgtype.UUID{Valid: false} // explicit null = unassign
		}
	}
	if _, ok := rawFields["project_id"]; ok {
		if req.ProjectID != nil {
			validatedProjectID, err := h.validateIssueProject(r.Context(), workspaceID, req.ProjectID)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			params.ProjectID = validatedProjectID
		} else {
			params.ProjectID = pgtype.UUID{Valid: false}
		}
	}
	if _, ok := rawFields["issue_type_id"]; ok {
		issueTypeID, err := h.validateIssueTypeID(r.Context(), workspaceID, req.IssueTypeID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.IssueTypeID = issueTypeID
	}
	if _, ok := rawFields["parent_issue_id"]; ok {
		validatedParentIssueID, err := h.validateParentIssue(r.Context(), workspaceID, &id, req.ParentIssueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.ParentIssueID = validatedParentIssueID
	}
	if _, ok := rawFields["due_date"]; ok {
		parsedDueDate, err := parseOptionalRFC3339Timestamp(req.DueDate, "due_date")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.DueDate = parsedDueDate
	}
	if _, ok := rawFields["start_date"]; ok {
		parsedStartDate, err := parseOptionalRFC3339Timestamp(req.StartDate, "start_date")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.StartDate = parsedStartDate
	}
	if _, ok := rawFields["end_date"]; ok {
		parsedEndDate, err := parseOptionalRFC3339Timestamp(req.EndDate, "end_date")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.EndDate = parsedEndDate
	}

	if err := validateScheduleWindow(params.StartDate, params.EndDate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Enforce agent visibility: private agents can only be assigned by owner/admin.
	if req.AssigneeType != nil && *req.AssigneeType == "agent" && req.AssigneeID != nil {
		if ok, msg := h.canAssignAgent(r.Context(), r, *req.AssigneeID, workspaceID); !ok {
			writeError(w, http.StatusForbidden, msg)
			return
		}
	}

	issue, err := h.Queries.UpdateIssue(r.Context(), params)
	if err != nil {
		slog.Warn("update issue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", id, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue: "+err.Error())
		return
	}

	resp, err := h.buildIssueDetailResponse(r.Context(), issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}
	slog.Info("issue updated", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)
	payload, assigneeChanged := buildIssueUpdateEventPayload(prevIssue, issue, resp, rawFields)

	// Determine actor identity: agent (via X-Agent-ID header) or member.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, payload)
	h.publishHierarchyParentSnapshots(r.Context(), workspaceID, actorType, actorID, prevIssue.ParentIssueID, issue.ParentIssueID)

	// Reconcile task queue when assignee changes (not on status changes —
	// agents manage issue status themselves via the CLI).
	if assigneeChanged {
		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)

		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// canAssignAgent checks whether the requesting user is allowed to assign issues
// to the given agent. Private agents can only be assigned by their owner or
// workspace admins/owners.
func (h *Handler) canAssignAgent(ctx context.Context, r *http.Request, agentID, workspaceID string) (bool, string) {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parseUUID(agentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return false, "agent not found"
	}
	if agent.ArchivedAt.Valid {
		return false, "cannot assign to archived agent"
	}
	if agent.Visibility != "private" {
		return true, ""
	}
	userID := requestUserID(r)
	if uuidToString(agent.OwnerID) == userID {
		return true, ""
	}
	member, err := h.getWorkspaceMember(ctx, userID, workspaceID)
	if err != nil {
		return false, "cannot assign to private agent"
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true, ""
	}
	return false, "cannot assign to private agent"
}

// shouldEnqueueAgentTask returns true when an issue assignment should trigger
// the assigned agent. No status gate — assignment is an explicit human action,
// so it should trigger regardless of issue status (e.g. assigning an agent to
// a done issue to fix a discovered problem).
func (h *Handler) shouldEnqueueAgentTask(ctx context.Context, issue db.Issue) bool {
	return h.isAgentTriggerEnabled(ctx, issue, "on_assign")
}

// shouldEnqueueOnComment returns true if a member comment on this issue should
// trigger the assigned agent. Fires for any non-terminal status — comments are
// conversational and can happen at any stage of active work.
func (h *Handler) shouldEnqueueOnComment(ctx context.Context, issue db.Issue) bool {
	// Don't trigger on terminal statuses (done, cancelled).
	if issue.Status == "done" || issue.Status == "cancelled" {
		return false
	}
	if !h.isAgentTriggerEnabled(ctx, issue, "on_comment") {
		return false
	}
	// Coalescing queue: allow enqueue when a task is running (so the agent
	// picks up new comments on the next cycle) but skip if a pending task
	// already exists (natural dedup for rapid-fire comments).
	hasPending, err := h.Queries.HasPendingTaskForIssue(ctx, issue.ID)
	if err != nil || hasPending {
		return false
	}
	return true
}

// isAgentTriggerEnabled checks if an issue is assigned to an agent with a
// specific trigger type enabled. Returns true if the agent has no triggers
// configured (default-enabled behavior for backwards compatibility).
func (h *Handler) isAgentTriggerEnabled(ctx context.Context, issue db.Issue, triggerType string) bool {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return false
	}

	agent, err := h.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return false
	}

	return agentHasTriggerEnabled(agent.Triggers, triggerType)
}

// isAgentMentionTriggerEnabled checks if a specific agent has the on_mention
// trigger enabled. Unlike isAgentTriggerEnabled, this takes an explicit agent
// ID rather than deriving it from the issue assignee.
func (h *Handler) isAgentMentionTriggerEnabled(ctx context.Context, agentID pgtype.UUID) bool {
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.RuntimeID.Valid {
		return false
	}

	return agentHasTriggerEnabled(agent.Triggers, "on_mention")
}

// agentHasTriggerEnabled checks if a trigger type is enabled in the agent's
// trigger config. Returns true (default-enabled) when the triggers list is
// empty or does not contain the requested type — for backwards compatibility
// with agents created before explicit trigger config was introduced.
func agentHasTriggerEnabled(raw []byte, triggerType string) bool {
	if raw == nil || len(raw) == 0 {
		return true
	}

	var triggers []agentTriggerSnapshot
	if err := json.Unmarshal(raw, &triggers); err != nil {
		return false
	}
	if len(triggers) == 0 {
		return true // Empty array = default-enabled (backwards compat)
	}
	for _, trigger := range triggers {
		if trigger.Type == triggerType {
			return trigger.Enabled
		}
	}
	return true // Trigger type not configured = enabled by default
}

func (h *Handler) DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)

	// Collect all attachment URLs (issue-level + comment-level) before CASCADE delete.
	attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(r.Context(), issue.ID)

	err := h.Queries.DeleteIssue(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue")
		return
	}

	h.deleteS3Objects(r.Context(), attachmentURLs)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	h.publish(protocol.EventIssueDeleted, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue_id": id})
	slog.Info("issue deleted", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", uuidToString(issue.WorkspaceID))...)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ArchiveIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prevIssue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(prevIssue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	h.TaskService.CancelTasksForIssue(r.Context(), prevIssue.ID)

	issue, err := h.Queries.ArchiveIssue(r.Context(), db.ArchiveIssueParams{
		ID:          prevIssue.ID,
		ArchivedBy:  parseUUID(userID),
		WorkspaceID: prevIssue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive issue")
		return
	}

	_, _ = h.Queries.DismissInboxByIssueInWorkspace(r.Context(), db.DismissInboxByIssueInWorkspaceParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		TriagedBy:   parseUUID(userID),
	})

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	h.publish(protocol.EventIssueArchived, workspaceID, actorType, actorID, map[string]any{"issue": resp})
	slog.Info("issue archived", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RestoreIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prevIssue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(prevIssue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	issue, err := h.Queries.RestoreIssue(r.Context(), db.RestoreIssueParams{
		ID:          prevIssue.ID,
		WorkspaceID: prevIssue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore issue")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	h.publish(protocol.EventIssueRestored, workspaceID, actorType, actorID, map[string]any{"issue": resp})
	slog.Info("issue restored", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Batch operations
// ---------------------------------------------------------------------------

type BatchUpdateIssuesRequest struct {
	IssueIDs []string           `json:"issue_ids"`
	Updates  UpdateIssueRequest `json:"updates"`
}

func (h *Handler) BatchUpdateIssues(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req BatchUpdateIssuesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Detect which fields in "updates" were explicitly set (including null).
	var rawTop map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawTop)
	var rawUpdates map[string]json.RawMessage
	if raw, exists := rawTop["updates"]; exists {
		json.Unmarshal(raw, &rawUpdates)
	}
	if _, ok := rawUpdates["parent_issue_id"]; ok {
		writeError(w, http.StatusBadRequest, "parent_issue_id is not supported in batch updates")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	updated := 0
	for _, issueID := range req.IssueIDs {
		prevIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(issueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue
		}

		params := db.UpdateIssueParams{
			ID:            prevIssue.ID,
			AssigneeType:  prevIssue.AssigneeType,
			AssigneeID:    prevIssue.AssigneeID,
			ParentIssueID: prevIssue.ParentIssueID,
			ProjectID:     prevIssue.ProjectID,
			DueDate:       prevIssue.DueDate,
			StartDate:     prevIssue.StartDate,
			EndDate:       prevIssue.EndDate,
		}

		if req.Updates.Title != nil {
			params.Title = pgtype.Text{String: *req.Updates.Title, Valid: true}
		}
		if req.Updates.Description != nil {
			params.Description = pgtype.Text{String: *req.Updates.Description, Valid: true}
		}
		if req.Updates.Status != nil {
			params.Status = pgtype.Text{String: *req.Updates.Status, Valid: true}
		}
		if req.Updates.Priority != nil {
			params.Priority = pgtype.Text{String: *req.Updates.Priority, Valid: true}
		}
		if req.Updates.Position != nil {
			params.Position = pgtype.Float8{Float64: *req.Updates.Position, Valid: true}
		}
		if _, ok := rawUpdates["assignee_type"]; ok {
			if req.Updates.AssigneeType != nil {
				params.AssigneeType = pgtype.Text{String: *req.Updates.AssigneeType, Valid: true}
			} else {
				params.AssigneeType = pgtype.Text{Valid: false}
			}
		}
		if _, ok := rawUpdates["assignee_id"]; ok {
			if req.Updates.AssigneeID != nil {
				params.AssigneeID = parseUUID(*req.Updates.AssigneeID)
			} else {
				params.AssigneeID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["project_id"]; ok {
			if req.Updates.ProjectID != nil {
				validatedProjectID, err := h.validateIssueProject(r.Context(), workspaceID, req.Updates.ProjectID)
				if err != nil {
					continue
				}
				params.ProjectID = validatedProjectID
			} else {
				params.ProjectID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["due_date"]; ok {
			parsedDueDate, err := parseOptionalRFC3339Timestamp(req.Updates.DueDate, "due_date")
			if err != nil {
				continue
			}
			params.DueDate = parsedDueDate
		}
		if _, ok := rawUpdates["start_date"]; ok {
			parsedStartDate, err := parseOptionalRFC3339Timestamp(req.Updates.StartDate, "start_date")
			if err != nil {
				continue
			}
			params.StartDate = parsedStartDate
		}
		if _, ok := rawUpdates["end_date"]; ok {
			parsedEndDate, err := parseOptionalRFC3339Timestamp(req.Updates.EndDate, "end_date")
			if err != nil {
				continue
			}
			params.EndDate = parsedEndDate
		}

		if err := validateScheduleWindow(params.StartDate, params.EndDate); err != nil {
			continue
		}

		// Enforce agent visibility for batch assignment.
		if req.Updates.AssigneeType != nil && *req.Updates.AssigneeType == "agent" && req.Updates.AssigneeID != nil {
			if ok, _ := h.canAssignAgent(r.Context(), r, *req.Updates.AssigneeID, workspaceID); !ok {
				continue
			}
		}

		issue, err := h.Queries.UpdateIssue(r.Context(), params)
		if err != nil {
			slog.Warn("batch update issue failed", "issue_id", issueID, "error", err)
			continue
		}

		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		payload, assigneeChanged := buildIssueUpdateEventPayload(prevIssue, issue, resp, rawUpdates)

		h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, payload)

		if assigneeChanged {
			h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
			if h.shouldEnqueueAgentTask(r.Context(), issue) {
				h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
			}
		}

		updated++
	}

	slog.Info("batch update issues", append(logger.RequestAttrs(r), "count", updated)...)
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

type BatchDeleteIssuesRequest struct {
	IssueIDs []string `json:"issue_ids"`
}

type BatchArchiveIssuesRequest struct {
	IssueIDs []string `json:"issue_ids"`
}

func (h *Handler) BatchArchiveIssues(w http.ResponseWriter, r *http.Request) {
	var req BatchArchiveIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	archived := 0
	for _, issueID := range req.IssueIDs {
		prevIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(issueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue
		}

		h.TaskService.CancelTasksForIssue(r.Context(), prevIssue.ID)

		issue, err := h.Queries.ArchiveIssue(r.Context(), db.ArchiveIssueParams{
			ID:          prevIssue.ID,
			ArchivedBy:  parseUUID(userID),
			WorkspaceID: prevIssue.WorkspaceID,
		})
		if err != nil {
			slog.Warn("batch archive issue failed", "issue_id", issueID, "error", err)
			continue
		}

		_, _ = h.Queries.DismissInboxByIssueInWorkspace(r.Context(), db.DismissInboxByIssueInWorkspaceParams{
			WorkspaceID: issue.WorkspaceID,
			IssueID:     issue.ID,
			TriagedBy:   parseUUID(userID),
		})
		archived++
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventIssueBatchArchived, workspaceID, actorType, actorID, map[string]any{
		"issue_ids": req.IssueIDs,
		"count":     archived,
	})
	slog.Info("batch archive issues", append(logger.RequestAttrs(r), "count", archived)...)
	writeJSON(w, http.StatusOK, map[string]any{"archived": archived})
}

func (h *Handler) BatchDeleteIssues(w http.ResponseWriter, r *http.Request) {
	var req BatchDeleteIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	deleted := 0
	for _, issueID := range req.IssueIDs {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(issueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue
		}

		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)

		if err := h.Queries.DeleteIssue(r.Context(), parseUUID(issueID)); err != nil {
			slog.Warn("batch delete issue failed", "issue_id", issueID, "error", err)
			continue
		}

		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": issueID})
		deleted++
	}

	slog.Info("batch delete issues", append(logger.RequestAttrs(r), "count", deleted)...)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

const bulkCreateIssuesLimit = 100

type BulkCreateIssueItem struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
}

type BulkCreateIssuesRequest struct {
	Issues []BulkCreateIssueItem `json:"issues"`
}

type BulkCreateIssueError struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

func (h *Handler) BulkCreateIssues(w http.ResponseWriter, r *http.Request) {
	var req BulkCreateIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Issues) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "issues array must not be empty")
		return
	}
	if len(req.Issues) > bulkCreateIssuesLimit {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("too many issues: limit is %d", bulkCreateIssuesLimit))
		return
	}

	// Validate all rows before touching the DB.
	var rowErrors []BulkCreateIssueError
	for i, item := range req.Issues {
		if strings.TrimSpace(item.Title) == "" {
			rowErrors = append(rowErrors, BulkCreateIssueError{Index: i, Reason: "title is required"})
		}
	}
	if len(rowErrors) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"errors": rowErrors})
		return
	}

	workspaceID := resolveWorkspaceID(r)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	creatorType, actualCreatorID := h.resolveActor(r, creatorID, workspaceID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	if err := qtx.EnsureDefaultIssueTypes(r.Context(), parseUUID(workspaceID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issues")
		return
	}
	defaultIssueType, err := qtx.GetIssueTypeByKey(r.Context(), db.GetIssueTypeByKeyParams{
		WorkspaceID: parseUUID(workspaceID),
		Key:         "task",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issues")
		return
	}
	created := make([]IssueResponse, 0, len(req.Issues))

	for _, item := range req.Issues {
		issueNumber, err := qtx.IncrementIssueCounter(r.Context(), parseUUID(workspaceID))
		if err != nil {
			slog.Warn("bulk create: increment issue counter failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create issues")
			return
		}

		status := item.Status
		if status == "" {
			status = "backlog"
		}
		priority := item.Priority
		if priority == "" {
			priority = "none"
		}

		issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID: parseUUID(workspaceID),
			Title:       strings.TrimSpace(item.Title),
			Description: ptrToText(item.Description),
			Status:      status,
			Priority:    priority,
			CreatorType: creatorType,
			CreatorID:   parseUUID(actualCreatorID),
			IssueTypeID: defaultIssueType.ID,
			Position:    0,
			Number:      issueNumber,
		})
		if err != nil {
			slog.Warn("bulk create issue failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create issues")
			return
		}

		resp := issueToResponse(issue, "")
		created = append(created, resp)
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issues")
		return
	}

	slog.Info("bulk issues created", append(logger.RequestAttrs(r), "count", len(created), "workspace_id", workspaceID)...)
	h.publish(protocol.EventIssueBulkCreated, workspaceID, creatorType, actualCreatorID, map[string]any{"issues": created})

	writeJSON(w, http.StatusCreated, map[string]any{"issues": created})
}
