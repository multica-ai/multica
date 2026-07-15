package apps

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type createWorkflowRequest struct {
	AppID      string          `json:"app_id"`
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Definition json.RawMessage `json:"definition"`
}

type testWorkflowRequest struct {
	TriggerPayload json.RawMessage `json:"trigger_payload"`
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	var req createWorkflowRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Version == "" {
		req.Version = "1.0.0"
	}
	if !semverPattern.MatchString(req.Version) {
		writeError(w, http.StatusBadRequest, "version must be semantic versioning")
		return
	}
	if err := validateWorkflowDefinition(req.Definition); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	appID, err := uuid.Parse(req.AppID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "app_id must be a UUID")
		return
	}
	var id uuid.UUID
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO cerebro_app_workflow_def (workspace_id,app_id,name,definition,version,owner_id)
		SELECT $1,$2,$3,$4,$5,$6 WHERE EXISTS (SELECT 1 FROM cerebro_app WHERE id=$2 AND workspace_id=$1)
		RETURNING id`, workspaceID, appID, strings.TrimSpace(req.Name), req.Definition, req.Version, memberID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create app workflow")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": strings.TrimSpace(req.Name), "version": req.Version, "enabled": false, "definition": req.Definition})
}

func (h *Handler) SetWorkflowEnabled(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "workflowId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	enabled := chi.URLParam(r, "state") == "enable"
	result, err := h.pool.Exec(r.Context(), `UPDATE cerebro_app_workflow_def SET enabled=$3,updated_at=now() WHERE id=$1 AND workspace_id=$2`, id, workspaceID, enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update app workflow")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "app workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
}

// TestWorkflow is the authenticated manual trigger. It records a durable
// identity envelope and queues the same real Hatchet path used by automation.
func (h *Handler) TestWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "workflowId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	var req testWorkflowRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.TriggerPayload) == 0 {
		req.TriggerPayload = json.RawMessage(`{}`)
	}
	if !json.Valid(req.TriggerPayload) {
		writeError(w, http.StatusBadRequest, "trigger_payload must be valid JSON")
		return
	}
	runID, _, err := h.queueWorkflowRun(r.Context(), id, workspaceID, "manual", &memberID, req.TriggerPayload, uuid.NewString(), false)
	if err != nil {
		writeError(w, workflowQueueStatus(err), workflowQueueMessage(err))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": runID, "status": "queued", "workflow_id": id})
}

func (h *Handler) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "workflowId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT r.id,r.status,r.workflow_version,r.step_log,r.error,r.started_at,r.finished_at,r.created_at FROM cerebro_app_workflow_run r JOIN cerebro_app_workflow_def d ON d.id=r.workflow_id WHERE r.workflow_id=$1 AND d.workspace_id=$2 ORDER BY r.created_at DESC LIMIT 100`, id, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list app workflow runs")
		return
	}
	defer rows.Close()
	runs := make([]map[string]any, 0)
	for rows.Next() {
		var runID uuid.UUID
		var status, version, runError string
		var stepLog json.RawMessage
		var startedAt, finishedAt *time.Time
		var createdAt time.Time
		if err := rows.Scan(&runID, &status, &version, &stepLog, &runError, &startedAt, &finishedAt, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read app workflow runs")
			return
		}
		runs = append(runs, map[string]any{"id": runID, "status": status, "version": version, "step_log": stepLog, "error": runError, "started_at": startedAt, "finished_at": finishedAt, "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}
