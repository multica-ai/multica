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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxStarter abstracts transaction creation. *pgxpool.Pool satisfies it.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

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
//
// Upstream is the optional upstream queries handle used by the per-issue
// custom-status routes (v2b, FIR-1550): the cerebro sidecar mirrors the
// upstream issue's base status, so setting a custom status writes both.
// When nil, the per-issue routes degrade to read-only.
//
// Tx is the transaction starter (e.g. *pgxpool.Pool) used to make the two
// writes in SetIssueCustomStatus atomic: the upstream base-status mirror and
// the cerebro sidecar upsert must commit together, or neither must commit.
// When nil, SetIssueCustomStatus falls back to non-atomic writes — kept only
// so existing unit tests that don't construct a pool still compile; the
// router always wires a pool in production.
type Handler struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
	Tx       TxStarter
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

// WithUpstream wires the upstream queries handle so the per-issue
// custom-status routes can mirror the base status onto the upstream issue
// row. Returns the handler for chaining at construction.
func (h *Handler) WithUpstream(queries *db.Queries) *Handler {
	h.Upstream = queries
	return h
}

// WithTx wires the transaction starter used by SetIssueCustomStatus to commit
// the upstream base-status mirror and the cerebro sidecar atomically.
func (h *Handler) WithTx(tx TxStarter) *Handler {
	h.Tx = tx
	return h
}

// statusEntry is one custom status within a model. position is derived from
// array order on write, so callers never have to keep it consistent.
//
// v2b additions (FIR-1550):
//   - Description: required text agents read to know what the status means.
//     Without it, an agent sees a label like "Plan-review" and has nothing to
//     reason about.
//   - Triggers: optional automation. AssignToID/AssignToType pins the issue
//     to a member/agent when it lands here; FireAgentID enqueues a new run
//     for that agent. Both are independent and either or both can be unset.
type statusEntry struct {
	Key         string          `json:"key"`
	Label       string          `json:"label"`
	Color       string          `json:"color"`
	BaseStatus  string          `json:"base_status"`
	Position    int             `json:"position"`
	Description string          `json:"description,omitempty"`
	Triggers    *statusTriggers `json:"triggers,omitempty"`
}

// statusTriggers is the optional automation block attached to a custom
// status. Empty / unset fields mean "no automation for this lever".
type statusTriggers struct {
	AssignToID   string `json:"assign_to_id,omitempty"`
	AssignToType string `json:"assign_to_type,omitempty"` // "member" | "agent"
	FireAgentID  string `json:"fire_agent_id,omitempty"`
}

type statusModelResponse struct {
	ID               string        `json:"id"`
	WorkspaceID      string        `json:"workspace_id"`
	Name             string        `json:"name"`
	Description      string        `json:"description,omitempty"`
	Statuses         []statusEntry `json:"statuses"`
	ProjectCount     int64         `json:"project_count"`
	WorkspaceDefault bool          `json:"workspace_default"`
	CreatedByID      string        `json:"created_by_id"`
	CreatedByType    string        `json:"created_by_type"`
	CreatedAt        string        `json:"created_at"`
	UpdatedAt        string        `json:"updated_at"`
}

