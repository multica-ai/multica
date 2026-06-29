package daemon

// CEREBRO-PATCH(daemon-report-context-footprint-guard): net-new cerebro tripwire
// test (FIR-1931).
//
// The daemon converts the agent's per-model usage map into TaskUsageEntry values
// before reporting them to the server. The streaming runtimes (Claude et al.)
// compute the last-turn context footprint and stamp it onto those map entries
// (ContextInputTokens/ContextCacheReadTokens via lastTurnFootprint.applyToMap),
// but the conversion must copy those two fields onto the outgoing TaskUsageEntry
// or the footprint is silently dropped at the report boundary — the server then
// falls back to the cumulative sum, which over-counts the warm cache and pins the
// context bar at "~100% · estimate" for every local Claude/[1m] session.
//
// A CEREBRO-PATCH marker cannot catch a deletion (once the line is gone there is
// no marker left to validate), and this conversion lives in the large upstream
// daemon.go where two prior upstream syncs already wiped the sibling usage wiring
// (see handler/daemon_usage_wiring_cerebro_test.go, FIR-2189). This source-presence
// tripwire fails loudly in CI the next time the forwarding disappears.

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonContextFootprintForwarded(t *testing.T) {
	t.Parallel()

	const path = "daemon.go"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Collapse whitespace runs so the check is immune to gofmt re-aligning the
	// struct-literal columns.
	body := strings.Join(strings.Fields(string(src)), " ")

	required := []struct {
		fragment string
		why      string
	}{
		{
			fragment: "ContextInputTokens: u.ContextInputTokens",
			why:      "the daemon→server usage conversion must forward the last-turn footprint size, or local Claude/[1m] sessions fall back to the cumulative \"estimate\" (FIR-1931).",
		},
		{
			fragment: "ContextCacheReadTokens: u.ContextCacheReadTokens",
			why:      "the cached subset of the footprint must travel with it so the bar can show cache share, not just the cumulative fallback (FIR-1931).",
		},
	}

	for _, r := range required {
		if !strings.Contains(body, r.fragment) {
			t.Errorf("daemon.go is missing required cerebro footprint wiring:\n  fragment: %q\n  why: %s\n  (likely deleted by an upstream sync — see FIR-1931 / docs/cerebro-patches.md)", r.fragment, r.why)
		}
	}
}
