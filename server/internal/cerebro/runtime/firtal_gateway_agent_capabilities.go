package runtime

// get_agent_capabilities on the Firtal Gateway (FIR-3398, Trin 2).
//
// The self-lookup was registered only in clitools.RegisterTools, which reaches
// local runtimes and Claude Code over `multica mcp serve`. The Gateway's own
// registry never had it, yet the tool matrix marks it callable — so
// Earlier seed logic allowed `get_agent_capabilities` in a retired direct store while
// GetEnabledToolsForAgent silently dropped it for want of an implementation.
// The grant said yes, the runtime had nothing to call, and nothing detected the
// gap. That is the declared-vs-real drift the availability evidence model names,
// and an agent asking "what am I allowed to do?" on the Gateway got no answer at
// all.
//
// The card itself is NOT rebuilt here. It is served by the same
// handler.BuildAgentCapabilitiesCard the HTTP route uses, injected as a narrow
// provider, so the two surfaces cannot drift.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
)

// AgentCapabilitiesProvider builds an agent's canonical capabilities card.
// Satisfied by *handler.Handler. Declared here as a narrow interface so the
// executor stays testable with a fake and the dependency stays one-way.
type AgentCapabilitiesProvider interface {
	BuildAgentCapabilitiesCard(ctx context.Context, agentID pgtype.UUID) (handler.AgentCapabilities, error)
}

// FirtalGetAgentCapabilitiesTool implements Tool for the agent's self-lookup.
type FirtalGetAgentCapabilitiesTool struct {
	provider AgentCapabilitiesProvider
	tctx     ToolContext
}

func (t *FirtalGetAgentCapabilitiesTool) Name() string { return "get_agent_capabilities" }

func (t *FirtalGetAgentCapabilitiesTool) Description() string {
	return "Get YOUR OWN capabilities card — what you can do (skills), which actions are allowed, available, enforced, callable, and verified, why a call is blocked, how to fix it, what credentials/data you can reference by name only (never secret values), and what limits apply. Tool registration is not permission. Omit agent_id to inspect yourself. Call this whenever you are unsure what you are allowed to do."
}

func (t *FirtalGetAgentCapabilitiesTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{},
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "UUID of the agent to inspect. Omit to inspect yourself; you may only inspect yourself.",
			},
		},
	}
}

// Call returns the calling agent's own capabilities card. agent_id is accepted
// for contract parity with the CLI/MCP tool, but only the caller's own ID is
// permitted — this mirrors the task-scope ceiling the HTTP route enforces
// (AllowTaskScopeForAgent), so the Gateway cannot become a way to read another
// agent's card.
func (t *FirtalGetAgentCapabilitiesTool) Call(ctx context.Context, args map[string]any) (string, error) {
	if !t.tctx.AgentID.Valid {
		return "", fmt.Errorf("get_agent_capabilities: missing agent context")
	}
	if t.provider == nil {
		return "", fmt.Errorf("get_agent_capabilities: capabilities provider not configured")
	}

	self := util.UUIDToString(t.tctx.AgentID)
	if raw, ok := args["agent_id"].(string); ok && raw != "" && raw != self {
		return "", fmt.Errorf("get_agent_capabilities: you may only inspect yourself (%s)", self)
	}

	card, err := t.provider.BuildAgentCapabilitiesCard(ctx, t.tctx.AgentID)
	if err != nil {
		return "", fmt.Errorf("get_agent_capabilities: %w", err)
	}
	handler.ApplyTaskMandate(ctx, t.tctx.TaskMandates, t.tctx.TaskID, t.tctx.WorkspaceID, t.tctx.AgentID, &card)
	out, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("get_agent_capabilities: marshal result: %w", err)
	}
	return string(out), nil
}

// Names returns every tool name currently registered, in no guaranteed order.
// This is the Gateway's live surface: the exact set GetEnabledToolsForAgent can
// resolve a grant against. Availability probes read it instead of the tool
// matrix, so a tool that is declared but not implemented cannot pass as real.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	return out
}
