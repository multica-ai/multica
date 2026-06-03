package runtime

// FIR-2761 — cloud gateway tool-loop gate follows runtime tools, not CSV allowlist.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestAgentHasCallableTools_UsesRuntimeGrantsNotCSVAllowlist(t *testing.T) {
	f := newCascadeFixture(t)
	// Use a real built-in tool name: agentHasCallableTools resolves through a
	// per-task NewDefaultRegistry that only carries the cerebro built-ins, so a
	// stub name like "alpha" would be filtered out even when the cascade emits
	// it. "get_issue" is registered by registerBuiltinTools, so it round-trips.
	f.addRuntimeTool("get_issue", true)
	f.addUserGrant("get_issue", f.userID)

	e := &FirtalGatewayExecutor{
		registry: f.registry,
		cerebro:  f.cerebro,
	}
	cfg := FirtalGatewayRuntimeConfig{
		ToolsEnabledAgentIDs: []pgtype.UUID{f.agentID},
	}
	_ = cfg // would have enabled tool loop under the old CSV gate

	if !e.agentHasCallableTools(context.Background(), f.agentID, runtimeAccountTestWSID, f.userID) {
		t.Fatal("expected callable tools when runtime grants exist")
	}
}

func TestAgentHasCallableTools_FalseWithoutGrantsEvenOnCSVAllowlist(t *testing.T) {
	f := newCascadeFixture(t)
	// Same built-in name as the positive case so both arms test the cascade end
	// to end. Without a grant the cascade rejects the user; agentHasCallableTools
	// must return false, even though the agent sits on the deprecated CSV.
	f.addRuntimeTool("get_issue", true)
	// No user/group grants — runtime default-deny.

	e := &FirtalGatewayExecutor{
		registry: f.registry,
		cerebro:  f.cerebro,
	}
	cfg := FirtalGatewayRuntimeConfig{
		ToolsEnabledAgentIDs: []pgtype.UUID{f.agentID},
	}
	if !cfg.ToolsEnabledForAgent(f.agentID) {
		t.Fatal("test setup: agent should be on deprecated CSV allowlist")
	}

	if e.agentHasCallableTools(context.Background(), f.agentID, runtimeAccountTestWSID, f.userID) {
		t.Fatal("expected chat-only without runtime grants, even when CSV allowlist includes agent")
	}
}
