package runtime

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestGuardToolCallMissingPolicyDecisionServiceFailsClosed(t *testing.T) {
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

// This contract test makes removal of the Gateway's fail-open paths durable.
// Reintroducing either the runtime-tool cascade (including agent_tool_grant) or
// the hardcoded POC tool server makes the test fail before the code can ship.
func TestGatewayUsesOnlyPolicyDecisionServiceForToolExposure(t *testing.T) {
	_, currentFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("locate contract test")
	}
	files := map[string][]string{
		"firtal_gateway_executor.go": {
			"GetCascadeEnabledToolsForAgent(",
			"GetEnabledToolsForAgent(",
			"NewFirtalGatewayToolServer(",
		},
		"approval_gate.go": {
			"decision = \"allow_gate_off\"",
			"decision = \"allow_out_of_scope\"",
			"return e.guardToolCallViaPolicy(",
			"e.gate.Guard(ctx, req)",
		},
	}
	for name, forbidden := range files {
		source, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, fragment := range forbidden {
			if strings.Contains(string(source), fragment) {
				t.Fatalf("Gateway fail-open path reintroduced in %s: %s", name, fragment)
			}
		}
	}
}
