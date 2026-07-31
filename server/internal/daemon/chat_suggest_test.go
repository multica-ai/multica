package daemon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestRunChatSuggestPassReturnsTrimmedOutputAndResumesSession(t *testing.T) {
	t.Parallel()
	backend := &fakeBackend{
		results: []agent.Result{{
			Status: "completed",
			Output: "\n[{\"label\":\"Next\",\"prompt\":\"Do the next thing\"}]\n",
			Usage:  map[string]agent.TokenUsage{"m1": {InputTokens: 10, OutputTokens: 5}},
		}},
	}
	d := &Daemon{logger: slog.Default()}

	raw, usage, ok := d.runChatSuggestPass(context.Background(), backend, agent.ExecOptions{Model: "m1"}, "sess-1", slog.Default())
	if !ok {
		t.Fatal("a completed pass must report ok=true")
	}
	if raw != `[{"label":"Next","prompt":"Do the next thing"}]` {
		t.Fatalf("raw = %q", raw)
	}
	if usage["m1"].OutputTokens != 5 {
		t.Fatalf("usage must be forwarded, got %+v", usage)
	}
	if len(backend.calls) != 1 {
		t.Fatalf("expected one backend call, got %d", len(backend.calls))
	}
	opts := backend.calls[0]
	if opts.ResumeSessionID != "sess-1" || !opts.ResumeExpected {
		t.Fatalf("suggest pass must resume the finished session, got %+v", opts)
	}
	if opts.Timeout != chatSuggestTimeout || opts.IdleWatchdogTimeout != chatSuggestTimeout {
		t.Fatalf("suggest pass must narrow its timeouts, got %+v", opts)
	}
}

func TestRunChatSuggestPassSoftFailsOnErrorAndNonCompletion(t *testing.T) {
	t.Parallel()
	d := &Daemon{logger: slog.Default()}

	failedStart := &fakeBackend{errors: []error{context.DeadlineExceeded}, results: []agent.Result{{}}}
	if raw, _, ok := d.runChatSuggestPass(context.Background(), failedStart, agent.ExecOptions{}, "sess-1", slog.Default()); raw != "" || ok {
		t.Fatalf("start failure must report ok=false and no suggestions, got raw=%q ok=%v", raw, ok)
	}

	notCompleted := &fakeBackend{results: []agent.Result{{Status: "timeout", Error: "slow", Usage: map[string]agent.TokenUsage{"m": {InputTokens: 1}}}}}
	raw, usage, ok := d.runChatSuggestPass(context.Background(), notCompleted, agent.ExecOptions{}, "sess-1", slog.Default())
	if raw != "" || ok {
		t.Fatalf("non-completed result must report ok=false and no suggestions, got raw=%q ok=%v", raw, ok)
	}
	if usage["m"].InputTokens != 1 {
		t.Fatalf("tokens burned by a failed pass must still be reported, got %+v", usage)
	}
}
