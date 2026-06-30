package handler

// daemon_effective_tools_cerebro.go (FIR-2312) resolves, at task-claim time,
// the set of NON-CLI tools an agent may actually use — MCP tools and connection
// tools — and ships them in the claim response as `effective_tools`. The daemon
// copies them into the agent brief's `## Available Commands` block via
// execenv.cerebroToolsBrief, so an agent finally sees the connections and MCP
// tools it has, not just the static `multica` CLI list.
//
// This is the single server-side chokepoint for the pattern: it reuses the same
// toolaccess resolver the admin "effective access" UI uses
// (h.runtimeToolAccess.ListEffectiveTools), which already folds the full
// tool-policy chain (Workspace › Runtime › Agent › Group › User). Any future
// tool family that flows through the runtime tool inventory + tool-policy chain
// is surfaced here automatically — no new wiring per tool. See
// docs/agents/agent-tool-brief.md.

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentTaskToolEntry is one non-CLI tool shipped to the daemon for the brief.
// Family groups it under a heading ("MCP tools" / "Connections" / "Platform
// tools"); Name is the exact tool name the agent calls; Description is the
// model-facing one-liner; Verdict is "allow" or "ask".
type AgentTaskToolEntry struct {
	Family      string `json:"family"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
}

// cerebroEffectiveToolsForBrief resolves the agent's exposed non-CLI tools for
// the brief. Returns nil (no section) when the tool-access service is not wired,
// the agent is unknown, or nothing is exposed — all expected, non-error cases.
func (h *Handler) cerebroEffectiveToolsForBrief(ctx context.Context, runtime db.AgentRuntime, agent *TaskAgentData, initiatorType, initiatorID string) []AgentTaskToolEntry {
	if h == nil || h.runtimeToolAccess == nil || agent == nil {
		return nil
	}
	agentID, err := util.ParseUUID(agent.ID)
	if err != nil {
		return nil
	}

	// User layer: prefer the member who triggered this task; fall back to the
	// agent's owner so the chain still resolves a user layer when an agent or
	// autopilot triggered the run. An invalid/zero UUID resolves the chain
	// without a user layer, which is fine.
	var userID pgtype.UUID
	if initiatorType == "member" && strings.TrimSpace(initiatorID) != "" {
		if u, err := util.ParseUUID(initiatorID); err == nil {
			userID = u
		}
	}
	if !userID.Valid && strings.TrimSpace(agent.OwnerID) != "" {
		if u, err := util.ParseUUID(agent.OwnerID); err == nil {
			userID = u
		}
	}

	rows, err := h.runtimeToolAccess.ListEffectiveTools(ctx, RuntimeToolAccessQuery{
		WorkspaceID:         runtime.WorkspaceID,
		RuntimeID:           runtime.ID,
		RuntimeMode:         runtime.RuntimeMode,
		RuntimeProvider:     runtime.Provider,
		RuntimeCapabilities: marshalRuntimeCapabilities(normalizedRuntimeCapabilities(runtime.Provider, runtime.Capabilities, runtime.ToolsConfig)),
		AgentID:             agentID,
		UserID:              userID,
	})
	if err != nil {
		// Best-effort: a resolution error must not fail the claim. The agent
		// just gets no dynamic tools section (the CLI list is unaffected).
		return nil
	}

	out := make([]AgentTaskToolEntry, 0, len(rows))
	for _, v := range rows {
		if !v.ExposureEffective.Effective {
			continue // not actually exposed to this agent → omit
		}
		name := strings.TrimSpace(v.Descriptor.DisplayName)
		if name == "" {
			name = strings.TrimSpace(v.Descriptor.ToolKey)
		}
		if name == "" {
			continue
		}
		family := "Platform tools"
		server := strings.TrimSpace(v.Inventory.MCPServerName)
		switch {
		case server != "":
			family = "Connections"
			name = server + " / " + name
		case v.Descriptor.Source == "mcp":
			family = "MCP tools"
		}
		out = append(out, AgentTaskToolEntry{
			Family:      family,
			Name:        name,
			Description: strings.TrimSpace(v.Descriptor.Description),
			Verdict:     strings.TrimSpace(v.Policy.Effective),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
