package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProposalExpirer struct {
	calls int
	n     int
	err   error
}

func (f *fakeProposalExpirer) ExpireDueProposals(context.Context) (int, error) {
	f.calls++
	return f.n, f.err
}

func TestProposalExpiryJobIsDailyAndValid(t *testing.T) {
	spec := ProposalExpiryJob(&fakeProposalExpirer{})
	if err := spec.validate(); err != nil {
		t.Fatalf("spec invalid: %v", err)
	}
	if spec.Name != JobNameProposalExpiry || spec.Cadence != 24*time.Hour {
		t.Fatalf("name/cadence = %q/%s, want %q daily", spec.Name, spec.Cadence, JobNameProposalExpiry)
	}
	if spec.CatchUpMode != CatchUpLatestOnly {
		t.Errorf("a missed day is not replayed: catch-up = %s, want latest_only", spec.CatchUpMode)
	}
}

func TestProposalExpiryJobRunsTheExpirer(t *testing.T) {
	expirer := &fakeProposalExpirer{n: 2}
	res, err := ProposalExpiryJob(expirer).Handler(context.Background(), HandlerInput{})
	if err != nil || expirer.calls != 1 || res.RowsAffected != 2 {
		t.Fatalf("handler = (%+v, %v) after %d calls, want 2 rows and one call", res, err, expirer.calls)
	}
}

func TestProposalExpiryJobReturnsItsErrorForARetry(t *testing.T) {
	boom := errors.New("database unavailable")
	_, err := ProposalExpiryJob(&fakeProposalExpirer{err: boom}).Handler(context.Background(), HandlerInput{})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the expirer's error so the scheduler retries", err)
	}
}
