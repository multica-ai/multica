package handler

// CEREBRO-PATCH(runtime-tools-config-handler): runtime-level MCP defaults admin API (9031).
//
// PATCH /api/runtimes/{runtimeId}/tools-config sets the runtime's tools_config
// JSON document. The daemon merges this with each task's agent.mcp_config at
// claim time, so a workspace admin can declare MCP servers once on a runtime
// instead of per-agent. Pass `tools_config: null` to clear.
//
// Auth: workspace owner/admin. Same threat model as persona_sandbox — a
// runtime owner shouldn't be able to silently install MCP servers (which
// spawn subprocesses) without admin approval.

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// UpdateRuntimeToolsConfigRequest carries the new tools_config JSON.
//
// `tools_config: null` (or omitted) is rejected — use the explicit body
// `{"tools_config": null}` to clear, matching the persona_sandbox endpoint's
// nil-vs-empty distinction.
type UpdateRuntimeToolsConfigRequest struct {
	ToolsConfig *json.RawMessage `json:"tools_config"`
}

// minimal shape sanity check: must be a JSON object, mcpServers (if present)
// must be an object whose values are objects with at least a "command" string.
// This catches obvious mistakes like passing a stringified JSON or an array
// without doing a full MCP schema validation here — the daemon validates the
// merged config when spawning Claude and surfaces errors there.
//
// The shape matches Claude Code's --mcp-config file format so the stored
// document can be merged with agent.mcp_config and passed through verbatim.
type toolsConfigShape struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

func validateToolsConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	// Empty object {} is allowed (equivalent to clearing).
	var probe toolsConfigShape
	if err := json.Unmarshal(raw, &probe); err != nil {
		return err
	}
	for name, server := range probe.MCPServers {
		var s struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(server, &s); err != nil {
			return &validationError{Msg: "mcpServers." + name + ": not a JSON object"}
		}
		if s.Command == "" {
			return &validationError{Msg: "mcpServers." + name + ": missing required \"command\" field"}
		}
	}
	return nil
}

type validationError struct{ Msg string }

func (e *validationError) Error() string { return e.Msg }

// UpdateAgentRuntimeToolsConfig sets or clears the runtime-level tools_config.
func (h *Handler) UpdateAgentRuntimeToolsConfig(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can change tools_config")
		return
	}

	var req UpdateRuntimeToolsConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ToolsConfig == nil {
		writeError(w, http.StatusBadRequest, "tools_config is required (use {\"tools_config\": null} to clear)")
		return
	}

	var value []byte
	// JSON `null` in the body — clear the column.
	if len(*req.ToolsConfig) == 0 || string(*req.ToolsConfig) == "null" {
		value = nil
	} else {
		if err := validateToolsConfig(*req.ToolsConfig); err != nil {
			writeError(w, http.StatusBadRequest, "invalid tools_config: "+err.Error())
			return
		}
		value = []byte(*req.ToolsConfig)
	}

	updated, err := h.Queries.UpdateAgentRuntimeToolsConfig(r.Context(), db.UpdateAgentRuntimeToolsConfigParams{
		ID:          rt.ID,
		ToolsConfig: value,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update tools_config")
		return
	}

	slog.Info("runtime tools_config updated",
		"runtime_id", runtimeID,
		"updated_by", uuidToString(member.UserID),
		"cleared", value == nil,
	)

	auditDetails, _ := json.Marshal(map[string]any{
		"runtime_id":   runtimeID,
		"runtime_name": rt.Name,
		"cleared":      value == nil,
	})
	_, _ = h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: rt.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      "runtime_tools_config_changed",
		Details:     auditDetails,
	})

	h.publish(protocol.EventDaemonRegister, wsID, "member", uuidToString(member.UserID), map[string]any{
		"action":     "update",
		"runtime_id": runtimeID,
	})

	writeJSON(w, http.StatusOK, runtimeToResponse(updated))
}
