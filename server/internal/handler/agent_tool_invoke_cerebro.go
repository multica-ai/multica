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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ToolExecutorInvoker is the upstream-side seam for server-side tool execution
// from external runtimes. Concrete implementation lives in
// server/internal/cerebro/runtime to avoid import cycles.
//
// CEREBRO-PATCH(handler-tool-executor-iface): seam for universal tool execution.
type ToolExecutorInvoker interface {
	Invoke(ctx context.Context, agentID pgtype.UUID, workspaceID pgtype.UUID, toolName string, args map[string]any) (string, error)
}

// InvokeAgentTool handles POST /api/agents/{id}/tools/{name}/invoke.
// Executes a named tool server-side on behalf of the calling agent.
// The tool must be enabled in agent_tool_grant; the result is the raw
// string that would be fed back to the model.
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

	// Check the tool is granted and enabled for this agent.
	var enabled bool
	err := h.DB.QueryRow(r.Context(),
		`SELECT enabled FROM agent_tool_grant WHERE agent_id = $1 AND tool_name = $2`,
		agent.ID, toolName,
	).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !enabled) {
		writeError(w, http.StatusForbidden, "tool not granted or not enabled for this agent")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "grant check: "+err.Error())
		return
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

	result, err := h.ToolExecutor.Invoke(r.Context(), agent.ID, agent.WorkspaceID, toolName, body.Args)
	if err != nil {
		// 422 so the caller can relay the message back to the model rather
		// than treating it as an infrastructure failure.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": result})
}
