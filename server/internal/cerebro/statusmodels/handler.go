// Package statusmodels hosts the REST surface for cerebro workflow v2a
// status models (FIR-1550): reusable, workspace-level status pipelines a
// project can opt into.
//
// Design (Jesper's locked decision, FIR-1550): every custom status is bound
// to one of the 7 upstream base statuses. The model only renames / recolors /
// reorders what the user sees; reporting, inbox, and agents keep reading the
// base status unchanged, so the upstream IssueStatus enum stays the single
// source of truth and upstream syncs stay clean.
//
// Wired into the router under /api/cerebro/status-models and
// /api/cerebro/projects/{projectId}/status-model by the
// cerebro-status-models-routes CEREBRO-PATCH in server/cmd/server/router.go.
package statusmodels

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

const rfc3339 = "2006-01-02T15:04:05Z07:00"

// minStatuses is the floor enforced by the issue's "Done" criterion: an admin
// must define at least 3 of their own statuses for a model to be useful.
const minStatuses = 3

// baseStatuses mirrors the 7 upstream IssueStatus values
// (packages/core/types/issue.ts). Kept as a local set so the cerebro zone
// validates without importing the upstream handler package; if upstream adds a
// status, this list and the issue.ts enum drift together by design and the
// validation simply rejects the unknown value until updated.
var baseStatuses = map[string]bool{
	"backlog":     true,
	"todo":        true,
	"in_progress": true,
	"in_review":   true,
	"done":        true,
	"blocked":     true,
	"cancelled":   true,
}

// Handler exposes status-model CRUD plus per-project selection. All endpoints
// require an authenticated session (X-User-ID middleware) and scope every
// read/write through the workspace ID in context. Reads are open to any
// workspace member; mutating routes (create/update/delete model, set/clear
// project model) are gated to owner/admin in the router (FIR-1550).
type Handler struct {
	Cerebro *cerebrodb.Queries
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

// statusEntry is one custom status within a model. position is derived from
// array order on write, so callers never have to keep it consistent.
type statusEntry struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Color      string `json:"color"`
	BaseStatus string `json:"base_status"`
	Position   int    `json:"position"`
}

type statusModelResponse struct {
	ID            string        `json:"id"`
	WorkspaceID   string        `json:"workspace_id"`
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Statuses      []statusEntry `json:"statuses"`
	ProjectCount  int64         `json:"project_count"`
	CreatedByID   string        `json:"created_by_id"`
	CreatedByType string        `json:"created_by_type"`
	CreatedAt     string        `json:"created_at"`
	UpdatedAt     string        `json:"updated_at"`
}

func toStatusModelResponse(row cerebrodb.CerebroStatusModel, projectCount int64) statusModelResponse {
	out := statusModelResponse{
		ID:            util.UUIDToString(row.ID),
		WorkspaceID:   util.UUIDToString(row.WorkspaceID),
		Name:          row.Name,
		Statuses:      decodeStatuses(row.Statuses),
		ProjectCount:  projectCount,
		CreatedByID:   util.UUIDToString(row.CreatedByID),
		CreatedByType: row.CreatedByType,
		CreatedAt:     row.CreatedAt.Time.UTC().Format(rfc3339),
		UpdatedAt:     row.UpdatedAt.Time.UTC().Format(rfc3339),
	}
	if row.Description.Valid {
		out.Description = row.Description.String
	}
	return out
}

// decodeStatuses parses the JSONB array, returning an empty slice on any
// malformed payload so the response never errors out the UI.
func decodeStatuses(raw []byte) []statusEntry {
	out := []statusEntry{}
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return []statusEntry{}
	}
	return out
}

