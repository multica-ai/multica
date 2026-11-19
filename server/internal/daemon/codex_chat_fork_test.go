package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestRunCodexChatForkRejectsUnscopedTask(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	task := Task{
		ID:            "fork-request",
		AgentID:       "agent",
		ChatSessionID: "same-chat",
		CodexChatFork: &CodexChatForkTask{SourceChatID: "same-chat", SourceSessionID: "source-thread"},
	}
	_, err := d.runCodexChatForkTask(t.Context(), task, "codex", agent.Config{})
	if err == nil || !strings.Contains(err.Error(), "distinct source and destination chats") {
		t.Fatalf("same-chat fork returned %v", err)
	}
	task.CodexChatFork.SourceChatID = "source-chat"
	_, err = d.runCodexChatForkTask(t.Context(), task, "claude", agent.Config{})
	if err == nil || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("fork on wrong provider returned %v", err)
	}
}

func TestRunCodexChatForkReplaysAfterSourceIsGone(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/tasks/fork-request/start" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	d := &Daemon{client: NewClient(server.URL), activeStores: make(map[string]int)}
	task := Task{ID: "fork-request", AgentID: "agent", ChatSessionID: "child",
		CodexChatFork: &CodexChatForkTask{
			SourceChatID:    "source",
			SourceSessionID: "019fe3de-56a2-7792-8913-ee53664dfa0e",
		}}
	const childID = "019fe3de-56a2-7792-8913-ee53664dfa0f"
	destination := filepath.Join(execenv.CodexSessionStorePath("", execenv.TaskContextForEnv{
		AgentID: task.AgentID, ChatSessionID: task.ChatSessionID,
	}), "fork")
	for _, name := range []string{"sessions", "archived_sessions"} {
		if err := os.MkdirAll(filepath.Join(destination, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(destination, "receipt.json"), []byte(`{"request_id":"fork-request","child_id":"`+childID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "sessions", "rollout-2026-11-20T00-00-00-"+childID+".jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := d.runCodexChatForkTask(t.Context(), task, "codex", agent.Config{ExecutablePath: "must-not-launch"})
	if err != nil || result.SessionID != childID || result.Status != "completed" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
