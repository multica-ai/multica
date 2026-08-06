package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
)

type RuntimeHookEventEvaluator interface {
	Evaluate(context.Context, cerebroworkflows.HookEvent) (cerebroworkflows.HookResult, error)
}

type runtimeHookEventRequest struct {
	EventType      cerebroworkflows.HookEventType `json:"event_type"`
	SessionID      string                         `json:"session_id,omitempty"`
	Model          string                         `json:"model,omitempty"`
	ProposedAction map[string]any                 `json:"proposed_action,omitempty"`
	Context        map[string]any                 `json:"context,omitempty"`
	MutableFields  []string                       `json:"mutable_fields,omitempty"`
}

var runtimeHookEventTypes = map[cerebroworkflows.HookEventType]bool{
	cerebroworkflows.HookBeforeSessionStart:   true,
	cerebroworkflows.HookAfterSessionStart:    true,
	cerebroworkflows.HookBeforeSessionEnd:     true,
	cerebroworkflows.HookAfterSessionEnd:      true,
	cerebroworkflows.HookBeforePromptAssemble: true,
	cerebroworkflows.HookBeforeToolCall:       true,
	cerebroworkflows.HookAfterToolCall:        true,
	cerebroworkflows.HookOnToolFailure:        true,
	cerebroworkflows.HookBeforeAgentStop:      true,
	cerebroworkflows.HookBeforeSubagentStart:  true,
	cerebroworkflows.HookAfterSubagentStop:    true,
	cerebroworkflows.HookOnError:              true,
}

// EmitRuntimeHookEvent is the authenticated daemon-to-server channel for hook
// events whose real lifecycle boundary exists inside the local agent runtime.
func (h *Handler) EmitRuntimeHookEvent(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}
	if h.RuntimeHookEvents == nil {
		writeJSON(w, http.StatusOK, cerebroworkflows.HookResult{Decision: cerebroworkflows.HookAllow})
		return
	}

	var req runtimeHookEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !runtimeHookEventTypes[req.EventType] {
		writeError(w, http.StatusBadRequest, "event_type is not emitted by the runtime")
		return
	}

	event := cerebroworkflows.HookEvent{
		EventID:       uuid.NewString(),
		Type:          req.EventType,
		WorkspaceID:   workspaceID,
		AgentID:       uuidToString(task.AgentID),
		IssueID:       uuidToString(task.IssueID),
		SessionID:     req.SessionID,
		Model:         req.Model,
		Actor:         cerebroworkflows.HookActor{Type: "agent", ID: uuidToString(task.AgentID)},
		Proposed:      req.ProposedAction,
		Context:       req.Context,
		MutableFields: req.MutableFields,
	}
	result, err := h.RuntimeHookEvents.Evaluate(r.Context(), event)
	if err != nil {
		slog.Warn("runtime workflow hook evaluation failed", "task_id", taskID, "event_type", req.EventType, "error", err)
		writeJSON(w, http.StatusOK, cerebroworkflows.HookResult{Decision: cerebroworkflows.HookAllow, Warning: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
