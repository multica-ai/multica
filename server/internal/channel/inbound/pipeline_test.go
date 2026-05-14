package inbound_test

// Tests for the inbound pipeline orchestrator.

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// spyStep is a Step double that records every call and returns a caller
// configured Decision/error. It lives in the *_test.go file so it can
// never leak into the production binary (DESIGN §3.2).
// ---------------------------------------------------------------------------

type spyStep struct {
	name     string
	decision inbound.Decision
	err      error

	// Recorded after Run is invoked. The slice grows by appending the
	// step's own name into the shared *log so callers can assert
	// cross-step ordering with a single shared log slice.
	calls int
	log   *[]string
}

func newSpy(name string, decision inbound.Decision, log *[]string) *spyStep {
	return &spyStep{name: name, decision: decision, log: log}
}

func (s *spyStep) Name() string { return s.name }

func (s *spyStep) Run(_ context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	s.calls++
	*s.log = append(*s.log, s.name)
	return evt, s.decision, s.err
}

// ---------------------------------------------------------------------------
// Continue all the way -> all steps run once, in order.
// ---------------------------------------------------------------------------

func TestPipeline_RunsAllStepsInOrder_WhenEachReturnsContinue(t *testing.T) {
	t.Parallel()

	var log []string
	steps := []inbound.Step{
		newSpy("normalize", inbound.DecisionContinue, &log),
		newSpy("identity-bind", inbound.DecisionContinue, &log),
		newSpy("intent-recog", inbound.DecisionContinue, &log),
		newSpy("dispatch", inbound.DecisionContinue, &log),
	}

	p := inbound.NewPipeline(steps...)
	outcome, err := p.Run(context.Background(), port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-1",
		Type:        port.EventTypeMessageReceived,
	})
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	wantOrder := []string{"normalize", "identity-bind", "intent-recog", "dispatch"}
	if !equalStrings(log, wantOrder) {
		t.Fatalf("call order = %v, want %v", log, wantOrder)
	}
	// All steps should have been visited exactly once.
	for i, s := range steps {
		if got := s.(*spyStep).calls; got != 1 {
			t.Errorf("step[%d] %q calls = %d, want 1", i, s.Name(), got)
		}
	}
	if outcome.Terminal != "dispatch" {
		t.Errorf("outcome.Terminal = %q, want %q", outcome.Terminal, "dispatch")
	}
	if outcome.Decision != inbound.DecisionContinue {
		t.Errorf("outcome.Decision = %v, want Continue", outcome.Decision)
	}
}

// ---------------------------------------------------------------------------
// Skip short-circuit. Step 2 returns Skip -> later steps must
// have zero invocations and the pipeline returns cleanly.
// ---------------------------------------------------------------------------

func TestPipeline_SkipShortCircuitsSubsequentSteps(t *testing.T) {
	t.Parallel()

	var log []string
	step1 := newSpy("normalize", inbound.DecisionContinue, &log)
	step2 := newSpy("identity-bind", inbound.DecisionSkip, &log) // <-- Skip here
	step3 := newSpy("intent-recog", inbound.DecisionContinue, &log)
	step4 := newSpy("dispatch", inbound.DecisionContinue, &log)

	p := inbound.NewPipeline(step1, step2, step3, step4)
	outcome, err := p.Run(context.Background(), port.InboundEvent{EventID: "evt-skip"})
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	// Steps 1 and 2 ran; later steps must not have been called.
	if step1.calls != 1 {
		t.Errorf("step1 calls = %d, want 1", step1.calls)
	}
	if step2.calls != 1 {
		t.Errorf("step2 calls = %d, want 1", step2.calls)
	}
	if step3.calls != 0 {
		t.Errorf("step3 (after Skip) calls = %d, want 0", step3.calls)
	}
	if step4.calls != 0 {
		t.Errorf("step4 (after Skip) calls = %d, want 0", step4.calls)
	}
	if outcome.Terminal != "identity-bind" {
		t.Errorf("outcome.Terminal = %q, want %q", outcome.Terminal, "identity-bind")
	}
	if outcome.Decision != inbound.DecisionSkip {
		t.Errorf("outcome.Decision = %v, want Skip", outcome.Decision)
	}
}

// ---------------------------------------------------------------------------
// Pipeline propagates a step error verbatim and aborts further steps.
// ---------------------------------------------------------------------------

func TestPipeline_StepError_AbortsAndPropagates(t *testing.T) {
	t.Parallel()

	var log []string
	wantErr := errors.New("boom")

	step1 := newSpy("normalize", inbound.DecisionContinue, &log)
	step2 := &spyStep{name: "identity-bind", decision: inbound.DecisionContinue, err: wantErr, log: &log}
	step3 := newSpy("intent-recog", inbound.DecisionContinue, &log)

	p := inbound.NewPipeline(step1, step2, step3)
	_, err := p.Run(context.Background(), port.InboundEvent{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run err = %v, want errors.Is(%v) == true", err, wantErr)
	}
	if step3.calls != 0 {
		t.Errorf("step3 (after error) calls = %d, want 0", step3.calls)
	}
}

// ---------------------------------------------------------------------------
// equalStrings is a tiny helper kept here (rather than imported) so the test
// file declares zero dependencies beyond stdlib + the package under test.
// ---------------------------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
