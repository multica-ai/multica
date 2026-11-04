package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeMetricNightly struct {
	n   int
	err error
	ran int
}

func (f *fakeMetricNightly) RunMetricNightly(context.Context) (int, error) {
	f.ran++
	return f.n, f.err
}

// TestMetricNightlyJobIsDailyAndRunsTheRunner checks the job's cadence and that
// its handler runs the runner once and reports what it wrote.
func TestMetricNightlyJobIsDailyAndRunsTheRunner(t *testing.T) {
	runner := &fakeMetricNightly{n: 42}
	spec := MetricNightlyJob(runner)
	if spec.Name != JobNameMetricNightly || spec.Cadence != 24*time.Hour {
		t.Fatalf("name %q cadence %v: want the nightly job, daily", spec.Name, spec.Cadence)
	}
	res, err := spec.Handler(context.Background(), HandlerInput{})
	if err != nil || runner.ran != 1 || res.RowsAffected != 42 {
		t.Fatalf("handler: res=%+v err=%v ran=%d", res, err, runner.ran)
	}
}

// TestMetricNightlyJobReturnsItsErrorForARetry: a failed run is retried by the
// scheduler, so the error must reach it.
func TestMetricNightlyJobReturnsItsErrorForARetry(t *testing.T) {
	boom := errors.New("boom")
	if _, err := MetricNightlyJob(&fakeMetricNightly{err: boom}).Handler(context.Background(), HandlerInput{}); !errors.Is(err, boom) {
		t.Fatalf("the runner's error must be returned, got %v", err)
	}
}
