package workflows

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler hosts the REST surface for cerebro workflows. Wired into the
// router under /api/cerebro/workflows by the cerebro-workflows-routes
// CEREBRO-PATCH in server/cmd/server/router.go.
//
// All endpoints require an authenticated session (X-User-ID middleware) and
// scope every read/write through the workspace ID in context.
type Handler struct {
	Cerebro *cerebrodb.Queries
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

const (
	defaultRunsLimit = 50
	maxRunsLimit     = 200
)

type workflowResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	ProjectID     string          `json:"project_id,omitempty"`
	Name          string          `json:"name"`
	Enabled       bool            `json:"enabled"`
	TriggerType   string          `json:"trigger_type"`
	TriggerConfig json.RawMessage `json:"trigger_config"`
	Conditions    json.RawMessage `json:"conditions"`
	ActionType    string          `json:"action_type"`
	ActionConfig  json.RawMessage `json:"action_config"`
	CreatedByID   string          `json:"created_by_id"`
	CreatedByType string          `json:"created_by_type"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

func toWorkflowResponse(row cerebrodb.CerebroWorkflow) workflowResponse {
	out := workflowResponse{
		ID:            util.UUIDToString(row.ID),
		WorkspaceID:   util.UUIDToString(row.WorkspaceID),
		Name:          row.Name,
		Enabled:       row.Enabled,
		TriggerType:   row.TriggerType,
		TriggerConfig: nonEmptyJSON(row.TriggerConfig),
		Conditions:    nonEmptyJSON(row.Conditions),
		ActionType:    row.ActionType,
		ActionConfig:  nonEmptyJSON(row.ActionConfig),
		CreatedByID:   util.UUIDToString(row.CreatedByID),
		CreatedByType: row.CreatedByType,
		CreatedAt:     row.CreatedAt.Time.UTC().Format(rfc3339),
		UpdatedAt:     row.UpdatedAt.Time.UTC().Format(rfc3339),
	}
	if row.ProjectID.Valid {
		out.ProjectID = util.UUIDToString(row.ProjectID)
	}
	return out
}

type runResponse struct {
	ID            string `json:"id"`
	WorkflowID    string `json:"workflow_id"`
	WorkspaceID   string `json:"workspace_id"`
	TargetIssueID string `json:"target_issue_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	Status        string `json:"status"`
	Attempt       int32  `json:"attempt"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
	NextRetryAt   string `json:"next_retry_at,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func toRunResponse(row cerebrodb.CerebroWorkflowRun) runResponse {
	out := runResponse{
		ID:          util.UUIDToString(row.ID),
		WorkflowID:  util.UUIDToString(row.WorkflowID),
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		Status:      row.Status,
		Attempt:     row.Attempt,
		CreatedAt:   row.CreatedAt.Time.UTC().Format(rfc3339),
	}
	if row.TargetIssueID.Valid {
		out.TargetIssueID = util.UUIDToString(row.TargetIssueID)
	}
	if row.TaskID.Valid {
		out.TaskID = util.UUIDToString(row.TaskID)
	}
	if row.Error.Valid {
		out.Error = row.Error.String
	}
	if row.StartedAt.Valid {
		out.StartedAt = row.StartedAt.Time.UTC().Format(rfc3339)
	}
	if row.FinishedAt.Valid {
		out.FinishedAt = row.FinishedAt.Time.UTC().Format(rfc3339)
	}
	if row.NextRetryAt.Valid {
		out.NextRetryAt = row.NextRetryAt.Time.UTC().Format(rfc3339)
	}
	return out
}

type writeWorkflowRequest struct {
	Name          string          `json:"name"`
	Enabled       *bool           `json:"enabled,omitempty"`
	ProjectID     string          `json:"project_id,omitempty"`
	TriggerType   string          `json:"trigger_type"`
	TriggerConfig json.RawMessage `json:"trigger_config,omitempty"`
	Conditions    json.RawMessage `json:"conditions,omitempty"`
	ActionType    string          `json:"action_type"`
	ActionConfig  json.RawMessage `json:"action_config,omitempty"`
}

// validateWriteRequest enforces the shape we let through to the DB before we
// hit the CHECK constraints. Friendlier than a raw constraint violation in
// the UI, and keeps the API surface stable when the constraint list grows.
func validateWriteRequest(req writeWorkflowRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if !knownTrigger(req.TriggerType) {
		return errors.New("unknown trigger_type")
	}
	if !knownAction(req.ActionType) {
		return errors.New("unknown action_type")
	}
	return nil
}

func knownTrigger(t string) bool {
	switch t {
	case TriggerStatusChanged, TriggerDueDateReached, TriggerDueTimeReached:
		return true
	}
	return false
}

func knownAction(a string) bool {
	switch a {
	case ActionSetStatus, ActionCreateSubIssue, ActionSendReminder:
		return true
	}
	return false
}

// List handles GET /api/cerebro/workflows.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroWorkflows(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	out := make([]workflowResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toWorkflowResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": out})
}

