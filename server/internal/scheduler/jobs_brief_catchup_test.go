package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeBriefCatchupRunner struct {
	calls int
	n     int
	err   error
}

func (f *fakeBriefCatchupRunner) CatchUpBriefs(context.Context) (int, error) {
	f.calls++
	return f.n, f.err
}

func TestBriefCatchupJobIsHourlyAndValid(t *testing.T) {
	spec := BriefCatchupJob(&fakeBriefCatchupRunner{})
	if err := spec.validate(); err != nil {
		t.Fatalf("spec invalid: %v", err)
	}
	if spec.Name != JobNameBriefCatchup || spec.Cadence != time.Hour {
		t.Fatalf("name/cadence = %q/%s, want %q hourly", spec.Name, spec.Cadence, JobNameBriefCatchup)
	}
	if spec.CatchUpMode != CatchUpLatestOnly {
		t.Errorf("a missed hour is not replayed: catch-up = %s, want latest_only", spec.CatchUpMode)
	}
}

func TestBriefCatchupJobRunsThePassAndReportsIt(t *testing.T) {
	runner := &fakeBriefCatchupRunner{n: 1}
	res, err := BriefCatchupJob(runner).Handler(context.Background(), HandlerInput{})
	if err != nil || runner.calls != 1 || res.RowsAffected != 1 {
		t.Fatalf("handler = (%+v, %v) after %d calls, want one fired brief and one call", res, err, runner.calls)
	}
}

func TestBriefCatchupJobReturnsItsErrorForARetry(t *testing.T) {
	boom := errors.New("database unavailable")
	_, err := BriefCatchupJob(&fakeBriefCatchupRunner{err: boom}).Handler(context.Background(), HandlerInput{})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the runner's error so the scheduler retries", err)
	}
}
