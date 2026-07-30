package daemon

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestRunTaskRejectsStoredIncompatibleModelBeforeProviderSetup(t *testing.T) {
	d := &Daemon{}
	_, err := d.runTask(context.Background(), Task{
		ID:          "task-model-mismatch",
		WorkspaceID: "workspace-model-mismatch",
		Agent:       &AgentData{Model: "claude-opus-5"},
	}, "codex", 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("runTask succeeded with a Claude model on the Codex provider")
	}
	if !strings.Contains(err.Error(), "incompatible agent model") {
		t.Fatalf("runTask error = %q, want incompatible-model rejection", err)
	}
}
