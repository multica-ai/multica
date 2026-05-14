package inbound_test

// Red-phase tests for the inbound dedup step.
//
// Dedup idempotency: given a mocked TryRecordInboundEvent,
// the first invocation for a given event_id reports inserted=true and the
// pipeline continues; the second invocation reports inserted=false and the
// pipeline returns Skip without invoking the rest of the step chain.
//
// These tests reference symbols that do not yet exist (inbound.DedupStore,
// inbound.NewDedupStep, the (provider, event_id) interface contract on the
// store) and must fail to compile until the Green phase implements them.

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// fakeDedupStore captures the (provider, eventID) pairs the dedup step asks
// it to record and replays caller-configured responses. The two-element
// slice models "first call: insert; second call: collide" exactly as
// QA TC-inb-2 prescribes.
// ---------------------------------------------------------------------------

type fakeDedupStore struct {
	calls     []dedupCall
	processed []dedupCall
	failed    []struct {
		dedupCall
		LastError string
	}

	// responses is consumed in order. If the dedup step calls the store
	// more times than there are responses, the test fails with a clear
	// "ran out of fixtures" message via the helper next() below.
	responses []dedupResp

	// failMarkErr, when non-nil, causes MarkInboundEventFailed to return
	// this error instead of nil — used to exercise Pipeline.finalize error
	// joining.
	failMarkErr error
}

type dedupCall struct {
	Provider string
	EventID  string
}

type dedupResp struct {
	Inserted bool
	Err      error
}

func (f *fakeDedupStore) TryRecordInboundEvent(_ context.Context, provider, _ string, eventID string) (bool, error) {
	f.calls = append(f.calls, dedupCall{Provider: provider, EventID: eventID})
	if len(f.responses) == 0 {
		return false, errors.New("fakeDedupStore: ran out of fixtures (test bug)")
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return r.Inserted, r.Err
}

func (f *fakeDedupStore) MarkInboundEventProcessed(_ context.Context, provider, eventID string) error {
	f.processed = append(f.processed, dedupCall{Provider: provider, EventID: eventID})
	return nil
}

func (f *fakeDedupStore) MarkInboundEventFailed(_ context.Context, provider, eventID, lastError string) error {
	f.failed = append(f.failed, struct {
		dedupCall
		LastError string
	}{dedupCall: dedupCall{Provider: provider, EventID: eventID}, LastError: lastError})
	return f.failMarkErr
}

// ---------------------------------------------------------------------------
// TC-inb-2 (a): first delivery → store reports inserted=true → step returns
// Continue and the rest of the pipeline executes.
// ---------------------------------------------------------------------------

func TestDedupStep_EmptyEventID_ReturnsError(t *testing.T) {
	t.Parallel()

	store := &fakeDedupStore{responses: []dedupResp{{Inserted: true}}}
	step := inbound.NewDedupStep(store)

	_, _, err := step.Run(context.Background(), port.InboundEvent{ChannelName: "feishu", EventID: ""})
	if err == nil {
		t.Fatal("expected error for empty event_id, got nil")
	}

	_, _, err = step.Run(context.Background(), port.InboundEvent{ChannelName: "", EventID: "evt-001"})
	if err == nil {
		t.Fatal("expected error for empty channel_name, got nil")
	}
}

func TestDedupStep_FirstDelivery_ReturnsContinueAndCallsStore(t *testing.T) {
	t.Parallel()

	store := &fakeDedupStore{
		responses: []dedupResp{{Inserted: true}},
	}
	step := inbound.NewDedupStep(store)

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-001",
	}
	_, decision, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if decision != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", decision)
	}
	if len(store.calls) != 1 {
		t.Fatalf("store calls = %d, want 1", len(store.calls))
	}
	got := store.calls[0]
	if got.Provider != "feishu" || got.EventID != "evt-001" {
		t.Errorf("store received (%q,%q), want (feishu,evt-001)", got.Provider, got.EventID)
	}
}

// ---------------------------------------------------------------------------
// TC-inb-2 (b): second delivery for the same event_id → store reports
// inserted=false → step returns Skip → the pipeline must not progress.
// ---------------------------------------------------------------------------

func TestDedupStep_DuplicateDelivery_ReturnsSkip(t *testing.T) {
	t.Parallel()

	store := &fakeDedupStore{
		responses: []dedupResp{
			{Inserted: true},  // first delivery: inserted
			{Inserted: false}, // replay: collision -> Skip
		},
	}
	step := inbound.NewDedupStep(store)

	evt := port.InboundEvent{ChannelName: "feishu", EventID: "evt-dup"}

	if _, d1, err := step.Run(context.Background(), evt); err != nil || d1 != inbound.DecisionContinue {
		t.Fatalf("first call decision = %v err = %v, want Continue/nil", d1, err)
	}

	_, d2, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if d2 != inbound.DecisionSkip {
		t.Errorf("second call decision = %v, want Skip", d2)
	}

	if len(store.calls) != 2 {
		t.Fatalf("store calls = %d, want 2", len(store.calls))
	}
}

// ---------------------------------------------------------------------------
// TC-inb-2 (c): integration with Pipeline — when the dedup step returns
// Skip, the configured downstream step (identity-bind onwards) MUST NOT be
// invoked. This is the property the QA row actually pins; we keep the unit
// flavour above for fast feedback and add this end-to-end assertion so a
// future refactor that splits dedup into its own pipeline stage is caught.
// ---------------------------------------------------------------------------