// Get handles GET /api/cerebro/workflows/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	row, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, row.WorkspaceID) {
		// Workspace mismatch — surface as 404 to avoid leaking row existence
		// across workspace boundaries.
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, toWorkflowResponse(row))
}

// Create handles POST /api/cerebro/workflows.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}

	var req writeWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateWriteRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	creatorUUID, err := util.ParseUUID(actorID(r, userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}

	var projectID pgtype.UUID
	if req.ProjectID != "" {
		parsed, err := util.ParseUUID(req.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
		projectID = parsed
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row, err := h.Cerebro.CreateCerebroWorkflow(r.Context(), cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   wsUUID,
		ProjectID:     projectID,
		Name:          req.Name,
		Enabled:       enabled,
		TriggerType:   req.TriggerType,
		TriggerConfig: defaultJSON(req.TriggerConfig, "{}"),
		Conditions:    defaultJSON(req.Conditions, "[]"),
		ActionType:    req.ActionType,
		ActionConfig:  defaultJSON(req.ActionConfig, "{}"),
		CreatedByID:   creatorUUID,
		CreatedByType: actorType(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}
	writeJSON(w, http.StatusCreated, toWorkflowResponse(row))
}

// Update handles PUT /api/cerebro/workflows/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	var req writeWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateWriteRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var projectID pgtype.UUID
	if req.ProjectID != "" {
		parsed, err := util.ParseUUID(req.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
		projectID = parsed
	}

	row, err := h.Cerebro.UpdateCerebroWorkflow(r.Context(), cerebrodb.UpdateCerebroWorkflowParams{
		ID:            id,
		Name:          req.Name,
		Enabled:       enabled,
		ProjectID:     projectID,
		TriggerType:   req.TriggerType,
		TriggerConfig: defaultJSON(req.TriggerConfig, "{}"),
		Conditions:    defaultJSON(req.Conditions, "[]"),
		ActionType:    req.ActionType,
		ActionConfig:  defaultJSON(req.ActionConfig, "{}"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow")
		return
	}
	writeJSON(w, http.StatusOK, toWorkflowResponse(row))
}

// Toggle handles POST /api/cerebro/workflows/{id}/toggle. Body: { enabled: bool }.
func (h *Handler) Toggle(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.Cerebro.SetCerebroWorkflowEnabled(r.Context(), cerebrodb.SetCerebroWorkflowEnabledParams{
		ID:      id,
		Enabled: req.Enabled,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to toggle workflow")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// Delete handles DELETE /api/cerebro/workflows/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err := h.Cerebro.DeleteCerebroWorkflow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Runs handles GET /api/cerebro/workflows/{id}/runs (per-workflow log) and
// GET /api/cerebro/workflows/runs (workspace-wide log when no id is set).
// Pagination is offset/limit, capped at maxRunsLimit.
func (h *Handler) Runs(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	limit, offset := parseLimitOffset(r, defaultRunsLimit, maxRunsLimit)

	idStr := chi.URLParam(r, "id")
	ctx := r.Context()
	var rows []cerebrodb.CerebroWorkflowRun
	if idStr != "" {
		wfID, err := util.ParseUUID(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		existing, err := h.Cerebro.GetCerebroWorkflow(ctx, wfID)
		if err != nil || !inWorkspace(r, existing.WorkspaceID) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		rows, err = h.Cerebro.ListCerebroWorkflowRuns(ctx, cerebrodb.ListCerebroWorkflowRunsParams{
			WorkflowID: wfID,
			Limit:      int32(limit),
			Offset:     int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list runs")
			return
		}
	} else {
		var err error
		rows, err = h.Cerebro.ListWorkspaceCerebroWorkflowRuns(ctx, cerebrodb.ListWorkspaceCerebroWorkflowRunsParams{
			WorkspaceID: wsUUID,
			Limit:       int32(limit),
			Offset:      int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list runs")
			return
		}
	}

	out := make([]runResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRunResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"runs":   out,
		"limit":  limit,
		"offset": offset,
	})
}

// --- helpers ---

const rfc3339 = "2006-01-02T15:04:05Z07:00"

func workspaceUUIDOr400(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	return wsUUID, true
}

func pathUUIDOr400(w http.ResponseWriter, r *http.Request, key string) (pgtype.UUID, bool) {
	raw := chi.URLParam(r, key)
	id, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+key)
		return pgtype.UUID{}, false
	}
	return id, true
}

func inWorkspace(r *http.Request, wsID pgtype.UUID) bool {
	got := middleware.WorkspaceIDFromContext(r.Context())
	return util.UUIDToString(wsID) == got
}

func parseLimitOffset(r *http.Request, defLimit, maxLimit int) (int, int) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = defLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func actorID(r *http.Request, userID string) string {
	if v := r.Header.Get("X-Agent-ID"); v != "" {
		return v
	}
	return userID
}

func actorType(r *http.Request) string {
	if r.Header.Get("X-Agent-ID") != "" {
		return "agent"
	}
	return "member"
}

func defaultJSON(raw json.RawMessage, fallback string) []byte {
	if len(raw) == 0 {
		return []byte(fallback)
	}
	return raw
}

func nonEmptyJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
