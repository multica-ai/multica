package runtime

import (
	"context"
	"log/slog"
	"testing"
)

func TestGuardToolCallMissingPolicyDecisionServiceFailsClosed(t *testing.T) {
	// Historical rollout switches must not be able to reopen the retired
	// Gateway paths. This is a behaviour assertion at the dispatch choke-point,
	// not a text search through selected source files.
	t.Setenv("MULTICA_SERVER_FIRTAL_GATEWAY_INPROCESS_BRIDGE", "true")
	t.Setenv("CEREBRO_APPROVAL_GATE_ENABLED", "false")
	t.Setenv("CEREBRO_APPROVAL_GATE_MODE", "observe")
	t.Setenv("CEREBRO_APPROVAL_GATE_AGENTS", "*")

	e := &FirtalGatewayExecutor{logger: slog.Default()}
	allowed, reason := e.guardToolCall(
		context.Background(), gateTestUUID(1), gateTestUUID(9), "get_issue", nil, nil, GatewayRequestMeta{},
	)
	if allowed {
		t.Fatal("missing Policy Decision Service allowed a Gateway tool call")
	}
	if reason != "policy decision service unavailable" {
		t.Fatalf("reason = %q, want policy decision service unavailable", reason)
	}
}
