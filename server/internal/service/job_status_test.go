package service

import (
	"testing"
	"time"
)

func TestStuckReasonFlagsALeaseHeldPastItsStaleTime(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	st := JobStatus{Cadence: time.Hour}
	stuck, reason := stuckReason(st, 1, now)
	if !stuck || reason == "" {
		t.Fatal("a run holding its lease past the stale time is stuck")
	}
}

func TestStuckReasonUsesThreeCadencesWithoutASuccess(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Hour)
	old := now.Add(-4 * time.Hour)

	hourly := JobStatus{Cadence: time.Hour, LastSuccessAt: &fresh}
	if stuck, _ := stuckReason(hourly, 0, now); stuck {
		t.Fatal("a success two cadences ago is not stuck")
	}
	hourly.LastSuccessAt = &old
	if stuck, reason := stuckReason(hourly, 0, now); !stuck || reason == "" {
		t.Fatal("a success four cadences ago is stuck")
	}
}

func TestStuckReasonDoesNotAccuseAJobThatNeverRan(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if stuck, _ := stuckReason(JobStatus{Cadence: time.Hour}, 0, now); stuck {
		t.Fatal("a job with no runs yet is not stuck")
	}
	failed := now.Add(-time.Minute)
	if stuck, _ := stuckReason(JobStatus{Cadence: time.Hour, LastFailedAt: &failed}, 0, now); !stuck {
		t.Fatal("a job that failed and never succeeded is stuck")
	}
}

func TestWatchedJobsHaveUniqueNamesAndPositiveCadence(t *testing.T) {
	seen := map[string]bool{}
	for _, j := range WatchedJobs {
		if seen[j.Name] {
			t.Errorf("job %q is watched twice", j.Name)
		}
		seen[j.Name] = true
		if j.Cadence <= 0 {
			t.Errorf("job %q has no cadence", j.Name)
		}
	}
}
