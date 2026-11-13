package agent

import (
	"log/slog"
	"slices"
	"testing"
)

// fixtureUsageFile is the --usage-output-file summary from #10501 (Copilot CLI
// 1.0.109). inputTokens already includes the cache tiers (17820 = 3 uncached +
// 17817 cache write), and agentMetrics repeats the same totals per sub-agent.
const fixtureUsageFile = `{
  "totalPremiumRequestCost": 1,
  "totalNanoAiu": 4459850000,
  "modelMetrics": {
    "gpt-6.1-sol": {
      "requests": { "count": 1, "cost": 1 },
      "usage": { "inputTokens": 17820, "outputTokens": 5, "cacheReadTokens": 0, "cacheWriteTokens": 17817, "reasoningTokens": 2 }
    }
  },
  "agentMetrics": {
    "main": {
      "modelMetrics": {
        "gpt-6.1-sol": {
          "requests": { "count": 1, "cost": 1 },
          "usage": { "inputTokens": 17820, "outputTokens": 5, "cacheReadTokens": 0, "cacheWriteTokens": 17817, "reasoningTokens": 2 }
        }
      }
    }
  }
}`

func TestCopilotUsageFileReportsModelMetricsOnce(t *testing.T) {
	t.Parallel()
	// The modern stream: no assistant.usage and no session.shutdown reach
	// --output-format json, so the usage file is the only source of tokens.
	_, st := runCopilotEventLoop(t, []string{fixtureSessionStart, fixtureTurnStart, fixtureResult}, "copilot", false)

	if err := st.applyUsageFile([]byte(fixtureUsageFile)); err != nil {
		t.Fatalf("applyUsageFile: %v", err)
	}

	usage := st.resolveUsage()
	if len(usage) != 1 {
		t.Fatalf("expected one model, got %#v", usage)
	}
	// Cache write is taken out of input, reasoning stays inside output, and
	// agentMetrics is not added on top of modelMetrics.
	want := TokenUsage{InputTokens: 3, OutputTokens: 5, CacheWriteTokens: 17817}
	if got := usage["gpt-6.1-sol"]; got != want {
		t.Fatalf("usage = %#v, want %#v", got, want)
	}
}

func TestCopilotUsageFileSkippedOnResume(t *testing.T) {
	t.Parallel()
	// Like session.shutdown, the file holds session totals; on a resumed
	// session that would bill every earlier turn again.
	_, st := runCopilotEventLoop(t, []string{fixtureSessionStart, fixtureTurnStart, fixtureResult}, "copilot", true)

	if err := st.applyUsageFile([]byte(fixtureUsageFile)); err != nil {
		t.Fatalf("applyUsageFile: %v", err)
	}
	if usage := st.resolveUsage(); len(usage) != 0 {
		t.Fatalf("expected no usage on a resumed run, got %#v", usage)
	}
}

func TestCopilotUsageFileDoesNotDisplaceStreamTotals(t *testing.T) {
	t.Parallel()
	// An older CLI that still streams session.shutdown keeps reporting exactly
	// what it does today; the file never adds to it.
	_, st := runCopilotEventLoop(t, []string{fixtureSessionStart, fixtureTurnStart, fixtureShutdown, fixtureResult}, "copilot", false)

	if err := st.applyUsageFile([]byte(fixtureUsageFile)); err != nil {
		t.Fatalf("applyUsageFile: %v", err)
	}
	usage := st.resolveUsage()
	if _, ok := usage["gpt-6.1-sol"]; ok || usage["claude-sonnet-4.5"].OutputTokens != 900 {
		t.Fatalf("expected the session.shutdown totals alone, got %#v", usage)
	}
}

func TestCopilotUsageFileMalformedLeavesRunUnreported(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "{", `{"modelMetrics":{"gpt-6.1-sol":{"usage":{}}}}`} {
		_, st := runCopilotEventLoop(t, []string{fixtureSessionStart, fixtureTurnStart, fixtureResult}, "copilot", false)
		_ = st.applyUsageFile([]byte(raw))
		if usage := st.resolveUsage(); len(usage) != 0 {
			t.Fatalf("usage file %q: expected unreported, got %#v", raw, usage)
		}
	}
}

func TestCopilotUsageFileSupported(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"built-in at the floor", Config{BuiltinRuntime: true, CLIVersion: "1.0.81"}, true},
		{"built-in newer", Config{BuiltinRuntime: true, CLIVersion: "GitHub Copilot CLI 1.0.109."}, true},
		{"built-in older", Config{BuiltinRuntime: true, CLIVersion: "1.0.80"}, false},
		{"built-in, version unknown", Config{BuiltinRuntime: true}, false},
		{"custom runtime profile", Config{BuiltinRuntime: false, CLIVersion: "1.0.109"}, false},
	}
	for _, tc := range cases {
		if got := copilotUsageFileSupported(tc.cfg); got != tc.want {
			t.Errorf("%s: copilotUsageFileSupported = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBuildCopilotArgsBlocksCustomUsageFile(t *testing.T) {
	t.Parallel()
	// The daemon passes its own per-run file; a user-supplied one would either
	// collide with it or redirect the summary somewhere the daemon never reads.
	args := buildCopilotArgs(ExecOptions{CustomArgs: []string{"--usage-output-file", "/tmp/mine.json", "--model-hint"}}, slog.Default())

	if slices.Contains(args, "--usage-output-file") || slices.Contains(args, "/tmp/mine.json") {
		t.Fatalf("expected --usage-output-file to be filtered from custom args, got %v", args)
	}
	if !slices.Contains(args, "--model-hint") {
		t.Fatalf("expected unrelated custom args to survive, got %v", args)
	}
}
