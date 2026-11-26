package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestGateCodexResumeToContextSize(t *testing.T) {
	t.Parallel()
	const session = "01a20e57-0000-7000-8000-000000000002"
	codexHomeWith := func(t *testing.T, input, window int64) string {
		t.Helper()
		home := t.TempDir()
		path := filepath.Join(home, "sessions", "2026", "11", "27", "rollout-2026-11-27T00-00-00-"+session+".jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		line := fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":%d},"model_context_window":%d}}}`, input, window)
		if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return home
	}

	tests := []struct {
		name     string
		provider string
		input    int64
		window   int64
		percent  int
		wantDrop bool
	}{
		{name: "near the window starts fresh", provider: "codex", input: 205680, window: 272000, percent: 60, wantDrop: true},
		{name: "exactly at the limit starts fresh", provider: "codex", input: 163200, window: 272000, percent: 60, wantDrop: true},
		{name: "below the limit resumes", provider: "codex", input: 163199, window: 272000, percent: 60},
		{name: "disabled", provider: "codex", input: 270000, window: 272000, percent: 0},
		{name: "unknown window resumes", provider: "codex", input: 270000, window: 0, percent: 60},
		{name: "other providers are untouched", provider: "claude", input: 270000, window: 272000, percent: 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			task := Task{PriorSessionID: session}
			taskCtx := execenv.TaskContextForEnv{PriorSessionResumed: true}
			gateCodexResumeToContextSize(&task, &taskCtx, tt.provider, codexHomeWith(t, tt.input, tt.window), tt.percent, slog.Default())

			dropped := task.PriorSessionID == ""
			if dropped != tt.wantDrop {
				t.Fatalf("dropped = %v, want %v", dropped, tt.wantDrop)
			}
			if taskCtx.PriorSessionResumed == dropped {
				t.Fatalf("PriorSessionResumed = %v after dropped = %v", taskCtx.PriorSessionResumed, dropped)
			}
			// A dropped resume must be disclosed in the prompt and the brief.
			if task.PriorSessionResumeUnavailable != dropped || taskCtx.PriorSessionResumeUnavailable != dropped {
				t.Fatalf("resume-unavailable flags = %v/%v, want %v",
					task.PriorSessionResumeUnavailable, taskCtx.PriorSessionResumeUnavailable, dropped)
			}
		})
	}
}
