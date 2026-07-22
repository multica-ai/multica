package runtime

// FIR-2761 / FIR-3400 — cloud Gateway tool-loop admission follows the
// authoritative Policy Decision Service, not the retired cascade or CSV list.

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestAgentHasCallableTools_UsesPolicyDecisionServiceAllow(t *testing.T) {
	e, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	e.registry = NewRegistry(runtimeAccountTestPool)
	setAgentToolPolicy(t, agentID, "get_issue", toolpolicy.SettingAllow)

	if !e.agentHasCallableTools(context.Background(), agentID, runtimeAccountTestWSID, runtimeAccountTestUserID, runtimeAccountTestUserID) {
		t.Fatal("expected callable tools when the Policy Decision Service allows a live registry tool")
	}
}

func TestAgentHasCallableTools_MissingPolicyDecisionServiceFailsClosed(t *testing.T) {
	e, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	e.registry = NewRegistry(runtimeAccountTestPool)
	e.accessDecisionObserver = nil

	if e.agentHasCallableTools(context.Background(), agentID, runtimeAccountTestWSID, runtimeAccountTestUserID, runtimeAccountTestUserID) {
		t.Fatal("expected chat-only when the Policy Decision Service is unavailable")
	}
}
