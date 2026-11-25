package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestContextUsageReporterThrottlesSmallChanges(t *testing.T) {
	t.Parallel()

	r := &contextUsageReporter{}
	limit := int64(200_000)
	report := func(used int64) bool {
		return r.shouldReport(agent.ContextInfo{UsedTokens: used, LimitTokens: limit})
	}

	if !report(50_000) {
		t.Fatal("first observation must be reported")
	}
	if report(51_000) {
		t.Fatal("a 1K change in a 200K window must not be reported")
	}
	if !report(55_000) {
		t.Fatal("a 5K change in a 200K window must be reported")
	}
	// The baseline is the last REPORTED figure, not the last observed one, so
	// a throttled sample cannot silently reset the step.
	if report(56_000) {
		t.Fatal("change is measured against the last reported figure")
	}
	// A drop is as informative as a rise: a run whose runtime compacted
	// reports a far smaller prompt on its next request, and that is the only
	// trace of it the reader gets.
	if !report(20_000) {
		t.Fatal("a large drop must be reported")
	}
}

// A window correction has to reach the reader even when occupancy stands
// still: the backend re-states a measurement once the runtime answers with
// its real window, and the throttle only watches occupancy.
func TestContextUsageReporterReportsACorrectedWindow(t *testing.T) {
	t.Parallel()

	r := &contextUsageReporter{}
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 120_000, LimitTokens: 200_000}) {
		t.Fatal("first observation must be reported")
	}
	// Same prompt, real window — 60% and 12% are different statements.
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 120_000, LimitTokens: 1_000_000}) {
		t.Fatal("a corrected window must be reported")
	}
	// The correction becomes the new baseline; the step is a share of it.
	if r.shouldReport(agent.ContextInfo{UsedTokens: 125_000, LimitTokens: 1_000_000}) {
		t.Fatal("a 5K change in a 1M window must not be reported")
	}
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 145_000, LimitTokens: 1_000_000}) {
		t.Fatal("a 25K change in a 1M window must be reported")
	}
}

// An occupancy with no window is not reportable at all: the step is a share
// of the window, and the figure alone is not something a reader can act on.
func TestContextUsageReporterDropsObservationsWithNoWindow(t *testing.T) {
	t.Parallel()

	r := &contextUsageReporter{}
	if r.shouldReport(agent.ContextInfo{UsedTokens: 10_000}) {
		t.Fatal("an occupancy with no window must be dropped")
	}
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 10_000, LimitTokens: 200_000}) {
		t.Fatal("a windowed figure after a dropped one is still the first report")
	}
}

func TestContextUsageReporterSkipsEmptyObservations(t *testing.T) {
	t.Parallel()

	r := &contextUsageReporter{}
	if r.shouldReport(agent.ContextInfo{LimitTokens: 200_000}) {
		t.Fatal("an observation with no occupancy says nothing and must be dropped")
	}
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 1, LimitTokens: 200_000}) {
		t.Fatal("a real figure after a dropped one is still the first report")
	}
}

func TestContextEventInputOmitsUnreportedFields(t *testing.T) {
	t.Parallel()

	// Reportable observations always carry both figures; the payload builder
	// still omits a missing one rather than writing a zero a client would
	// render as a real window.
	input := contextEventInput(agent.ContextInfo{UsedTokens: 1_000})
	if _, ok := input["limit_tokens"]; ok {
		t.Fatal("an unknown window must be absent, not zero")
	}
	if input["used_tokens"] != int64(1_000) {
		t.Fatalf("used_tokens = %v, want 1000", input["used_tokens"])
	}
	if _, ok := input["model"]; ok {
		t.Fatal("an unreported model must be absent, not empty")
	}

	// The payload carries occupancy and nothing else: a reader that meets an
	// unknown key is meeting an older daemon, not a new kind of event.
	full := contextEventInput(agent.ContextInfo{
		UsedTokens:  90_000,
		LimitTokens: 200_000,
		Model:       "claude-opus-5-5",
	})
	if len(full) != 3 {
		t.Fatalf("payload = %#v, want exactly used/limit/model", full)
	}
}
