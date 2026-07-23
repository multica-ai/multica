package evals

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeBaseGate struct {
	passed bool
	err    error
}

func (f fakeBaseGate) EvaluateCheckGate(context.Context, string, string, any) (bool, error) {
	return f.passed, f.err
}

type fakeBlockingStore struct {
	passed bool
	err    error
	called bool
	phase  string
}

func (f *fakeBlockingStore) BlockingEvalsPassed(_ context.Context, _, _ uuid.UUID, phase string) (bool, error) {
	f.called = true
	f.phase = phase
	return f.passed, f.err
}

type fakePhaseGateConfig struct{ phase string }

func (f fakePhaseGateConfig) EvalBindingPhase() string { return f.phase }

func TestGateEvaluatorStopsWhenBaseGateFails(t *testing.T) {
	store := &fakeBlockingStore{passed: true}
	gate := NewGateEvaluator(fakeBaseGate{passed: false}, store)
	passed, err := gate.EvaluateCheckGate(context.Background(), uuid.NewString(), uuid.NewString(), nil)
	if err != nil || passed {
		t.Fatalf("expected failed base gate without error, got passed=%v err=%v", passed, err)
	}
	if store.called {
		t.Fatal("blocking eval store must not run before the base gate passes")
	}
}

func TestGateEvaluatorFailsClosedWhenBlockingEvalFails(t *testing.T) {
	store := &fakeBlockingStore{passed: false}
	gate := NewGateEvaluator(fakeBaseGate{passed: true}, store)
	passed, err := gate.EvaluateCheckGate(context.Background(), uuid.NewString(), uuid.NewString(), nil)
	if err != nil || passed {
		t.Fatalf("expected blocking eval to fail closed, got passed=%v err=%v", passed, err)
	}
}

func TestGateEvaluatorPropagatesBlockingEvalError(t *testing.T) {
	want := errors.New("database unavailable")
	store := &fakeBlockingStore{err: want}
	gate := NewGateEvaluator(fakeBaseGate{passed: true}, store)
	passed, err := gate.EvaluateCheckGate(context.Background(), uuid.NewString(), uuid.NewString(), nil)
	if passed || !errors.Is(err, want) {
		t.Fatalf("expected blocking eval error, got passed=%v err=%v", passed, err)
	}
}

func TestGateEvaluatorUsesTheGateBindingPhase(t *testing.T) {
	store := &fakeBlockingStore{passed: true}
	gate := NewGateEvaluator(fakeBaseGate{passed: true}, store)
	passed, err := gate.EvaluateCheckGate(
		context.Background(), uuid.NewString(), uuid.NewString(), fakePhaseGateConfig{phase: "plan"},
	)
	if err != nil || !passed {
		t.Fatalf("expected phase-scoped blocking eval to pass, got passed=%v err=%v", passed, err)
	}
	if store.phase != "plan" {
		t.Fatalf("blocking eval store received phase %q, want plan", store.phase)
	}
}

func TestGateEvaluatorDefaultsInvalidOrLegacyPhaseToDelivery(t *testing.T) {
	for _, value := range []any{nil, map[string]any{"eval_phase": "bogus"}} {
		store := &fakeBlockingStore{passed: true}
		gate := NewGateEvaluator(fakeBaseGate{passed: true}, store)
		if _, err := gate.EvaluateCheckGate(context.Background(), uuid.NewString(), uuid.NewString(), value); err != nil {
			t.Fatalf("evaluate legacy gate: %v", err)
		}
		if store.phase != "delivery" {
			t.Fatalf("legacy/invalid phase resolved to %q, want delivery", store.phase)
		}
	}
}
