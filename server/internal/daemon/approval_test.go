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