type projectAssignmentResponse struct {
	ProjectID     string `json:"project_id"`
	WorkspaceID   string `json:"workspace_id"`
	StatusModelID string `json:"status_model_id"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func toProjectAssignmentResponse(row cerebrodb.CerebroProjectStatusModel) projectAssignmentResponse {
	return projectAssignmentResponse{
		ProjectID:     util.UUIDToString(row.ProjectID),
		WorkspaceID:   util.UUIDToString(row.WorkspaceID),
		StatusModelID: util.UUIDToString(row.StatusModelID),
		CreatedAt:     row.CreatedAt.Time.UTC().Format(rfc3339),
		UpdatedAt:     row.UpdatedAt.Time.UTC().Format(rfc3339),
	}
}

type writeStatusModelRequest struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Statuses    []statusEntry `json:"statuses"`
}

// validateWriteRequest enforces the model shape before it hits the DB:
// a name, at least 3 statuses, and each status bound to a known base status
// with a non-empty key/label. Keys must be unique within the model.
func validateWriteRequest(req writeStatusModelRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name is required")
	}
	if len(req.Statuses) < minStatuses {
		return fmt.Errorf("a model needs at least %d statuses", minStatuses)
	}
	seen := make(map[string]bool, len(req.Statuses))
	for i, s := range req.Statuses {
		if strings.TrimSpace(s.Key) == "" {
			return fmt.Errorf("status %d: key is required", i+1)
		}
		if strings.TrimSpace(s.Label) == "" {
			return fmt.Errorf("status %d: label is required", i+1)
		}
		if !baseStatuses[s.BaseStatus] {
			return fmt.Errorf("status %d: unknown base_status %q", i+1, s.BaseStatus)
		}
		if seen[s.Key] {
			return fmt.Errorf("duplicate status key %q", s.Key)
		}
		seen[s.Key] = true
	}
	return nil
}

// normalizeStatuses assigns position by array order and serializes to JSONB.
func normalizeStatuses(in []statusEntry) ([]byte, error) {
	out := make([]statusEntry, len(in))
	for i, s := range in {
		s.Position = i
		out[i] = s
	}
	return json.Marshal(out)
}

// List handles GET /api/cerebro/status-models. Includes project_count per
// model so the settings overview panel can show usage without a second call.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroStatusModels(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list status models")
		return
	}
	out := make([]statusModelResponse, 0, len(rows))
	for _, row := range rows {
		count, err := h.Cerebro.CountProjectsUsingStatusModel(r.Context(), row.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to count projects")
			return
		}
		out = append(out, toStatusModelResponse(row, count))
	}
	writeJSON(w, http.StatusOK, map[string]any{"status_models": out})
}

// Assignments handles GET /api/cerebro/status-models/assignments. Returns the
// project→model pairs in the workspace so the overview panel can render
// "projects using a custom workflow" with project names joined client-side.
func (h *Handler) Assignments(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListProjectStatusModelsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list assignments")
		return
	}
	out := make([]projectAssignmentResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toProjectAssignmentResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignments": out})
}

// Get handles GET /api/cerebro/status-models/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	row, err := h.Cerebro.GetCerebroStatusModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "status model not found")
		return
	}
	if !inWorkspace(r, row.WorkspaceID) {
		writeError(w, http.StatusNotFound, "status model not found")
		return
	}
	count, err := h.Cerebro.CountProjectsUsingStatusModel(r.Context(), row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count projects")
		return
	}
	writeJSON(w, http.StatusOK, toStatusModelResponse(row, count))
}

// Create handles POST /api/cerebro/status-models.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	var req writeStatusModelRequest
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
	statuses, err := normalizeStatuses(req.Statuses)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid statuses")
		return
	}
	row, err := h.Cerebro.CreateCerebroStatusModel(r.Context(), cerebrodb.CreateCerebroStatusModelParams{
		WorkspaceID:   wsUUID,
		Name:          strings.TrimSpace(req.Name),
		Description:   textOrNull(req.Description),
		Statuses:      statuses,
		CreatedByID:   creatorUUID,
		CreatedByType: actorType(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create status model")
		return
	}
	writeJSON(w, http.StatusCreated, toStatusModelResponse(row, 0))
}

// Update handles PUT /api/cerebro/status-models/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroStatusModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "status model not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "status model not found")
		return
	}
	var req writeStatusModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateWriteRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	statuses, err := normalizeStatuses(req.Statuses)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid statuses")
		return
	}
	row, err := h.Cerebro.UpdateCerebroStatusModel(r.Context(), cerebrodb.UpdateCerebroStatusModelParams{
		ID:          id,
		Name:        strings.TrimSpace(req.Name),
		Description: textOrNull(req.Description),
		Statuses:    statuses,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update status model")
		return
	}
	count, err := h.Cerebro.CountProjectsUsingStatusModel(r.Context(), row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count projects")
		return
	}
	writeJSON(w, http.StatusOK, toStatusModelResponse(row, count))
}

// Delete handles DELETE /api/cerebro/status-models/{id}. Blocked while the
// model is in use by any project — predictable, and no project silently loses
// its workflow. The UI surfaces the count so the admin can detach first.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroStatusModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "status model not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "status model not found")
		return
	}
	count, err := h.Cerebro.CountProjectsUsingStatusModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count projects")
		return
	}
	if count > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":         fmt.Sprintf("in use by %d project(s) — detach them first", count),
			"project_count": count,
		})
		return
	}
	if err := h.Cerebro.DeleteCerebroStatusModel(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete status model")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetProjectModel handles GET /api/cerebro/projects/{projectId}/status-model.
// Returns the assignment (404 when the project uses the default statuses).
func (h *Handler) GetProjectModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := pathUUIDOr400(w, r, "projectId")
	if !ok {
		return
	}
	row, err := h.Cerebro.GetProjectStatusModel(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no status model assigned")
		return
	}
	if !inWorkspace(r, row.WorkspaceID) {
		writeError(w, http.StatusNotFound, "no status model assigned")
		return
	}
	writeJSON(w, http.StatusOK, toProjectAssignmentResponse(row))
}

type assignProjectModelRequest struct {
	StatusModelID string `json:"status_model_id"`
}

// SetProjectModel handles PUT /api/cerebro/projects/{projectId}/status-model.
// Body: { status_model_id }. Upserts the project's selected model.
func (h *Handler) SetProjectModel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	projectID, ok := pathUUIDOr400(w, r, "projectId")
	if !ok {
		return
	}
	var req assignProjectModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	modelUUID, err := util.ParseUUID(req.StatusModelID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid status_model_id")
		return
	}
	// Guard: the model must exist and belong to this workspace.
	model, err := h.Cerebro.GetCerebroStatusModel(r.Context(), modelUUID)
	if err != nil || !inWorkspace(r, model.WorkspaceID) {
		writeError(w, http.StatusBadRequest, "status model not found in workspace")
		return
	}
	actorUUID, err := util.ParseUUID(actorID(r, userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}
	row, err := h.Cerebro.UpsertProjectStatusModel(r.Context(), cerebrodb.UpsertProjectStatusModelParams{
		ProjectID:      projectID,
		WorkspaceID:    wsUUID,
		StatusModelID:  modelUUID,
		AssignedByID:   actorUUID,
		AssignedByType: actorType(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign status model")
		return
	}
	writeJSON(w, http.StatusOK, toProjectAssignmentResponse(row))
}

// ClearProjectModel handles DELETE /api/cerebro/projects/{projectId}/status-model.
// Reverts the project to the default 7 statuses.
func (h *Handler) ClearProjectModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := pathUUIDOr400(w, r, "projectId")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetProjectStatusModel(r.Context(), projectID)
	if err != nil {
		// Already on default — idempotent success.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "no status model assigned")
		return
	}
	if err := h.Cerebro.DeleteProjectStatusModel(r.Context(), projectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear status model")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- local helpers (kept in-package so the cerebro zone stays self-contained) ---

func textOrNull(s string) pgtype.Text {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func workspaceUUIDOr400(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	wsUUID, err := util.ParseUUID(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	return wsUUID, true
}

func pathUUIDOr400(w http.ResponseWriter, r *http.Request, key string) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(chi.URLParam(r, key))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+key)
		return pgtype.UUID{}, false
	}
	return id, true
}

func inWorkspace(r *http.Request, wsID pgtype.UUID) bool {
	return util.UUIDToString(wsID) == middleware.WorkspaceIDFromContext(r.Context())
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
