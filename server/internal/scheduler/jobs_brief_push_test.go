package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeBriefPushDispatcher struct {
	calls int
	n     int
	err   error
}

func (f *fakeBriefPushDispatcher) DeliverDue(context.Context) (int, error) {
	f.calls++
	return f.n, f.err
}

func TestBriefPushDispatchJobRunsEveryThirtySecondsAndIsValid(t *testing.T) {
	spec := BriefPushDispatchJob(&fakeBriefPushDispatcher{})
	if err := spec.validate(); err != nil {
		t.Fatalf("spec invalid: %v", err)
	}
	if spec.Name != JobNameBriefPushDispatch || spec.Cadence != 30*time.Second {
		t.Fatalf("name/cadence = %q/%s, want %q every 30s", spec.Name, spec.Cadence, JobNameBriefPushDispatch)
	}
	if spec.MaxAttempts != 1 {
		t.Errorf("MaxAttempts = %d; retries belong to the delivery rows, not the job", spec.MaxAttempts)
	}
}

func TestBriefPushDispatchJobRunsTheDispatcher(t *testing.T) {
	d := &fakeBriefPushDispatcher{n: 3}
	res, err := BriefPushDispatchJob(d).Handler(context.Background(), HandlerInput{})
	if err != nil || d.calls != 1 || res.RowsAffected != 3 {
		t.Fatalf("handler = (%+v, %v) after %d calls, want 3 rows and one call", res, err, d.calls)
	}
}

func TestBriefPushDispatchJobReturnsItsErrorForARetry(t *testing.T) {
	boom := errors.New("database unavailable")
	_, err := BriefPushDispatchJob(&fakeBriefPushDispatcher{err: boom}).Handler(context.Background(), HandlerInput{})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the dispatcher's error", err)
	}
}
