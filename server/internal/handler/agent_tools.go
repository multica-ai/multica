package handler

// CEREBRO-PATCH(agent-tools-handler): cerebro agent tool grant admin API.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype" // CEREBRO-PATCH(agent-tool-grant-retirement): FIR-3403 legacy grant DB imports removed.
	"github.com/multica-ai/multica/server/internal/util"
)

// CerebroToolItem holds display metadata for one registered tool.
// Injected into the Handler via SetCerebroToolMeta.
type CerebroToolItem struct {
	Name        string
	Description string
	Status      string         // CEREBRO-PATCH(agent-tools-status): carries implemented/excluded registry state into admin API.
	InputSchema map[string]any // CEREBRO-PATCH(handler-tool-schema): TECH-3226 JSON Schema for the tool; nil for tools not in the server registry.
}

// AgentToolResponse is the wire shape returned by GET /api/agents/{id}/tools.
// Includes display metadata (name, description) plus policy-derived availability. // CEREBRO-PATCH(agent-tool-grant-retirement): no legacy config/grant fields.
type AgentToolResponse struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Status      string          `json:"status"` // CEREBRO-PATCH(agent-tools-status-response): let UI show explicit exclusions.
	Enabled     bool            `json:"enabled"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"` // CEREBRO-PATCH(handler-tool-schema): TECH-3226 JSON Schema; omitted for tools not in the server registry.
}

// SetCerebroToolMeta wires tool display metadata into the handler. Called by
// the router after construction so the upstream handler.New signature stays
// unchanged.
//
// CEREBRO-PATCH(handler-tool-meta-setter): wires tool metadata without
// changing the upstream handler.New signature.
func (h *Handler) SetCerebroToolMeta(items []CerebroToolItem) {
	// CEREBRO-PATCH(agent-tool-grant-retirement): metadata is independent of the removed legacy grant store.
	h.cerebroToolItems = items
	m := make(map[string]string, len(items))
	statuses := make(map[string]string, len(items))
	for _, it := range items {
		m[it.Name] = it.Description
		statuses[it.Name] = it.Status
	}
	h.cerebroToolDesc = m
	h.cerebroToolStatus = statuses
}

// ListAgentTools handles GET /api/agents/{id}/tools
// Returns all registered tools, annotated with policy-derived enabled status.
func (h *Handler) ListAgentTools(w http.ResponseWriter, r *http.Request) {
	// CEREBRO-PATCH(agent-tool-grant-retirement): policy replaces direct agent_tool_grant reads and writes.
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	// CEREBRO-PATCH(agent-tools-policy-read): FIR-3403 — the external runtime
	// listing reads the same capability register + tool-policy decision as the
	// runtime page and call-time gate.
	policyEnabled := map[string]bool{}
	if h.runtimeToolAccess != nil && agent.RuntimeID.Valid {
		if rt, err := h.Queries.GetAgentRuntime(r.Context(), agent.RuntimeID); err == nil {
			userID := agent.OwnerID
			if headerID, ok := parseOptionalUUID(r.Header.Get("X-User-ID")); ok {
				userID = headerID
			}
			if rows, err := h.runtimeToolAccess.ListEffectiveTools(r.Context(), RuntimeToolAccessQuery{
				WorkspaceID: agent.WorkspaceID, RuntimeID: rt.ID, RuntimeMode: rt.RuntimeMode,
				RuntimeProvider: rt.Provider, RuntimeCapabilities: marshalRuntimeCapabilities(normalizedRuntimeCapabilities(rt.Provider, rt.Capabilities, rt.ToolsConfig)),
				AgentID: agent.ID, UserID: userID,
			}); err == nil {
				for _, row := range rows {
					policyEnabled[row.Descriptor.ToolKey] = row.Policy.Effective != "deny"
				}
			}
		}
	}

	// CEREBRO-PATCH(memory-tools-offer): FIR-1794 — the memory tools are driven
	// by the three memory gates (workspace flag, create_memory capability,
	// per-user×agent switches), not by grants. Resolve the caller's real gate
	// state once so the listing matches what the runtime will actually offer.
	memRecallEnabled, memWriteEnabled := h.cerebroMemoryToolStates(r, agent.ID, agent.WorkspaceID)

	// CEREBRO-PATCH(agent-tool-grant-retirement): build the response from canonical policy, without legacy grant config.
	out := make([]AgentToolResponse, 0, len(h.cerebroToolItems))
	for _, item := range h.cerebroToolItems {
		resp := AgentToolResponse{
			Name:        item.Name,
			Description: item.Description,
			Status:      item.Status,
		}
		if item.InputSchema != nil { // CEREBRO-PATCH(handler-tool-schema): include JSON Schema so external runtimes can present tools to their model.
			if raw, err := json.Marshal(item.InputSchema); err == nil {
				resp.InputSchema = json.RawMessage(raw)
			}
		}
		if policyEnabled[item.Name] { // CEREBRO-PATCH(agent-tool-grant-retirement): canonical policy replaces legacy grant/config reads.
			resp.Enabled = true
		}
		if item.Status == "explicitly_excluded" {
			resp.Enabled = false
		}
		// CEREBRO-PATCH(memory-tools-offer): FIR-1794 — memory tools carry the
		// excluded status (never manually grantable) but ARE enabled when the
		// memory gates pass, so this override runs after the excluded reset.
		switch item.Name {
		case "memory_recall":
			resp.Enabled = memRecallEnabled
		case "memory_remember", "memory_delete":
			resp.Enabled = memWriteEnabled
		}
		out = append(out, resp)
	}

	// CEREBRO-PATCH(workflow-open-step-tool): FIR-3493 — open_loop_step is a
	// task-scoped capability, not an administrator grant. Offer it to external
	// runtimes only when the authenticated task's trusted workflow context says
	// the current block allows more steps. InvokeAgentTool re-validates the same
	// task and the runtime layer pins the exact IDs and limits before execution.
	if r.Header.Get("X-Actor-Source") == "task_token" {
		if taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID")); err == nil {
			if task, err := h.Queries.GetAgentTask(r.Context(), taskID); err == nil && task.AgentID == agent.ID && taskAllowsOpenLoopStep(task.Context) {
				schema, _ := json.Marshal(map[string]any{
					"type": "object", "additionalProperties": false, "properties": map[string]any{},
				})
				out = append(out, AgentToolResponse{
					Name:        "open_loop_step",
					Description: "Open the next step of the workflow block currently running. The server enforces the block and phase limits.",
					Status:      "implemented",
					Enabled:     true,
					InputSchema: schema,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, out)
}

func taskAllowsOpenLoopStep(raw json.RawMessage) bool {
	var context struct {
		LoopStep struct {
			Steps struct {
				Allowed bool `json:"allowed"`
				Max     int  `json:"max"`
			} `json:"steps"`
		} `json:"loop_step"`
	}
	return json.Unmarshal(raw, &context) == nil && context.LoopStep.Steps.Allowed && context.LoopStep.Steps.Max > 0
}

func parseOptionalUUID(raw string) (pgtype.UUID, bool) { // CEREBRO-PATCH(agent-tool-grant-retirement): legacy UpsertAgentTool ended above this fork seam.
	if raw == "" {
		return pgtype.UUID{}, false
	}
	u, err := util.ParseUUID(raw)
	return u, err == nil
}
