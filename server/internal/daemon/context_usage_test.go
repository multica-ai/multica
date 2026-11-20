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
	// A drop is as informative as a rise: it is what compaction looks like
	// when the runtime reported no boundary event.
	if !report(20_000) {
		t.Fatal("a large drop must be reported")
	}
}

func TestContextUsageReporterUnknownLimitUsesAbsoluteStep(t *testing.T) {
	t.Parallel()

	r := &contextUsageReporter{}
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 10_000}) {
		t.Fatal("first observation must be reported")
	}
	if r.shouldReport(agent.ContextInfo{UsedTokens: 12_000}) {
		t.Fatal("below the absolute step, must not be reported")
	}
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 14_000}) {
		t.Fatal("at the absolute step, must be reported")
	}
}

func TestContextUsageReporterAlwaysReportsLifecycleEvents(t *testing.T) {
	t.Parallel()

	r := &contextUsageReporter{}
	if !r.shouldReport(agent.ContextInfo{UsedTokens: 100_000, LimitTokens: 200_000}) {
		t.Fatal("first observation must be reported")
	}
	for range 3 {
		if !r.shouldReport(agent.ContextInfo{
			Phase:       agent.ContextPhaseCompacted,
			PreTokens:   180_000,
			LimitTokens: 200_000,
			Trigger:     "auto",
		}) {
			t.Fatal("a compaction event must never be throttled")
		}
	}
	// A compaction carries no occupancy of its own, so it must not become the
	// baseline the next usage sample is measured against.
	if r.shouldReport(agent.ContextInfo{UsedTokens: 101_000, LimitTokens: 200_000}) {
		t.Fatal("a lifecycle event must not reset the usage baseline")
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

	input := contextEventInput(agent.ContextInfo{UsedTokens: 1_000})
	if _, ok := input["limit_tokens"]; ok {
		t.Fatal("an unknown window must be absent, not zero")
	}
	if _, ok := input["phase"]; ok {
		t.Fatal("a plain usage observation must carry no phase")
	}
	if input["used_tokens"] != int64(1_000) {
		t.Fatalf("used_tokens = %v, want 1000", input["used_tokens"])
	}

	compaction := contextEventInput(agent.ContextInfo{
		Phase:     agent.ContextPhaseCompacted,
		PreTokens: 180_000,
		Trigger:   "auto",
	})
	if compaction["phase"] != agent.ContextPhaseCompacted {
		t.Fatalf("phase = %v, want %q", compaction["phase"], agent.ContextPhaseCompacted)
	}
	if compaction["pre_tokens"] != int64(180_000) || compaction["trigger"] != "auto" {
		t.Fatalf("unexpected compaction payload: %#v", compaction)
	}
}