func toStatusModelResponse(row cerebrodb.CerebroStatusModel, projectCount int64) statusModelResponse {
	out := statusModelResponse{
		ID:               util.UUIDToString(row.ID),
		WorkspaceID:      util.UUIDToString(row.WorkspaceID),
		Name:             row.Name,
		Statuses:         decodeStatuses(row.Statuses),
		ProjectCount:     projectCount,
		WorkspaceDefault: row.WorkspaceDefault,
		CreatedByID:      util.UUIDToString(row.CreatedByID),
		CreatedByType:    row.CreatedByType,
		CreatedAt:        row.CreatedAt.Time.UTC().Format(rfc3339),
		UpdatedAt:        row.UpdatedAt.Time.UTC().Format(rfc3339),
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
// with a non-empty key/label/description. Keys must be unique within the
// model. Triggers are optional and validated structurally only — the
// referenced member/agent identity is not resolved here (the trigger engine
// resolves at fire time and best-efforts a missing target).
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
		if strings.TrimSpace(s.Description) == "" {
			return fmt.Errorf("status %d: description is required (agents read this to know what the status means)", i+1)
		}
		if !baseStatuses[s.BaseStatus] {
			return fmt.Errorf("status %d: unknown base_status %q", i+1, s.BaseStatus)
		}
		if seen[s.Key] {
			return fmt.Errorf("duplicate status key %q", s.Key)
		}
		seen[s.Key] = true
		if s.Triggers != nil {
			if t := s.Triggers.AssignToType; t != "" && t != "member" && t != "agent" {
				return fmt.Errorf("status %d: invalid assign_to_type %q", i+1, t)
			}
			if (s.Triggers.AssignToID == "") != (s.Triggers.AssignToType == "") {
				return fmt.Errorf("status %d: assign_to_id and assign_to_type must be set together", i+1)
			}
		}
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

// SetWorkspaceDefault handles PATCH /api/cerebro/status-models/{id}/set-default.
// Marks the given model as the workspace default; clears any existing default
// first (atomically, in a single transaction). Admin/owner only (router gate).
//
// FIR-2800: workspace-level default model. New projects created after this is
// set are auto-assigned the default model by the project creation hook.
func (h *Handler) SetWorkspaceDefault(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
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
	// Two writes in one transaction: clear old default, set new one.
	if h.Tx != nil {
		tx, err := h.Tx.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start transaction")
			return
		}
		defer tx.Rollback(r.Context())
		txQ := h.Cerebro.WithTx(tx)
		if err := txQ.ClearWorkspaceDefaultStatusModel(r.Context(), wsUUID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear existing default")
			return
		}
		row, err := txQ.SetWorkspaceDefaultStatusModel(r.Context(), cerebrodb.SetWorkspaceDefaultStatusModelParams{
			ID:          id,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set workspace default")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit")
			return
		}
		count, _ := h.Cerebro.CountProjectsUsingStatusModel(r.Context(), row.ID)
		writeJSON(w, http.StatusOK, toStatusModelResponse(row, count))
		return
	}
	// Fallback (no pool, e.g. unit tests): non-atomic path.
	if err := h.Cerebro.ClearWorkspaceDefaultStatusModel(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear existing default")
		return
	}
	row, err := h.Cerebro.SetWorkspaceDefaultStatusModel(r.Context(), cerebrodb.SetWorkspaceDefaultStatusModelParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set workspace default")
		return
	}
	count, _ := h.Cerebro.CountProjectsUsingStatusModel(r.Context(), row.ID)
	writeJSON(w, http.StatusOK, toStatusModelResponse(row, count))
}

// ClearWorkspaceDefault handles DELETE /api/cerebro/status-models/default.
// Removes the workspace default flag — new projects will not be auto-assigned
// any model until a new default is configured. Admin/owner only (router gate).
func (h *Handler) ClearWorkspaceDefault(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	if err := h.Cerebro.ClearWorkspaceDefaultStatusModel(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear workspace default")
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
	// Mapping is the admin's per-base choice from the assign modal:
	// base_status -> custom_status_key. When present, every existing issue in
	// the project that currently sits on a mapped base is pinned to the chosen
	// custom status right away (no silent omplacering — the board matches what
	// the admin confirmed). Bases omitted from the map fall back to the lazy
	// auto-resolver (first custom status under that base on next update).
	Mapping map[string]string `json:"mapping,omitempty"`
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
	// Validate the admin's mapping against the model BEFORE assigning, so a
	// malformed mapping never leaves the project assigned-but-unmapped.
	entries := decodeStatuses(model.Statuses)
	if msg := validateMapping(req.Mapping, entries); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
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
	// Apply the admin's mapping to existing issues so the board immediately
	// reflects the confirmed choice. Best-effort: the assignment already
	// committed, so a partial apply is logged, never rolled back.
	if len(req.Mapping) > 0 {
		h.applyProjectMapping(r.Context(), projectID, wsUUID, modelUUID, req.Mapping, actorUUID, actorType(r))
	}
	writeJSON(w, http.StatusOK, toProjectAssignmentResponse(row))
}

// validateMapping checks every base_status -> custom_status_key pair against
// the model: the key must exist and its base must equal the map key. Returns
// "" when valid (or empty), else a 400-worthy message. The admin built the
// mapping from this model, so a mismatch is a real client error, not a
// degrade case.
func validateMapping(mapping map[string]string, entries []statusEntry) string {
	for base, key := range mapping {
		if !baseStatuses[base] {
			return fmt.Sprintf("unknown base_status %q in mapping", base)
		}
		entry := findStatusEntry(entries, key)
		if entry == nil || entry.BaseStatus != base {
			return fmt.Sprintf("custom_status_key %q is not valid for base %q in this model", key, base)
		}
	}
	return ""
}

// applyProjectMapping pins every existing issue in the project that currently
// sits on a mapped base to the admin's chosen custom_status_key. Pages through
// the project's issues (admin scope) and upserts the sidecar per issue. The
// issue's upstream base status is untouched — only the cerebro pin moves — so
// no upstream write is needed.
func (h *Handler) applyProjectMapping(
	ctx context.Context,
	projectID, workspaceID, modelID pgtype.UUID,
	mapping map[string]string,
	actorUUID pgtype.UUID,
	actorTypeStr string,
) {
	if h.Upstream == nil {
		return
	}
	if actorTypeStr != "member" && actorTypeStr != "agent" {
		actorTypeStr = "system"
	}
	const pageSize = 500
	pinned, failed := 0, 0
	for offset := int32(0); ; offset += pageSize {
		rows, err := h.Upstream.ListIssues(ctx, db.ListIssuesParams{
			WorkspaceID: workspaceID,
			Limit:       pageSize,
			Offset:      offset,
			IsAdmin:     true, // route is owner/admin-gated; see all project issues
			ProjectID:   projectID,
		})
		if err != nil {
			slog.Warn("cerebro mapping apply: list issues failed",
				"project_id", util.UUIDToString(projectID), "error", err)
			return
		}
		for _, issue := range rows {
			key, ok := mapping[issue.Status]
			if !ok {
				continue
			}
			if _, err := h.Cerebro.UpsertCerebroIssueStatus(ctx, cerebrodb.UpsertCerebroIssueStatusParams{
				IssueID:         issue.ID,
				WorkspaceID:     workspaceID,
				StatusModelID:   modelID,
				CustomStatusKey: key,
				BaseStatus:      issue.Status,
				SetByID:         actorUUID,
				SetByType:       actorTypeStr,
			}); err != nil {
				failed++
				continue
			}
			pinned++
		}
		if len(rows) < pageSize {
			break
		}
	}
	slog.Info("cerebro mapping applied to existing issues",
		"project_id", util.UUIDToString(projectID),
		"model_id", util.UUIDToString(modelID),
		"pinned", pinned, "failed", failed)
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

// --- v2b: per-issue custom status (FIR-1550) -------------------------------

// issueCustomStatusResponse is the per-issue sidecar payload.
type issueCustomStatusResponse struct {
	IssueID         string `json:"issue_id"`
	WorkspaceID     string `json:"workspace_id"`
	StatusModelID   string `json:"status_model_id"`
	CustomStatusKey string `json:"custom_status_key"`
	BaseStatus      string `json:"base_status"`
	SetByID         string `json:"set_by_id"`
	SetByType       string `json:"set_by_type"`
	// Label and Description are joined in from the model so the UI / agent
	// never has to look up the model separately.
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toIssueCustomStatusResponse(row cerebrodb.CerebroIssueStatus, entry *statusEntry) issueCustomStatusResponse {
	out := issueCustomStatusResponse{
		IssueID:         util.UUIDToString(row.IssueID),
		WorkspaceID:     util.UUIDToString(row.WorkspaceID),
		StatusModelID:   util.UUIDToString(row.StatusModelID),
		CustomStatusKey: row.CustomStatusKey,
		BaseStatus:      row.BaseStatus,
		SetByID:         util.UUIDToString(row.SetByID),
		SetByType:       row.SetByType,
		CreatedAt:       row.CreatedAt.Time.UTC().Format(rfc3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(rfc3339),
	}
	if entry != nil {
		out.Label = entry.Label
		out.Description = entry.Description
	}
	return out
}

type setIssueCustomStatusRequest struct {
	StatusModelID   string `json:"status_model_id"`
	CustomStatusKey string `json:"custom_status_key"`
}

// GetIssueCustomStatus handles GET /api/cerebro/issues/{issueId}/custom-status.
// Returns 404 when the issue has no pinned custom status (default mode).
func (h *Handler) GetIssueCustomStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issueID, ok := pathUUIDOr400(w, r, "issueId")
	if !ok {
		return
	}
	row, err := h.Cerebro.GetCerebroIssueStatus(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no custom status assigned")
		return
	}
	if !inWorkspace(r, row.WorkspaceID) {
		writeError(w, http.StatusNotFound, "no custom status assigned")
		return
	}
	entry := h.lookupStatusEntry(r.Context(), row.StatusModelID, row.CustomStatusKey)
	writeJSON(w, http.StatusOK, toIssueCustomStatusResponse(row, entry))
}

// SetIssueCustomStatus handles PUT /api/cerebro/issues/{issueId}/custom-status.
// Body: { status_model_id, custom_status_key }. Resolves the entry, mirrors
// the base status onto the upstream issue (so reports / inbox / upstream
// automation stay correct), and upserts the sidecar.
func (h *Handler) SetIssueCustomStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	issueID, ok := pathUUIDOr400(w, r, "issueId")
	if !ok {
		return
	}
	var req setIssueCustomStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	modelUUID, err := util.ParseUUID(req.StatusModelID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid status_model_id")
		return
	}
	if strings.TrimSpace(req.CustomStatusKey) == "" {
		writeError(w, http.StatusBadRequest, "custom_status_key is required")
		return
	}
	model, err := h.Cerebro.GetCerebroStatusModel(r.Context(), modelUUID)
	if err != nil || !inWorkspace(r, model.WorkspaceID) {
		writeError(w, http.StatusBadRequest, "status model not found in workspace")
		return
	}
	entry := findStatusEntry(decodeStatuses(model.Statuses), req.CustomStatusKey)
	if entry == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("custom_status_key %q is not in this model", req.CustomStatusKey))
		return
	}
	actorUUID, err := util.ParseUUID(actorID(r, userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}
	// Atomic write (FIR-1550 v2b): the upstream base-status mirror and the
	// cerebro sidecar upsert must commit together. Mia caught the original
	// non-atomic version — if the sidecar write failed after the base mirror
	// succeeded, the issue moved to the base status without the custom-status
	// pin and the two stores drifted apart. Wrapping both writes in one tx
	// (issuing a single Rollback via defer + an explicit Commit at the end)
	// gives us all-or-nothing semantics.
	row, err := h.setIssueCustomStatusAtomic(r.Context(), setIssueCustomStatusArgs{
		IssueID:         issueID,
		WorkspaceID:     wsUUID,
		StatusModelID:   modelUUID,
		CustomStatusKey: req.CustomStatusKey,
		BaseStatus:      entry.BaseStatus,
		SetByID:         actorUUID,
		SetByType:       actorType(r),
	})
	if err != nil {
		switch {
		case errors.Is(err, errMirrorIssueNotFound):
			writeError(w, http.StatusNotFound, "issue not found in workspace")
		default:
			writeError(w, http.StatusInternalServerError, "failed to set custom status")
		}
		return
	}
	writeJSON(w, http.StatusOK, toIssueCustomStatusResponse(row, entry))
}

// errMirrorIssueNotFound signals that the upstream issue either does not
// exist or lives in a different workspace; the caller maps it to 404.
var errMirrorIssueNotFound = errors.New("issue not found in workspace")

// setIssueCustomStatusArgs bundles the parameters resolved by the HTTP
// handler before crossing into the atomic write path.
type setIssueCustomStatusArgs struct {
	IssueID         pgtype.UUID
	WorkspaceID     pgtype.UUID
	StatusModelID   pgtype.UUID
	CustomStatusKey string
	BaseStatus      string
	SetByID         pgtype.UUID
	SetByType       string
}

// setIssueCustomStatusAtomic performs the two writes (upstream base mirror +
// cerebro sidecar upsert) inside a single transaction so they succeed or fail
// together. Falls back to non-atomic writes only when Tx is unwired (test
// shims that don't construct a pool); production always passes a pool.
func (h *Handler) setIssueCustomStatusAtomic(ctx context.Context, args setIssueCustomStatusArgs) (cerebrodb.CerebroIssueStatus, error) {
	if h.Tx == nil {
		return h.setIssueCustomStatusFallback(ctx, args)
	}
	tx, err := h.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.CerebroIssueStatus{}, err
	}
	defer tx.Rollback(ctx)
	if h.Upstream != nil {
		if _, mirrorErr := h.Upstream.WithTx(tx).UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          args.IssueID,
			Status:      args.BaseStatus,
			WorkspaceID: args.WorkspaceID,
		}); mirrorErr != nil {
			return cerebrodb.CerebroIssueStatus{}, errMirrorIssueNotFound
		}
	}
	row, err := h.Cerebro.WithTx(tx).UpsertCerebroIssueStatus(ctx, cerebrodb.UpsertCerebroIssueStatusParams{
		IssueID:         args.IssueID,
		WorkspaceID:     args.WorkspaceID,
		StatusModelID:   args.StatusModelID,
		CustomStatusKey: args.CustomStatusKey,
		BaseStatus:      args.BaseStatus,
		SetByID:         args.SetByID,
		SetByType:       args.SetByType,
	})
	if err != nil {
		return cerebrodb.CerebroIssueStatus{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.CerebroIssueStatus{}, err
	}
	return row, nil
}

