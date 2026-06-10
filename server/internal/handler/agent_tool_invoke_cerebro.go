package handler

// CEREBRO-PATCH(handler-tool-invoke): TECH-3226 — server-side tool execution
// endpoint for external runtimes (firtal-local, future runtimes).
// POST /api/agents/{id}/tools/{name}/invoke runs any granted tool server-side
// so all runtimes have identical capabilities including connections
// (web_fetch, firtal_registry, Google Sheets, etc.).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// ErrToolNotPermitted is returned by ToolExecutorInvoker.Invoke when the
// cascade permission check denies the tool for the given agent+user combination.
// CEREBRO-PATCH(invoke-err-sentinel): TECH-3226 — typed 403 vs 422 distinction.
var ErrToolNotPermitted = errors.New("tool not permitted")

// ToolExecutorInvoker is the upstream-side seam for server-side tool execution
// from external runtimes. Concrete implementation lives in
// server/internal/cerebro/runtime to avoid import cycles.
//
// CEREBRO-PATCH(handler-tool-executor-iface): seam for universal tool execution.
type ToolExecutorInvoker interface {
	// userID: authorship (member when set, agent when zero).
	// cascadeUserID: passed to GetCascadeEnabledToolsForAgent for permission
	// resolution. For task tokens: task.OriginalUserID. For user tokens: caller.
	Invoke(ctx context.Context, agentID, workspaceID, userID, cascadeUserID pgtype.UUID, toolName string, args map[string]any) (string, error)
}

// InvokeAgentTool handles POST /api/agents/{id}/tools/{name}/invoke.
// Permissions match the gateway: cascade via GetCascadeEnabledToolsForAgent.
// Task tokens use task.OriginalUserID for cascade (agent authorship).
// User tokens use the calling user for both cascade and authorship (member).
func (h *Handler) InvokeAgentTool(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "name")
	if toolName == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}
	if h.ToolExecutor == nil {
		writeError(w, http.StatusNotImplemented, "tool executor not configured")
		return
	}

	// CEREBRO-PATCH(invoke-cascade-perm): TECH-3226 — resolve caller identities.
	// Task tokens: agent authorship + task's OriginalUserID drives cascade.
	// User tokens: member authorship + calling user drives cascade.
	var callerUserID, cascadeUserID pgtype.UUID
	if r.Header.Get("X-Actor-Source") == "task_token" {
		if taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID")); err == nil {
			if task, err := h.Queries.GetAgentTask(r.Context(), taskID); err == nil {
				cascadeUserID = task.OriginalUserID
			}
		}
	} else {
		callerUserID, _ = util.ParseUUID(r.Header.Get("X-User-ID"))
		cascadeUserID = callerUserID
	}

	var body struct {
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if body.Args == nil {
		body.Args = map[string]any{}
	}

	result, err := h.ToolExecutor.Invoke(r.Context(), agent.ID, agent.WorkspaceID, callerUserID, cascadeUserID, toolName, body.Args)
	if err != nil {
		if errors.Is(err, ErrToolNotPermitted) {
			writeError(w, http.StatusForbidden, "tool not granted or not enabled for this agent")
			return
		}
		// 422 so the caller can relay the message back to the model rather
		// than treating it as an infrastructure failure.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": result})
}
