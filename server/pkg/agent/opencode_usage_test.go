package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpencodeProcessEventsTokenUsage(t *testing.T) {
	t.Parallel()

	// OpenCode getUsage keeps reasoning inside output through v1.3.15 and
	// emits it separately from v1.3.16. The mixed case replays v1.18.31 usage.
	// https://github.com/anomalyco/opencode/pull/21047
	// https://github.com/anomalyco/opencode/blob/v1.18.31/packages/opencode/src/session/session.ts#L338
	const mixed = `{"total":1050,"input":200,"output":30,"reasoning":20,"cache":{"read":600,"write":200}}`
	const legacy = `{"total":14674,"input":14585,"output":89,"reasoning":82,"cache":{"read":0,"write":0}}`
	for _, tc := range []struct {
		name    string
		version string
		custom  bool
		tokens  []string
		want    TokenUsage
	}{
		{"first_separate_release", "1.3.16", false, []string{mixed}, TokenUsage{InputTokens: 200, OutputTokens: 50, CacheReadTokens: 600, CacheWriteTokens: 200}},
		{"current_release", "1.18.31", false, []string{mixed}, TokenUsage{InputTokens: 200, OutputTokens: 50, CacheReadTokens: 600, CacheWriteTokens: 200}},
		{"v_prefix_and_whitespace", " v1.17.7\n", false, []string{mixed}, TokenUsage{InputTokens: 200, OutputTokens: 50, CacheReadTokens: 600, CacheWriteTokens: 200}},
		{"multiple_steps", "1.18.31", false, []string{mixed, mixed}, TokenUsage{InputTokens: 400, OutputTokens: 100, CacheReadTokens: 1200, CacheWriteTokens: 400}},
		{"missing_total", "1.18.31", false, []string{`{"input":200,"output":30,"reasoning":20,"cache":{"read":600,"write":200}}`}, TokenUsage{InputTokens: 200, OutputTokens: 50, CacheReadTokens: 600, CacheWriteTokens: 200}},
		{"total_does_not_choose_output_convention", "1.18.31", false, []string{`{"total":1030,"input":200,"output":30,"reasoning":20,"cache":{"read":600,"write":200}}`}, TokenUsage{InputTokens: 200, OutputTokens: 50, CacheReadTokens: 600, CacheWriteTokens: 200}},
		{"all_output_is_reasoning", "1.18.31", false, []string{`{"input":0,"output":0,"reasoning":20}`}, TokenUsage{OutputTokens: 20}},
		{"cache_only", "1.18.31", false, []string{`{"input":0,"output":0,"cache":{"read":600,"write":200}}`}, TokenUsage{CacheReadTokens: 600, CacheWriteTokens: 200}},
		{"no_reasoning_breakdown", "1.18.31", false, []string{`{"input":200,"output":50}`}, TokenUsage{InputTokens: 200, OutputTokens: 50}},
		{"negative_reasoning_does_not_reduce_output", "1.18.31", false, []string{`{"input":200,"output":50,"reasoning":-20}`}, TokenUsage{InputTokens: 200, OutputTokens: 50}},
		{"last_inclusive_release", "1.3.15", false, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
		{"legacy_without_total", "1.3.15", false, []string{`{"input":200,"output":50,"reasoning":20}`}, TokenUsage{InputTokens: 200, OutputTokens: 50}},
		{"missing_version", "", false, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
		{"unrecognized_version", "unknown", false, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
		{"prerelease_is_not_release_evidence", "1.18.31-beta.1", false, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
		{"development_build_is_not_release_evidence", "v1.3.15-7-gabcdef", false, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
		{"banner_is_not_release_evidence", "wrapper 1.18.31", false, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
		{"unverified_major_version", "2.0.0", false, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
		{"custom_command_version_is_not_opencode_version", "1.18.31", true, []string{legacy}, TokenUsage{InputTokens: 14585, OutputTokens: 89}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := &opencodeBackend{cfg: Config{Logger: slog.Default(), CLIVersion: tc.version, BuiltinRuntime: !tc.custom}}
			var stream strings.Builder
			for i, tokens := range tc.tokens {
				stream.WriteString(`{"type":"step_start","sessionID":"ses_usage","part":{"type":"step-start"}}` + "\n")
				reason := "tool-calls"
				if i == len(tc.tokens)-1 {
					reason = "stop"
				}
				fmt.Fprintf(&stream, "{\"type\":\"step_finish\",\"sessionID\":\"ses_usage\",\"part\":{\"type\":\"step-finish\",\"reason\":%q,\"tokens\":%s}}\n", reason, tokens)
			}
			ch := make(chan Message, 32)
			result := b.processEvents(strings.NewReader(stream.String()), ch)
			close(ch)
			if result.status != "completed" || result.errMsg != "" {
				t.Fatalf("status=%q error=%q", result.status, result.errMsg)
			}
			if result.usage != tc.want {
				t.Fatalf("usage=%+v want=%+v", result.usage, tc.want)
			}
		})
	}
}

func TestOpencodeExecuteRetainsReasoningOnlyUsage(t *testing.T) {
	t.Parallel()
	// A reasoning-only stream used to pass the step-liveness guard but lose
	// its entire usage entry because all four normalized counters were zero.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	backend, err := New("opencode", Config{
		ExecutablePath: self,
		CLIVersion:     "1.18.31",
		BuiltinRuntime: true,
		Logger:         slog.Default(),
		Env: map[string]string{
			opencodeStdinHelperEnv:       "1",
			opencodeStdinHelperArgvFile:  filepath.Join(dir, "argv.txt"),
			opencodeStdinHelperInFile:    filepath.Join(dir, "stdin.txt"),
			opencodeStdinHelperUsageOnly: "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir(), Model: "test/model"})
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	go func() {
		for range session.Messages {
		}
		close(drained)
	}()
	select {
	case result := <-session.Result:
		if result.Status != "completed" || result.Error != "" {
			t.Fatalf("status=%q error=%q", result.Status, result.Error)
		}
		if got, want := result.Usage["test/model"], (TokenUsage{OutputTokens: 20}); got != want {
			t.Fatalf("reported usage=%+v want=%+v", got, want)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	<-drained
}
