package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestResolveTaskModelSelectionThinkingLevelWarning(t *testing.T) {
	catalog := agent.Catalog{
		Models: []agent.Model{
			{ID: "claude-sonnet-5", Default: true, Thinking: &agent.ModelThinking{
				SupportedLevels: []agent.ThinkingLevel{{Value: "medium"}},
			}},
			{ID: "claude-haiku-4-5-20251001"},
		},
		Unavailable: []agent.UnavailableModel{{ID: "disabled[1m]"}},
	}
	for _, tc := range []struct {
		name     string
		provider string
		model    string
		level    string
		want     string
	}{
		{name: "supported", provider: "claude", model: "claude-sonnet-5", level: "medium"},
		{name: "unlisted medium", provider: "claude", model: "claude-sonnet-4-6", level: "medium"},
		{name: "unlisted max", provider: "claude", model: "claude-sonnet-4-6", level: "max"},
		{name: "no effort", provider: "claude", model: "claude-haiku-4-5-20251001", level: "medium", want: `Thinking level "medium" was not applied: model "claude-haiku-4-5-20251001" does not advertise thinking-level control. The runtime will use its default thinking level.`},
		{name: "unsupported level", provider: "claude", model: "claude-sonnet-5", level: "max", want: `Thinking level "max" was not applied: model "claude-sonnet-5" does not support this thinking level. The runtime will use its default thinking level.`},
		{name: "unavailable", provider: "claude", model: "disabled", level: "max", want: `Thinking level "max" was not applied: model "disabled" is unavailable in the installed CLI. The runtime will use its default thinking level.`},
		{name: "Codex requires model", provider: "codex", level: "medium", want: `Thinking level "medium" was not applied: codex requires an explicit model. The runtime will use its default thinking level.`},
		{name: "OMP requires model", provider: "omp", level: "max", want: `Thinking level "max" was not applied: omp requires an explicit model. The runtime will use its default thinking level.`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubModelDiscovery(t, map[string]agent.Catalog{tc.provider: catalog})
			sel := resolveTaskModelSelection(context.Background(), tc.provider, agent.Command{}, taskModelSelection{
				Model: tc.model, ThinkingLevel: tc.level,
			}, quietTaskLog())
			if sel.Model != tc.model || sel.ThinkingLevelWarning != tc.want {
				t.Fatalf("selection = %+v, want model %q and warning %q", sel, tc.model, tc.want)
			}
			wantLevel := tc.level
			if tc.want != "" {
				wantLevel = ""
			}
			if sel.ThinkingLevel != wantLevel {
				t.Fatalf("launch effort = %q, want %q", sel.ThinkingLevel, wantLevel)
			}
			d, rec := newTranscriptRecorder(t)
			var seq atomic.Int32
			d.reportThinkingLevelWarning(context.Background(), "task-effort", sel.ThinkingLevelWarning, &seq, quietTaskLog())
			rows := rec.snapshot()
			if tc.want == "" {
				if len(rows) != 0 || seq.Load() != 0 {
					t.Fatalf("a preserved effort should not produce a fallback warning: %+v", rows)
				}
				return
			}
			if len(rows) != 1 || rows[0].Type != "text" || rows[0].Content != tc.want+"\n\n" || rows[0].Seq != 1 || rows[0].CreatedAt.IsZero() {
				t.Fatalf("persisted warning = %+v, want a timestamped text message before runtime output", rows)
			}
		})
	}
}

func TestThinkingLevelWarningSequenceAcrossResumeRetry(t *testing.T) {
	t.Parallel()
	d, rec := newTranscriptRecorder(t)
	var seq atomic.Int32
	d.reportThinkingLevelWarning(context.Background(), "task-effort", "Thinking level was not applied.", &seq, quietTaskLog())
	for attempt := 0; attempt < 2; attempt++ {
		messages := make(chan agent.Message, 1)
		messages <- agent.Message{Type: agent.MessageText, Content: "runtime output"}
		close(messages)
		results := make(chan agent.Result, 1)
		results <- agent.Result{Status: "completed"}
		close(results)
		backend := sessionBackend{session: &agent.Session{Messages: messages, Result: results}}
		result, _, err := d.executeAndDrain(context.Background(), backend, "p", agent.ExecOptions{}, quietTaskLog(), "task-effort", "", &seq)
		if err != nil || result.Status != "completed" {
			t.Fatalf("warning must not change the task outcome: result=%+v, err=%v", result, err)
		}
	}
	rows := rec.snapshot()
	slices.SortFunc(rows, func(a, b TaskMessageData) int { return a.Seq - b.Seq })
	if len(rows) != 3 || rows[0].Content != "Thinking level was not applied.\n\n" {
		t.Fatalf("expected one warning followed by two attempts' output: %+v", rows)
	}
	for i, row := range rows {
		if row.Seq != i+1 {
			t.Fatalf("warning/retry sequence collision: %+v", rows)
		}
	}
}

func TestThinkingLevelWarningReportFailure(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	d := &Daemon{client: NewClient(srv.URL)}
	var seq atomic.Int32
	d.reportThinkingLevelWarning(context.Background(), "task-effort", "Thinking level was not applied.", &seq, quietTaskLog())
	if requests.Load() != 1 || seq.Load() != 1 {
		t.Fatalf("report failure must return without retrying or reusing the sequence: requests=%d, seq=%d", requests.Load(), seq.Load())
	}
}

func TestThinkingLevelWarningRedactsBeforeReporting(t *testing.T) {
	t.Parallel()
	d, rec := newTranscriptRecorder(t)
	var seq atomic.Int32
	secret := "sk-" + strings.Repeat("a", 48)
	d.reportThinkingLevelWarning(context.Background(), "task-effort", "Model "+secret+" does not support effort.", &seq, quietTaskLog())
	rows := rec.snapshot()
	if len(rows) != 1 || strings.Contains(rows[0].Content, secret) {
		t.Fatalf("warning must be redacted before leaving the daemon: %+v", rows)
	}
}
