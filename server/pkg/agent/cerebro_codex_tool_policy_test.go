package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestCodexApprovalRequestsUseToolPolicy(t *testing.T) {
	for _, tc := range []struct {
		method       string
		wantTool     string
		allowed      bool
		wantDecision string
	}{
		{"item/commandExecution/requestApproval", "bash", true, "accept"},
		{"item/commandExecution/requestApproval", "bash", false, "decline"},
		{"item/fileChange/requestApproval", "apply_patch", true, "accept"},
		{"item/fileChange/requestApproval", "apply_patch", false, "decline"},
	} {
		t.Run(fmt.Sprintf("%s/%t", tc.wantTool, tc.allowed), func(t *testing.T) {
			stdin := &fakeStdin{}
			called := ""
			client := &codexClient{
				cfg: Config{
					Logger: slog.Default(),
					ToolPolicy: func(_ context.Context, tool string, _ map[string]any) (bool, string) {
						called = tool
						return tc.allowed, "test verdict"
					},
				},
				stdin: stdin,
			}
			client.handleServerRequest(map[string]json.RawMessage{
				"id":     json.RawMessage(`7`),
				"method": json.RawMessage(fmt.Sprintf("%q", tc.method)),
				"params": json.RawMessage(`{"command":"echo ok"}`),
			})
			if called != tc.wantTool {
				t.Fatalf("policy tool = %q, want %q", called, tc.wantTool)
			}
			if !strings.Contains(string(stdin.data), `"decision":"`+tc.wantDecision+`"`) {
				t.Fatalf("response = %s, want %s", stdin.data, tc.wantDecision)
			}
		})
	}
}

func TestCodexApprovalFailsClosedWithoutPolicy(t *testing.T) {
	stdin := &fakeStdin{}
	client := &codexClient{cfg: Config{Logger: slog.Default()}, stdin: stdin}
	client.handleServerRequest(map[string]json.RawMessage{
		"id":     json.RawMessage(`8`),
		"method": json.RawMessage(`"item/commandExecution/requestApproval"`),
	})
	if !strings.Contains(string(stdin.data), `"decision":"decline"`) {
		t.Fatalf("response = %s, want decline", stdin.data)
	}
}
