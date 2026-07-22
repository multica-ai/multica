package service

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedCappedAutopilot inserts an active run_only autopilot owned by the fixture
// user/agent and returns the row. Cap is set separately by the caller so the
// same helper covers the unlimited and capped cases.
func seedCappedAutopilot(t *testing.T, q *db.Queries, workspaceID, userID, agentID string, cap pgtype.Int4) db.Autopilot {
	t.Helper()
	ctx := context.Background()
	ap, err := q.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:       util.MustParseUUID(workspaceID),
		Title:             "cap-test",
		AssigneeType:      "agent",
		AssigneeID:        util.MustParseUUID(agentID),
		Status:            "active",
		ExecutionMode:     "run_only",
		CreatedByType:     "member",
		CreatedByID:       util.MustParseUUID(userID),
		MaxConcurrentRuns: cap,
	})
	if err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	return ap
}

// insertAutopilotRunWithStatus inserts a run row in the given (terminal or
// in-flight) status with no downstream linkage, so the concurrency-cap count
// can be exercised against every status value.
func insertAutopilotRunWithStatus(t *testing.T, q *db.Queries, autopilotID pgtype.UUID, source, status string) db.AutopilotRun {
	t.Helper()
	ctx := context.Background()
	run, err := q.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID: autopilotID,
		Source:      source,
		Status:      status,
	})
	if err != nil {
		t.Fatalf("seed run %s: %v", status, err)
	}
	return run
}

// TestShouldSkipDispatch_ConcurrencyCap is the WS-750 admission test: a capped
// autopilot skips (reason concurrency_cap) once in-flight runs (issue_created /
// running) reach max_concurrent_runs, admits below the cap, ignores terminal
// runs, and is unlimited when the cap is NULL.
func TestShouldSkipDispatch_ConcurrencyCap(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	svc := &AutopilotService{Queries: q}

	t.Run("null cap admits with no active runs", func(t *testing.T) {
		ap := seedCappedAutopilot(t, q, workspaceID, userID, agentID, pgtype.Int4{})
		if reason, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{}); skip {
			t.Fatalf("unlimited cap should admit, got skip reason=%q code=%q", reason, code)
		}
	})

	t.Run("cap admits when active below max", func(t *testing.T) {
		ap := seedCappedAutopilot(t, q, workspaceID, userID, agentID, pgtype.Int4{Int32: 2, Valid: true})
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "issue_created")
		if reason, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{}); skip {
			t.Fatalf("1 of 2 active should admit, got skip reason=%q code=%q", reason, code)
		}
	})

	t.Run("cap skips when active reaches max", func(t *testing.T) {
		ap := seedCappedAutopilot(t, q, workspaceID, userID, agentID, pgtype.Int4{Int32: 2, Valid: true})
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "issue_created")
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "running")
		reason, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{})
		if !skip {
			t.Fatalf("2 of 2 active should skip")
		}
		if code != dispatch.ReasonConcurrencyCap {
			t.Fatalf("skip code = %q, want concurrency_cap", code)
		}
		if !strings.Contains(reason, "concurrency cap") {
			t.Fatalf("skip reason = %q, want concurrency cap phrasing", reason)
		}
	})

	t.Run("cap of 1 skips on first active run", func(t *testing.T) {
		ap := seedCappedAutopilot(t, q, workspaceID, userID, agentID, pgtype.Int4{Int32: 1, Valid: true})
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "running")
		if _, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{}); !skip || code != dispatch.ReasonConcurrencyCap {
			t.Fatalf("1 of 1 active should skip with concurrency_cap, got skip=%v code=%q", skip, code)
		}
	})

	t.Run("terminal runs do not count toward cap", func(t *testing.T) {
		ap := seedCappedAutopilot(t, q, workspaceID, userID, agentID, pgtype.Int4{Int32: 2, Valid: true})
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "completed")
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "failed")
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "skipped")
		if reason, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{}); skip {
			t.Fatalf("terminal runs should not count, got skip reason=%q code=%q", reason, code)
		}
	})

	t.Run("skipped runs do not count but running does", func(t *testing.T) {
		ap := seedCappedAutopilot(t, q, workspaceID, userID, agentID, pgtype.Int4{Int32: 1, Valid: true})
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "skipped")
		if reason, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{}); skip {
			t.Fatalf("skipped-only should admit, got skip reason=%q code=%q", reason, code)
		}
		insertAutopilotRunWithStatus(t, q, ap.ID, "manual", "running")
		if _, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{}); !skip || code != dispatch.ReasonConcurrencyCap {
			t.Fatalf("1 skipped + 1 running should skip with concurrency_cap, got skip=%v code=%q", skip, code)
		}
	})
}

// TestDispatchAutopilotManual_ConcurrencyCapSkip is the WS-750 end-to-end test:
// a capped autopilot that is already at its cap records a `skipped` run with
// reason concurrency_cap (via recordSkippedRun) instead of stacking another
// dispatch, and returns the typed code to the manual caller.
func TestDispatchAutopilotManual_ConcurrencyCapSkip(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	svc := &AutopilotService{Queries: q, Bus: events.New()}

	ap := seedCappedAutopilot(t, q, workspaceID, userID, agentID, pgtype.Int4{Int32: 1, Valid: true})
	// One in-flight run already occupies the cap.
	insertAutopilotRunWithStatus(t, q, ap.ID, "schedule", "running")

	run, code, err := svc.DispatchAutopilotManual(ctx, ap, pgtype.UUID{}, nil, util.MustParseUUID(userID))
	if err != nil {
		t.Fatalf("DispatchAutopilotManual: %v", err)
	}
	if code != dispatch.ReasonConcurrencyCap {
		t.Fatalf("reason code = %q, want concurrency_cap", code)
	}
	if run.Status != "skipped" {
		t.Fatalf("run status = %q, want skipped", run.Status)
	}
	if !run.FailureReason.Valid || !strings.Contains(run.FailureReason.String, "concurrency cap") {
		t.Fatalf("failure_reason = %+v, want concurrency cap phrasing", run.FailureReason)
	}

	// The pre-existing in-flight run is untouched - no orphan, no double-dispatch.
	active, err := q.CountActiveAutopilotRuns(ctx, ap.ID)
	if err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Fatalf("active run count = %d, want 1 (skipped run must not count)", active)
	}
}
