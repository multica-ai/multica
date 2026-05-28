package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestApprovalSimilarKey_CommandUsesConcreteCommand(t *testing.T) {
	t.Parallel()

	reqA := agent.ApprovalRequest{Type: "command_approval", Detail: "git status"}
	reqB := agent.ApprovalRequest{Type: "command_approval", Detail: "rm -rf /tmp/example"}

	if approvalSimilarKey(reqA) == approvalSimilarKey(reqB) {
		t.Fatalf("different commands should not share the same similarity key")
	}
}

func TestApprovalSimilarKey_FileChangeUsesPath(t *testing.T) {
	t.Parallel()

	reqA := agent.ApprovalRequest{Type: "file_change_approval", Detail: `{"path":"src/a.ts"}`}
	reqB := agent.ApprovalRequest{Type: "file_change_approval", Detail: `{"path":"src/b.ts"}`}

	if approvalSimilarKey(reqA) == approvalSimilarKey(reqB) {
		t.Fatalf("different file paths should not share the same similarity key")
	}
}

func TestPromptViaServer_DisablesExpiryForInteractiveRequests(t *testing.T) {
	t.Parallel()

	var (
		reported map[string]any
		cancel   context.CancelFunc
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/daemon/tasks/task-1/interactions":
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&reported); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"interaction-1"}`))
			if cancel != nil {
				cancel()
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, stop := context.WithCancel(context.Background())
	cancel = stop

	_, _, _ = promptViaServer(ctx, "task-1", "claude", client, agent.ApprovalRequest{
		Type:  protocol.InteractionPermissionRequest,
		Title: "Post comment",
	})

	if got, ok := reported["expires_in"].(float64); !ok || got != -1 {
		t.Fatalf("expires_in = %v, want -1", reported["expires_in"])
	}
}

func TestPromptViaServer_CommandApprovalPreservesTerminalState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		status     string
		reqType    string
		wantChosen string
	}{
		{"command_timed_out", protocol.InteractionStatusTimedOut, protocol.InteractionCommandApproval, protocol.InteractionStatusTimedOut},
		{"command_cancelled", protocol.InteractionStatusCancelled, protocol.InteractionCommandApproval, protocol.InteractionStatusCancelled},
		{"file_change_timed_out_still_deny", protocol.InteractionStatusTimedOut, "file_change_approval", "deny"},
		{"file_change_cancelled_still_deny", protocol.InteractionStatusCancelled, "file_change_approval", "deny"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pollCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost:
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":"interaction-1"}`))
				case r.Method == http.MethodGet:
					pollCount++
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"status":"` + tc.status + `"}`))
				}
			}))
			defer server.Close()

			client := NewClient(server.URL)
			ctx := context.Background()

			chosen, approved, _ := promptViaServer(ctx, "task-1", "claude", client, agent.ApprovalRequest{
				Type:  tc.reqType,
				Title: "Run: rm -rf /",
			})
			if approved {
				t.Fatal("expected approved=false")
			}
			if chosen != tc.wantChosen {
				t.Fatalf("chosen = %q, want %q", chosen, tc.wantChosen)
			}
		})
	}
}
