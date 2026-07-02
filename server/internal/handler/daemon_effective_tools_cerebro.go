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

// CerebroAPIConnectionBriefResolver (FIR-2388) resolves the api-type connection
// endpoint tools an agent is granted, for the claim-time brief. It is the SAME
// resolver the cloud gateway and the local MCP handler use, so a tool listed in
// the brief is a tool the agent can actually call. It is an injected interface —
// not a direct call — because the concrete resolver lives in the cerebro/runtime
// package, which imports this handler package; the handler importing runtime back
// would be a cycle. The router wires *cerebro/runtime.APIConnectionResolver here.
type CerebroAPIConnectionBriefResolver interface {
	APIConnectionToolsForBrief(ctx context.Context, ident CerebroAPIConnectionBriefIdentity) []CerebroAPIConnectionBriefTool
}

// CerebroAPIConnectionBriefIdentity is the actor the endpoints are gated for. It
// mirrors runtime.APIConnectionIdentity but is declared here so the interface has
// no runtime-package types in its signature (keeping the cycle broken).
//
// InitiatorID is the delegated task initiator (on_behalf_of, FIR-2441): a
// tighten-only layer the endpoint gate honours Deny-only, so the brief lists a
// member-denied tool as hidden exactly as the call path refuses it. Zero when
// there is no delegation.
type CerebroAPIConnectionBriefIdentity struct {
	WorkspaceID pgtype.UUID
	RuntimeID   pgtype.UUID
	AgentID     pgtype.UUID
	OwnerID     pgtype.UUID
	InitiatorID pgtype.UUID
}

// CerebroAPIConnectionBriefTool is one resolved endpoint tool for the brief.
// Name is the exact tool name the agent calls verbatim; Verdict is "allow" or
// "ask".
type CerebroAPIConnectionBriefTool struct {
	Name        string
	Description string
	Verdict     string
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

	// Actor layers (FIR-2441). The agent owner and the delegated member (the task
	// initiator, on_behalf_of) are two DISTINCT policy layers — matching the claim-
	// time connection resolver (connection_tool_resolver.userCeiling), so the brief
	// lists exactly what the run can call (list == callable):
	//   - User layer  = the fail-closed intersection of owner and initiator: no
	//     initiator → the owner; owner == initiator → that user; owner != initiator
	//     → the zero UUID, so a personal grant on either party never leaks to the
	//     other.
	//   - on_behalf_of = the initiator member, resolved tighten-only, so a member
	//     denied a tool across every agent they drive is reflected here too.
	var ownerID, initiatorMember pgtype.UUID
	if strings.TrimSpace(agent.OwnerID) != "" {
		if u, err := util.ParseUUID(agent.OwnerID); err == nil {
			ownerID = u
		}
	}
	if initiatorType == "member" && strings.TrimSpace(initiatorID) != "" {
		if u, err := util.ParseUUID(initiatorID); err == nil {
			initiatorMember = u
		}
	}
	userID := ownerID
	if initiatorMember.Valid {
		if !(ownerID.Valid && ownerID.Bytes == initiatorMember.Bytes) {
			// Owner != initiator (or owner unset): the intersection is empty, so the
			// user layer is fail-closed (no user-scoped grant applies to either).
			userID = pgtype.UUID{}
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
		OnBehalfOfID:        initiatorMember,
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

	// FIR-2388: append the api-type connection endpoint tools this agent is
	// granted. The tool-policy chain above (ListEffectiveTools) covers MCP and
	// platform tools but NOT api-connection endpoints — those are a separate,
	// server-side-dispatched family gated by ConnectionEndpointEffective. Resolve
	// them through the SAME shared resolver the cloud gateway and local MCP handler
	// use, so "listed in the brief" == "callable" for every runtime. The endpoint
	// gate keys on the agent's OWNER as the user layer (matching the cloud call-
	// time guard, apiEndpointSetting) PLUS the delegated initiator as the tighten-
	// only on_behalf_of layer (FIR-2441), so the brief cannot show a tool the call
	// path would then refuse — including a member-level Deny on the initiator.
	if h.APIConnectionBrief != nil {
		seen := make(map[string]struct{}, len(out))
		for _, e := range out {
			seen[e.Name] = struct{}{}
		}
		apiTools := h.APIConnectionBrief.APIConnectionToolsForBrief(ctx, CerebroAPIConnectionBriefIdentity{
			WorkspaceID: runtime.WorkspaceID,
			RuntimeID:   runtime.ID,
			AgentID:     agentID,
			OwnerID:     ownerID,
			InitiatorID: initiatorMember,
		})
		for _, t := range apiTools {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			verdict := strings.TrimSpace(t.Verdict)
			if verdict == "" {
				verdict = "allow"
			}
			out = append(out, AgentTaskToolEntry{
				Family:      "Connections",
				Name:        name,
				Description: strings.TrimSpace(t.Description),
				Verdict:     verdict,
			})
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
