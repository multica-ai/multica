package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const maxToolConfirmationDenyMessage = 2000

// TaskToolConfirmationResponse is one tool call an external runtime paused on
// until a person allows or denies it.
type TaskToolConfirmationResponse struct {
	CallID        string          `json:"call_id"`
	ToolName      string          `json:"tool_name"`
	McpServerName string          `json:"mcp_server_name,omitempty"`
	Input         json.RawMessage `json:"input,omitempty"`
	Status        string          `json:"status"`
	DenyMessage   string          `json:"deny_message,omitempty"`
	CreatedAt     string          `json:"created_at"`
}

func toolConfirmationToResponse(c db.TaskToolConfirmation) TaskToolConfirmationResponse {
	resp := TaskToolConfirmationResponse{
		CallID:        c.CallID,
		ToolName:      c.ToolName,
		McpServerName: c.McpServerName.String,
		Status:        c.Status,
		DenyMessage:   c.DenyMessage.String,
		CreatedAt:     c.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
	}
	if len(c.Input) > 0 {
		resp.Input = json.RawMessage(c.Input)
	}
	return resp
}

func (h *Handler) hydratePendingToolConfirmations(ctx context.Context, tasks []db.AgentTaskQueue, resp []AgentTaskResponse) {
	if len(tasks) == 0 || len(tasks) != len(resp) {
		return
	}
	ids := make([]pgtype.UUID, 0, len(tasks))
	for _, t := range tasks {
		if t.Status == "running" {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := h.Queries.ListPendingTaskToolConfirmations(ctx, ids)
	if err != nil {
		return // optional decoration; the run row still renders
	}
	byTask := make(map[string][]TaskToolConfirmationResponse, len(rows))
	for _, row := range rows {
		key := uuidToString(row.TaskID)
		byTask[key] = append(byTask[key], toolConfirmationToResponse(row))
	}
	for i := range tasks {
		if pending := byTask[uuidToString(tasks[i].ID)]; len(pending) > 0 {
			resp[i].PendingToolConfirmations = pending
		}
	}
}

func (h *Handler) publishTaskToolConfirmation(workspaceID string, task db.AgentTaskQueue) {
	h.Bus.Publish(events.Event{
		Type:        protocol.EventTaskToolConfirmation,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		TaskID:      uuidToString(task.ID),
		Payload: protocol.TaskToolConfirmationPayload{
			TaskID:  uuidToString(task.ID),
			IssueID: uuidToString(task.IssueID),
		},
	})
}

type registerToolConfirmationsRequest struct {
	Confirmations []struct {
		CallID        string          `json:"call_id"`
		ToolName      string          `json:"tool_name"`
		McpServerName string          `json:"mcp_server_name"`
		Input         json.RawMessage `json:"input"`
	} `json:"confirmations"`
}

// RegisterTaskToolConfirmations records tool calls a running task is blocked
// on. Registration is idempotent per call id so the runtime can retry.
func (h *Handler) RegisterTaskToolConfirmations(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var req registerToolConfirmationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Confirmations) == 0 {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !task.IssueID.Valid {
		writeErrorCode(w, http.StatusUnprocessableEntity, "tool_confirmation_unsupported", "tool approvals are only supported for issue runs")
		return
	}
	if task.Status != "running" {
		writeErrorCode(w, http.StatusConflict, "task_not_running", "task is not running")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	for _, c := range req.Confirmations {
		if strings.TrimSpace(c.CallID) == "" || strings.TrimSpace(c.ToolName) == "" {
			writeError(w, http.StatusBadRequest, "call_id and tool_name are required")
			return
		}
	}
	before, err := h.Queries.ListTaskToolConfirmations(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tool confirmations")
		return
	}
	for _, c := range req.Confirmations {
		var input []byte
		if len(c.Input) > 0 && json.Valid(c.Input) {
			input = c.Input
		}
		if err := h.Queries.InsertTaskToolConfirmation(r.Context(), db.InsertTaskToolConfirmationParams{
			TaskID:        task.ID,
			WorkspaceID:   wsUUID,
			IssueID:       task.IssueID,
			CallID:        c.CallID,
			ToolName:      c.ToolName,
			McpServerName: pgtype.Text{String: c.McpServerName, Valid: c.McpServerName != ""},
			Input:         input,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record tool confirmation")
			return
		}
	}
	h.writeTaskToolConfirmations(w, r, workspaceID, task, len(before))
}

// ListTaskToolConfirmationsForDaemon returns every recorded call and decision
// so the runtime can resume once all pending calls are decided.
func (h *Handler) ListTaskToolConfirmationsForDaemon(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	h.writeTaskToolConfirmations(w, r, workspaceID, task, -1)
}

// writeTaskToolConfirmations publishes an update when the row count grew past
// previousCount (pass -1 to never publish).
func (h *Handler) writeTaskToolConfirmations(w http.ResponseWriter, r *http.Request, workspaceID string, task db.AgentTaskQueue, previousCount int) {
	rows, err := h.Queries.ListTaskToolConfirmations(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tool confirmations")
		return
	}
	if previousCount >= 0 && len(rows) > previousCount {
		h.publishTaskToolConfirmation(workspaceID, task)
	}
	out := make([]TaskToolConfirmationResponse, len(rows))
	for i, row := range rows {
		out[i] = toolConfirmationToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"confirmations": out})
}

type decideToolConfirmationRequest struct {
	Decision string `json:"decision"`
	Message  string `json:"message"`
}

// DecideTaskToolConfirmation lets a member who may run the agent allow or deny
// one pending tool call.
func (h *Handler) DecideTaskToolConfirmation(w http.ResponseWriter, r *http.Request) {
	issue, task, userID, ok := h.loadTaskSupplementTarget(w, r)
	if !ok {
		return
	}
	var req decideToolConfirmationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var status string
	switch req.Decision {
	case "allow":
		status = "allowed"
	case "deny":
		status = "denied"
	default:
		writeError(w, http.StatusBadRequest, "decision must be allow or deny")
		return
	}
	message := strings.TrimSpace(req.Message)
	if status != "denied" {
		message = ""
	}
	if len(message) > maxToolConfirmationDenyMessage {
		writeError(w, http.StatusBadRequest, "message is too long")
		return
	}
	row, err := h.Queries.DecideTaskToolConfirmation(r.Context(), db.DecideTaskToolConfirmationParams{
		Status:      status,
		DenyMessage: pgtype.Text{String: message, Valid: message != ""},
		DecidedBy:   userID,
		TaskID:      task.ID,
		CallID:      chi.URLParam(r, "callId"),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, "tool_confirmation_not_pending", "this tool call is no longer waiting for approval")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record decision")
		return
	}
	h.publishTaskToolConfirmation(uuidToString(issue.WorkspaceID), task)
	writeJSON(w, http.StatusOK, toolConfirmationToResponse(row))
}
