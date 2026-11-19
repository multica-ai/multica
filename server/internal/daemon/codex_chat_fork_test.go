package daemon

import (
	"strings"
	"testing"

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