// setIssueCustomStatusFallback preserves the original (non-atomic) order for
// the few code paths that construct a Handler without a Tx — they get the
// pre-fix behaviour and should be migrated.
func (h *Handler) setIssueCustomStatusFallback(ctx context.Context, args setIssueCustomStatusArgs) (cerebrodb.CerebroIssueStatus, error) {
	if h.Upstream != nil {
		if _, mirrorErr := h.Upstream.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          args.IssueID,
			Status:      args.BaseStatus,
			WorkspaceID: args.WorkspaceID,
		}); mirrorErr != nil {
			return cerebrodb.CerebroIssueStatus{}, errMirrorIssueNotFound
		}
	}
	return h.Cerebro.UpsertCerebroIssueStatus(ctx, cerebrodb.UpsertCerebroIssueStatusParams{
		IssueID:         args.IssueID,
		WorkspaceID:     args.WorkspaceID,
		StatusModelID:   args.StatusModelID,
		CustomStatusKey: args.CustomStatusKey,
		BaseStatus:      args.BaseStatus,
		SetByID:         args.SetByID,
		SetByType:       args.SetByType,
	})
}

// ClearIssueCustomStatus handles DELETE /api/cerebro/issues/{issueId}/custom-status.
// Removes the sidecar pin — the issue falls back to the project's default
// mapping (which still resolves to a custom status if the project has a model;
// only the manual pin is cleared).
func (h *Handler) ClearIssueCustomStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issueID, ok := pathUUIDOr400(w, r, "issueId")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroIssueStatus(r.Context(), issueID)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "no custom status assigned")
		return
	}
	if err := h.Cerebro.DeleteCerebroIssueStatus(r.Context(), issueID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear custom status")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ErrCustomStatusKeyMismatch signals that the caller explicitly passed a
// custom_status_key that either does not exist in the project's model or
// is bound to a different base than the one the issue is ending up on.
// UpdateIssue translates this into a 400 so the client knows the picker
// payload is malformed (vs. server-side problems that fall through as 500).
var ErrCustomStatusKeyMismatch = errors.New("custom_status_key is not valid for the resulting base status in this project's model")

// ResolveCustomStatusAfterBaseChange is the hook called from the upstream
// issue update handler (via a tiny CEREBRO-PATCH) once the upstream base
// status has been updated. It serves two callers:
//
//   - **Auto-resolve** (requestedKey == ""): if the issue's project has a
//     model and the current sidecar pin no longer matches the new base,
//     pick the first custom_status in the model whose base matches. Keeps
//     the existing pin when its base still matches (a picker change inside
//     the same base column is a no-op for the sidecar).
//
//   - **Explicit pick** (requestedKey != ""): the caller picked a specific
//     custom status (status-picker on a board with a model, or any other
//     UI surface that knows the modelled key). Validate the key exists in
//     the model AND its base matches the new base, then pin it. Mismatch
//     returns ErrCustomStatusKeyMismatch so the HTTP layer can map it to
//     a 400.
//
// When the project has no model OR no entry matches the new base in the
// auto-resolve path, the sidecar is cleared so the board can fall back to
// base presentation. Returns the resolved entry (nil when no model / no
// match). Errors other than ErrCustomStatusKeyMismatch are best-effort —
// upstream callers MUST NOT roll back the base status change on resolver
// failure, since the upstream commit has already happened by the time
// this is invoked.
func (h *Handler) ResolveCustomStatusAfterBaseChange(
	ctx context.Context,
	issueID, projectID, workspaceID pgtype.UUID,
	newBase, actorIDStr, actorTypeStr, requestedKey string,
) error {
	if !baseStatuses[newBase] {
		return fmt.Errorf("unknown base_status %q", newBase)
	}
	assignment, err := h.Cerebro.GetProjectStatusModel(ctx, projectID)
	if err != nil {
		// No model on this project — an explicit key from the caller is a
		// client bug (picker should not have offered a custom key), but in
		// practice the desktop app may be older than the server: degrade to
		// auto-resolve (clear sidecar) rather than 400 the request.
		_ = h.Cerebro.DeleteCerebroIssueStatus(ctx, issueID)
		return nil
	}
	model, err := h.Cerebro.GetCerebroStatusModel(ctx, assignment.StatusModelID)
	if err != nil {
		_ = h.Cerebro.DeleteCerebroIssueStatus(ctx, issueID)
		return nil
	}
	entries := decodeStatuses(model.Statuses)
	var target *statusEntry
	if strings.TrimSpace(requestedKey) != "" {
		entry := findStatusEntry(entries, requestedKey)
		if entry == nil || entry.BaseStatus != newBase {
			return ErrCustomStatusKeyMismatch
		}
		target = entry
	} else {
		current, _ := h.Cerebro.GetCerebroIssueStatus(ctx, issueID)
		// If the current sidecar still matches the new base AND the pinned
		// key still exists in the model, keep it — a picker change inside
		// the same base column is a no-op for the sidecar.
		if current.BaseStatus == newBase {
			if e := findStatusEntry(entries, current.CustomStatusKey); e != nil {
				_ = e
				return nil
			}
		}
		// Otherwise pick the first entry whose base_status matches the new base.
		for i := range entries {
			if entries[i].BaseStatus == newBase {
				target = &entries[i]
				break
			}
		}
	}
	if target == nil {
		// No custom status maps to this base — clear the sidecar so the
		// board falls back to the base column for this issue.
		_ = h.Cerebro.DeleteCerebroIssueStatus(ctx, issueID)
		return nil
	}
	actorUUID, err := util.ParseUUID(actorIDStr)
	if err != nil {
		// Auto-resolve falls back to a "system" actor — the upstream change
		// already authenticated the user, this is just bookkeeping.
		actorUUID = pgtype.UUID{}
	}
	if actorTypeStr != "member" && actorTypeStr != "agent" {
		actorTypeStr = "system"
	}
	if _, err := h.Cerebro.UpsertCerebroIssueStatus(ctx, cerebrodb.UpsertCerebroIssueStatusParams{
		IssueID:         issueID,
		WorkspaceID:     workspaceID,
		StatusModelID:   model.ID,
		CustomStatusKey: target.Key,
		BaseStatus:      target.BaseStatus,
		SetByID:         actorUUID,
		SetByType:       actorTypeStr,
	}); err != nil {
		return err
	}
	return nil
}

// ValidateCustomStatusKey checks — without writing anything — that an explicit
// custom_status_key can be applied to an issue ending up on newBase in
// projectID's model. The upstream UpdateIssue handler calls this BEFORE it
// commits the base-status change, so a malformed picker payload is rejected
// with a 400 without half-applying the base status (FIR-1550 atomicity).
//
// Returns nil when there is nothing to reject: empty key, no model on the
// project (degrade — older clients), model unreadable, or a valid key. Returns
// ErrCustomStatusKeyMismatch ONLY when a non-empty key is missing from the
// model or bound to a different base than newBase — the single case the HTTP
// layer maps to 400.
func (h *Handler) ValidateCustomStatusKey(
	ctx context.Context,
	projectID pgtype.UUID,
	newBase, requestedKey string,
) error {
	if strings.TrimSpace(requestedKey) == "" {
		return nil
	}
	if !baseStatuses[newBase] {
		return ErrCustomStatusKeyMismatch
	}
	assignment, err := h.Cerebro.GetProjectStatusModel(ctx, projectID)
	if err != nil {
		return nil
	}
	model, err := h.Cerebro.GetCerebroStatusModel(ctx, assignment.StatusModelID)
	if err != nil {
		return nil
	}
	entry := findStatusEntry(decodeStatuses(model.Statuses), requestedKey)
	if entry == nil || entry.BaseStatus != newBase {
		return ErrCustomStatusKeyMismatch
	}
	return nil
}

// lookupStatusEntry resolves a statusEntry by (model_id, key). Returns nil
// silently on any failure — the response just degrades to "no label / no
// description" instead of erroring out the API.
func (h *Handler) lookupStatusEntry(ctx context.Context, modelID pgtype.UUID, key string) *statusEntry {
	model, err := h.Cerebro.GetCerebroStatusModel(ctx, modelID)
	if err != nil {
		return nil
	}
	return findStatusEntry(decodeStatuses(model.Statuses), key)
}

func findStatusEntry(entries []statusEntry, key string) *statusEntry {
	for i := range entries {
		if entries[i].Key == key {
			return &entries[i]
		}
	}
	return nil
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
