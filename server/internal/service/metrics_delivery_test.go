package service

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestBuiltinDeliveryMetricsFixtures checks delivery.issues_done,
// delivery.cycle_time_days and delivery.rework_rate on squad issues finished
// yesterday:
//   - a clean delivery, created two days before it was done: 2 days;
//   - a delivery created four days before it was done and reopened yesterday:
//     4 days, and rework;
//   - a legacy done issue with no done_at, counted at updated_at: created a day
//     and fourteen hours before, so 1.5833 days;
//   - an issue done a week ago, which the window does not count.
//
// Done is 3, the mean cycle time is (2 + 4 + 1.5833) / 3 days, and rework is 1 in
// 3, so 33.33%.
func TestBuiltinDeliveryMetricsFixtures(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	yesterday := f.now.AddDate(0, 0, -1)
	at := func(h int) time.Time { return yesterday.Add(time.Duration(h) * time.Hour) }

	f.Issue(t, "clean delivery", testutil.Cols{
		"assignee_type": "squad", "assignee_id": f.squad, "status": "done",
		"created_at": yesterday.AddDate(0, 0, -2).Add(10 * time.Hour), "done_at": at(10), "updated_at": at(10),
	})
	f.Issue(t, "reworked delivery", testutil.Cols{
		"assignee_type": "squad", "assignee_id": f.squad, "status": "done",
		"created_at": yesterday.AddDate(0, 0, -4).Add(10 * time.Hour), "done_at": at(10),
		"reopen_count": 1, "last_reopened_at": at(12), "updated_at": at(12),
	})
	f.Issue(t, "legacy done", testutil.Cols{
		"assignee_type": "squad", "assignee_id": f.squad, "status": "done",
		"created_at": yesterday.AddDate(0, 0, -1), "updated_at": at(14),
	})
	f.Issue(t, "old done", testutil.Cols{
		"assignee_type": "squad", "assignee_id": f.squad, "status": "done",
		"created_at": yesterday.AddDate(0, 0, -20), "updated_at": yesterday.AddDate(0, 0, -7),
	})

	if _, err := f.metricsService().RollupBuiltinMetrics(ctx, f.ws, f.now); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	squad := f.readingsOf(t, ScopeSquad, f.squad)
	wantCycle := (2.0 + 4.0 + (1 + 14.0/24)) / 3
	for key, want := range map[string]float64{
		"delivery.issues_done":     3,
		"delivery.cycle_time_days": wantCycle,
		"delivery.rework_rate":     100.0 / 3,
	} {
		if got, ok := squad[key]; !ok || got-want > 1e-6 || want-got > 1e-6 {
			t.Errorf("squad %s = %v (present %v), want %v", key, got, ok, want)
		}
	}
}

// TestBuiltinDeliveryMetricsWithoutDoneWriteNothing checks that a squad with no
// done issue in the window writes no cycle time and no rework rate, rather than
// zero.
func TestBuiltinDeliveryMetricsWithoutDoneWriteNothing(t *testing.T) {
	f := newGovFixture(t)
	f.Issue(t, "still open", testutil.Cols{"assignee_type": "squad", "assignee_id": f.squad, "status": "todo"})
	if _, err := f.metricsService().RollupBuiltinMetrics(context.Background(), f.ws, f.now); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	squad := f.readingsOf(t, ScopeSquad, f.squad)
	for _, key := range []string{"delivery.cycle_time_days", "delivery.rework_rate"} {
		if _, ok := squad[key]; ok {
			t.Errorf("squad %s written with no done issue behind it", key)
		}
	}
}

// TestIssueDoneTransitionTrigger drives the status trigger (migration 881) through
// the status writes a delivery takes: finishing sets done_at; leaving done for an
// open status counts a reopen; finishing again moves done_at without counting
// again; cancelling a done issue is not a reopen.
func TestIssueDoneTransitionTrigger(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	// A new workspace gets the built-in status catalog; the trigger reads it to
	// tell a done or closed status from an open one.
	if err := f.q.SeedIssueStatusEntries(ctx, f.ws); err != nil {
		t.Fatalf("seed statuses: %v", err)
	}
	id := f.Issue(t, "trigger issue", testutil.Cols{"status": "todo"})

	setStatus := func(status string) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, `UPDATE issue SET status = $2 WHERE id = $1`, id, status); err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
	}
	read := func() (done bool, reopens int, reopened bool) {
		t.Helper()
		var doneAt, lastReopened *time.Time
		if err := f.pool.QueryRow(ctx, `SELECT done_at, reopen_count, last_reopened_at FROM issue WHERE id = $1`, id).Scan(&doneAt, &reopens, &lastReopened); err != nil {
			t.Fatalf("read issue: %v", err)
		}
		return doneAt != nil, reopens, lastReopened != nil
	}

	setStatus("in_progress")
	if done, n, re := read(); done || n != 0 || re {
		t.Fatalf("open issue: done=%v reopens=%d reopened=%v, want none", done, n, re)
	}
	setStatus("done")
	if done, n, _ := read(); !done || n != 0 {
		t.Fatalf("first done: done_at set=%v reopens=%d, want set and 0", done, n)
	}
	setStatus("in_progress")
	if _, n, re := read(); n != 1 || !re {
		t.Fatalf("reopen: reopens=%d reopened=%v, want 1 and true", n, re)
	}
	setStatus("done")
	if _, n, _ := read(); n != 1 {
		t.Fatalf("finishing again: reopens=%d, want still 1", n)
	}
	setStatus("cancelled")
	if _, n, _ := read(); n != 1 {
		t.Fatalf("done to cancelled counted as a reopen: reopens=%d, want 1", n)
	}
}