func TestPipeline_DedupSkipPreventsDownstreamSteps(t *testing.T) {
	t.Parallel()

	store := &fakeDedupStore{
		responses: []dedupResp{
			{Inserted: true},
			{Inserted: false},
		},
	}
	dedup := inbound.NewDedupStep(store)

	var log []string
	identityBind := newSpy("identity-bind", inbound.DecisionContinue, &log)
	intentRecog := newSpy("intent-recog", inbound.DecisionContinue, &log)
	dispatch := newSpy("dispatch", inbound.DecisionContinue, &log)
	reply := newSpy("reply", inbound.DecisionContinue, &log)

	p := inbound.NewPipeline(dedup, identityBind, intentRecog, dispatch, reply)

	evt := port.InboundEvent{ChannelName: "feishu", EventID: "evt-loop"}

	// First delivery — full pipeline runs.
	out1, err := p.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if out1.Decision != inbound.DecisionContinue {
		t.Errorf("first run decision = %v, want Continue", out1.Decision)
	}
	if identityBind.calls != 1 || intentRecog.calls != 1 || dispatch.calls != 1 || reply.calls != 1 {
		t.Errorf("first run downstream calls = (%d,%d,%d,%d), want all 1",
			identityBind.calls, intentRecog.calls, dispatch.calls, reply.calls)
	}

	// Second delivery (same event_id) — dedup short-circuits everything.
	out2, err := p.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if out2.Decision != inbound.DecisionSkip {
		t.Errorf("second run decision = %v, want Skip", out2.Decision)
	}
	// Counts must be unchanged from after the first run.
	if identityBind.calls != 1 || intentRecog.calls != 1 || dispatch.calls != 1 || reply.calls != 1 {
		t.Errorf("after dedup-skip, downstream calls = (%d,%d,%d,%d), want all still 1 (no re-entry)",
			identityBind.calls, intentRecog.calls, dispatch.calls, reply.calls)
	}
	if len(store.processed) != 1 {
		t.Fatalf("processed marks = %d, want 1; duplicate skip must not mark processing events as processed", len(store.processed))
	}
}

// ---------------------------------------------------------------------------
// Defensive guard: store error is propagated and pipeline aborts.
// ---------------------------------------------------------------------------

func TestDedupStep_StoreError_Propagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("conn reset")
	store := &fakeDedupStore{responses: []dedupResp{{Err: wantErr}}}
	step := inbound.NewDedupStep(store)

	_, _, err := step.Run(context.Background(), port.InboundEvent{ChannelName: "feishu", EventID: "evt-err"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want errors.Is(%v) == true", err, wantErr)
	}
}

func TestPipeline_DedupFinalizer_MarksProcessedOnSuccess(t *testing.T) {
	t.Parallel()

	store := &fakeDedupStore{responses: []dedupResp{{Inserted: true}}}
	p := inbound.NewPipeline(
		inbound.NewDedupStep(store),
		newSpy("dispatch", inbound.DecisionContinue, &[]string{}),
	)

	_, err := p.Run(context.Background(), port.InboundEvent{ChannelName: "feishu", EventID: "evt-ok"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(store.processed) != 1 {
		t.Fatalf("processed marks = %d, want 1", len(store.processed))
	}
	if store.processed[0] != (dedupCall{Provider: "feishu", EventID: "evt-ok"}) {
		t.Fatalf("processed mark = %+v", store.processed[0])
	}
}

func TestPipeline_DedupFinalizer_MarksFailedOnError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("dispatch failed")
	store := &fakeDedupStore{responses: []dedupResp{{Inserted: true}}}
	var log []string
	p := inbound.NewPipeline(
		inbound.NewDedupStep(store),
		&spyStep{name: "dispatch", decision: inbound.DecisionContinue, err: wantErr, log: &log},
	)

	_, err := p.Run(context.Background(), port.InboundEvent{ChannelName: "feishu", EventID: "evt-fail"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run err = %v, want %v", err, wantErr)
	}
	if len(store.failed) != 1 {
		t.Fatalf("failed marks = %d, want 1", len(store.failed))
	}
	if store.failed[0].Provider != "feishu" || store.failed[0].EventID != "evt-fail" {
		t.Fatalf("failed mark = %+v", store.failed[0])
	}
	if store.failed[0].LastError == "" {
		t.Fatal("failed mark should keep last error")
	}
}

func TestPipeline_DedupFinalizer_ReturnsBothErrors(t *testing.T) {
	t.Parallel()

	stepErr := errors.New("dispatch failed")
	markErr := errors.New("dedup mark failed")
	store := &fakeDedupStore{
		responses:   []dedupResp{{Inserted: true}},
		failMarkErr: markErr,
	}
	var log []string
	p := inbound.NewPipeline(
		inbound.NewDedupStep(store),
		&spyStep{name: "dispatch", decision: inbound.DecisionContinue, err: stepErr, log: &log},
	)

	_, err := p.Run(context.Background(), port.InboundEvent{ChannelName: "feishu", EventID: "evt-both"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, stepErr) {
		t.Errorf("errors.Is(err, stepErr) = false, want true; err = %v", err)
	}
	if !errors.Is(err, markErr) {
		t.Errorf("errors.Is(err, markErr) = false, want true; err = %v", err)
	}
	if len(store.failed) != 1 {
		t.Fatalf("failed marks = %d, want 1", len(store.failed))
	}
}
