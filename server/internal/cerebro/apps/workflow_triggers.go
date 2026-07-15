package apps

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"
)

var (
	errWorkflowUnavailable = errors.New("workflow requires a published app with approved scopes")
	errTriggerMismatch     = errors.New("workflow trigger does not match this endpoint")
	errWorkflowDispatch    = errors.New("failed to dispatch workflow")
)

func (h *Handler) queueWorkflowRun(ctx context.Context, workflowID, workspaceID uuid.UUID, triggerType string, actorID *uuid.UUID, payload json.RawMessage, triggerKey string, enabledOnly bool) (uuid.UUID, bool, error) {
	var workflowVersion, configuredTrigger, appID, appVersion string
	var rawScopes json.RawMessage
	var ownerID uuid.UUID
	err := h.pool.QueryRow(ctx, `
		SELECT d.version,d.definition->'trigger'->>'type',a.id::text,a.current_version,g.scopes,
		       COALESCE($5::uuid,d.owner_id,a.owner_id)
		FROM cerebro_app_workflow_def d
		JOIN cerebro_app a ON a.id=d.app_id AND a.workspace_id=d.workspace_id
		JOIN cerebro_app_grant g ON g.app_id=a.id AND g.version=a.current_version AND g.status='approved'
		WHERE d.id=$1 AND ($2::uuid=$3::uuid OR d.workspace_id=$2) AND (NOT $4 OR d.enabled)`,
		workflowID, workspaceID, uuid.Nil, enabledOnly, actorID).Scan(&workflowVersion, &configuredTrigger, &appID, &appVersion, &rawScopes, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, errWorkflowUnavailable
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	if configuredTrigger != triggerType {
		return uuid.Nil, false, errTriggerMismatch
	}
	var scopes any
	if err := json.Unmarshal(rawScopes, &scopes); err != nil {
		return uuid.Nil, false, err
	}
	envelope, err := json.Marshal(map[string]any{
		"workflow_id": workflowID, "version": workflowVersion,
		"principal": map[string]any{"type": "member", "id": ownerID},
		"app":       map[string]any{"id": appID, "version": appVersion, "scopes": scopes},
	})
	if err != nil {
		return uuid.Nil, false, err
	}
	var runID uuid.UUID
	err = h.pool.QueryRow(ctx, `
		INSERT INTO cerebro_app_workflow_run (workflow_id,workflow_version,identity_envelope,trigger_payload,trigger_type,trigger_key)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (workflow_id,trigger_type,trigger_key) WHERE trigger_key <> '' DO NOTHING
		RETURNING id`, workflowID, workflowVersion, envelope, payload, triggerType, triggerKey).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = h.pool.QueryRow(ctx, `SELECT id FROM cerebro_app_workflow_run WHERE workflow_id=$1 AND trigger_type=$2 AND trigger_key=$3`, workflowID, triggerType, triggerKey).Scan(&runID)
		return runID, false, err
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	if h.dispatcher == nil {
		return runID, true, errWorkflowDispatch
	}
	if err := h.dispatcher.Dispatch(ctx, runID.String()); err != nil {
		_, _ = h.pool.Exec(ctx, `UPDATE cerebro_app_workflow_run SET status='failed',error='workflow dispatch failed',finished_at=now() WHERE id=$1`, runID)
		return runID, true, errWorkflowDispatch
	}
	return runID, true, nil
}

func (h *Handler) TriggerChat(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	workflowID, err := uuid.Parse(chi.URLParam(r, "workflowId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	payload, ok := decodeTriggerPayload(w, r)
	if !ok {
		return
	}
	runID, _, err := h.queueWorkflowRun(r.Context(), workflowID, workspaceID, "chat", &memberID, payload, uuid.NewString(), true)
	writeQueuedRun(w, workflowID, runID, err)
}

func (h *Handler) TriggerWebhook(w http.ResponseWriter, r *http.Request) {
	workflowID, err := uuid.Parse(chi.URLParam(r, "workflowId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var expected string
	err = h.pool.QueryRow(r.Context(), `SELECT definition->'trigger'->'config'->>'token' FROM cerebro_app_workflow_def WHERE id=$1 AND enabled AND definition->'trigger'->>'type'='webhook'`, workflowID).Scan(&expected)
	if err != nil || expected == "" || subtleString(chi.URLParam(r, "token"), expected) == false {
		http.NotFound(w, r)
		return
	}
	payload, ok := decodeTriggerPayload(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.Header.Get("X-Event-ID"))
	if key == "" {
		key = uuid.NewString()
	}
	runID, _, err := h.queueWorkflowRun(r.Context(), workflowID, uuid.Nil, "webhook", nil, payload, key, true)
	writeQueuedRun(w, workflowID, runID, err)
}

type dataEventRequest struct {
	ResourceID string          `json:"resource_id"`
	EventID    string          `json:"event_id"`
	Payload    json.RawMessage `json:"payload"`
}

func (h *Handler) TriggerDataEvent(w http.ResponseWriter, r *http.Request) {
	if h.dataEventKey == "" || !constantTimeBearer(r, h.dataEventKey) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req dataEventRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ResourceID) == "" || strings.TrimSpace(req.EventID) == "" {
		writeError(w, http.StatusBadRequest, "resource_id and event_id are required")
		return
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{}`)
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{}`)
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id FROM cerebro_app_workflow_def WHERE enabled AND definition->'trigger'->>'type'='data_event' AND definition->'trigger'->'config'->>'resource_id'=$1`, req.ResourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to find data-event workflows")
		return
	}
	defer rows.Close()
	queued := 0
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			_, created, queueErr := h.queueWorkflowRun(r.Context(), id, uuid.Nil, "data_event", nil, req.Payload, req.EventID, true)
			if queueErr == nil && created {
				queued++
			}
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": queued})
}

func (h *Handler) TriggerSchedule(w http.ResponseWriter, r *http.Request) {
	if h.workerIngestKey == "" || !constantTimeBearer(r, h.workerIngestKey) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	now := time.Now().UTC()
	rows, err := h.pool.Query(r.Context(), `SELECT id,definition->'trigger'->'config'->>'cron' FROM cerebro_app_workflow_def WHERE enabled AND definition->'trigger'->>'type'='schedule'`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to find scheduled workflows")
		return
	}
	defer rows.Close()
	queued := 0
	for rows.Next() {
		var id uuid.UUID
		var expression string
		if rows.Scan(&id, &expression) != nil {
			continue
		}
		schedule, parseErr := cron.ParseStandard(expression)
		if parseErr != nil {
			continue
		}
		minute := now.Truncate(time.Minute)
		if schedule.Next(minute.Add(-time.Nanosecond)).After(now) {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"scheduled_at": minute.Format(time.RFC3339)})
		_, created, queueErr := h.queueWorkflowRun(r.Context(), id, uuid.Nil, "schedule", nil, payload, minute.Format(time.RFC3339), true)
		if queueErr == nil && created {
			queued++
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": queued})
}

func decodeTriggerPayload(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	var raw json.RawMessage
	if !decodeJSON(w, r, &raw) {
		return nil, false
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	return raw, true
}

func writeQueuedRun(w http.ResponseWriter, workflowID, runID uuid.UUID, err error) {
	if err != nil {
		writeError(w, workflowQueueStatus(err), workflowQueueMessage(err))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": runID, "status": "queued", "workflow_id": workflowID})
}

func workflowQueueStatus(err error) int {
	if errors.Is(err, errWorkflowUnavailable) || errors.Is(err, errTriggerMismatch) {
		return http.StatusConflict
	}
	if errors.Is(err, errWorkflowDispatch) {
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

func workflowQueueMessage(err error) string {
	if errors.Is(err, errWorkflowUnavailable) {
		return errWorkflowUnavailable.Error()
	}
	if errors.Is(err, errTriggerMismatch) {
		return errTriggerMismatch.Error()
	}
	if errors.Is(err, errWorkflowDispatch) {
		return errWorkflowDispatch.Error()
	}
	return "failed to queue workflow run"
}

func subtleString(provided, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
