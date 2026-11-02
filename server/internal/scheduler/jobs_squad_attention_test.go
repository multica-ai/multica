package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeAttentionRecomputer struct {
	calls int
	n     int
	err   error
}

func (f *fakeAttentionRecomputer) RecomputeSquadAttention(context.Context) (int, error) {
	f.calls++
	return f.n, f.err
}

func TestSquadAttentionJobIsDailyAndValid(t *testing.T) {
	spec := SquadAttentionJob(&fakeAttentionRecomputer{})
	if err := spec.validate(); err != nil {
		t.Fatalf("spec invalid: %v", err)
	}
	if spec.Name != JobNameSquadAttention || spec.Cadence != 24*time.Hour {
		t.Fatalf("name/cadence = %q/%s, want %q daily", spec.Name, spec.Cadence, JobNameSquadAttention)
	}
	if spec.CatchUpMode != CatchUpLatestOnly {
		t.Errorf("a missed night is not replayed: catch-up = %s, want latest_only", spec.CatchUpMode)
	}
}

func TestSquadAttentionJobRunsTheRecomputer(t *testing.T) {
	recomputer := &fakeAttentionRecomputer{n: 4}
	res, err := SquadAttentionJob(recomputer).Handler(context.Background(), HandlerInput{})
	if err != nil || recomputer.calls != 1 || res.RowsAffected != 4 {
		t.Fatalf("handler = (%+v, %v) after %d calls, want 4 squads and one call", res, err, recomputer.calls)
	}
}

func TestSquadAttentionJobReturnsItsErrorForARetry(t *testing.T) {
	boom := errors.New("database unavailable")
	_, err := SquadAttentionJob(&fakeAttentionRecomputer{err: boom}).Handler(context.Background(), HandlerInput{})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the recomputer's error so the scheduler retries", err)
	}
}
