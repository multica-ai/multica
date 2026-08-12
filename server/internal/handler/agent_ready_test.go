package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

// TestAgentReadiness_DrainingReason pins AC-15: an agent whose runtime is in
// safe-shutdown (draining) is refused new work with a dedicated reason,
// distinct from plain offline, so the issue / autopilot / squad admission
// paths (which all share service.AgentReadiness) can tell the user "the
// runtime was intentionally drained" from "the runtime is down".
func TestAgentReadiness_DrainingReason(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	agentID := insertAgent(t, ctx, testWorkspaceID, runtimeID, testUserID, "draining-readiness-agent")

	setRuntimeStatus(t, runtimeID, "draining")

	agent, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}

	ready, reason, err := service.AgentReadiness(ctx, testHandler.Queries, agent)
	if err != nil {
		t.Fatalf("AgentReadiness: %v", err)
	}
	if ready {
		t.Fatalf("AgentReadiness: draining runtime reported ready, want refused")
	}
	if !strings.Contains(reason, "draining") {
		t.Fatalf("AgentReadiness reason = %q, want it to mention draining", reason)
	}

	// Control: the same agent with the runtime back online must be ready,
	// proving the draining branch — not a stale row — caused the refusal.
	setRuntimeStatus(t, runtimeID, "online")
	ready, _, err = service.AgentReadiness(ctx, testHandler.Queries, agent)
	if err != nil {
		t.Fatalf("AgentReadiness (online control): %v", err)
	}
	if !ready {
		t.Fatalf("AgentReadiness: online runtime reported not ready, want ready")
	}
}
