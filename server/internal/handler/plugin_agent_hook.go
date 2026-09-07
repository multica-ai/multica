package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// The daemon's end of the agent trigger.
//
// An agent calls a tool; the daemon's local MCP server turns that into this
// request; the server performs the signed hook call. The daemon never holds the
// signing secret and never talks to the plugin's endpoint, which is what keeps
// the rate limit, the circuit breaker, the `net:` destination check and the
// invocation record on one code path instead of two.
//
// Authenticated as the daemon and scoped to a claimed task, exactly like the
// rest of /api/daemon: the workspace comes from the task row, so a daemon
// cannot reach an installation in a workspace it is not running a task for.

type invokeAgentHookRequest struct {
	InstallationID string          `json:"installation_id"`
	HookKey        string          `json:"hook_key"`
	Input          json.RawMessage `json:"input,omitempty"`
}

// InvokeAgentPluginHook — POST /api/daemon/tasks/{id}/plugin-hooks
func (h *Handler) InvokeAgentPluginHook(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req invokeAgentHookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.InstallationID == "" || req.HookKey == "" {
		writeError(w, http.StatusBadRequest, "installation_id and hook_key are required")
		return
	}

	// The installation must belong to the task's workspace. Without this a
	// daemon holding one valid task could name any installation id in the
	// deployment and have the server call it.
	installation, err := h.PluginService.InstallationForWorkspace(r.Context(), parseUUID(workspaceID), req.InstallationID)
	if err != nil {
		writePluginError(w, err, "failed to load the Plugin")
		return
	}

	result, err := h.PluginService.InvokeAgentHook(
		r.Context(), uuidToString(installation.ID), req.HookKey, task.AgentID, req.Input)
	if err != nil {
		// 200 with an error body, deliberately.
		//
		// The daemon turns this into a tool ERROR, and a tool error is
		// something the agent reads and works around. A transport-level failure
		// would instead look to the daemon like the broker itself is broken,
		// which is how one unreachable plugin endpoint ends up failing the
		// whole task — the outcome acceptance criterion 3 forbids.
		writeJSON(w, http.StatusOK, map[string]any{
			"status": result.Status,
			"error":  result.Error,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
