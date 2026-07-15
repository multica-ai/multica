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
}

func (f *fakeBlockingStore) BlockingEvalsPassed(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	f.called = true
	return f.passed, f.err
}

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
