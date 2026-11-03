package scheduler

import (
	"context"
	"testing"
	"time"
)

type recordingLibrarian struct {
	job  string
	when time.Time
}

func (r *recordingLibrarian) RunLibrarian(_ context.Context, job string, now time.Time) (int, error) {
	r.job, r.when = job, now
	return 3, nil
}

func TestKnowledgeLibrarianJobRunsItsRoutineAtThePlanTime(t *testing.T) {
	runner := &recordingLibrarian{}
	spec := KnowledgeLibrarianJob(runner, JobNameKnowledgeAudit, "audit", 30*24*time.Hour)
	if spec.Name != JobNameKnowledgeAudit || spec.Cadence != 30*24*time.Hour || spec.CatchUpMode != CatchUpLatestOnly {
		t.Fatalf("spec = %+v", spec)
	}
	plan := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	res, err := spec.Handler(context.Background(), HandlerInput{PlanTime: plan})
	if err != nil {
		t.Fatal(err)
	}
	if runner.job != "audit" || !runner.when.Equal(plan) || res.RowsAffected != 3 {
		t.Fatalf("runner got %q at %v, result %+v", runner.job, runner.when, res)
	}
}
