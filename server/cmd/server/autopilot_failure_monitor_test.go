package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// pickFixtureAgent grabs the first agent in the workspace fixture. The
// integration TestMain seeds exactly one agent, so this is deterministic.
func pickFixtureAgent(t *testing.T) pgtype.UUID {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	return parseUUID(agentID)
}

// seedAutopilot creates an autopilot owned by the given creator (member or
// agent UUID + type) and registers cleanup. Status defaults to "active".
func seedAutopilot(t *testing.T, queries *db.Queries, title, creatorType string, creatorID pgtype.UUID, agentID pgtype.UUID) db.Autopilot {
	t.Helper()
	ctx := context.Background()
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		Title:         title,
		AssigneeID:    agentID,
		Status:        "active",
		ExecutionMode: "run_only",
		CreatedByType: creatorType,
		CreatedByID:   creatorID,
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		// inbox_item has no FK to autopilot, so clean both up explicitly.
		testPool.Exec(context.Background(),
			`DELETE FROM inbox_item WHERE workspace_id = $1 AND details->>'autopilot_id' = $2`,
			testWorkspaceID, util.UUIDToString(ap.ID))
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})
	return ap
}

// seedAutopilotRuns inserts n runs for the given autopilot, the first
// `failed` of which have status='failed' and the rest 'completed'. All runs
// are timestamped at `runAt` so they fall inside or outside a chosen lookback
// window deterministically.
func seedAutopilotRuns(t *testing.T, autopilotID pgtype.UUID, total, failed int, runAt time.Time) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < total; i++ {
		status := "completed"
		if i < failed {
			status = "failed"
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO autopilot_run (autopilot_id, source, status, created_at, triggered_at, completed_at)
			VALUES ($1, 'schedule', $2, $3, $3, $3)
		`, autopilotID, status, runAt); err != nil {
			t.Fatalf("seed autopilot_run: %v", err)
		}
	}
}

func reloadAutopilotStatus(t *testing.T, queries *db.Queries, id pgtype.UUID) string {
	t.Helper()
	ap, err := queries.GetAutopilot(context.Background(), id)
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	return ap.Status
}

func TestAutopilotFailureMonitor_PausesOffenderAndNotifiesCreator(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()

	cfg := failureMonitorConfig{
		Interval:  time.Hour,
		Lookback:  7 * 24 * time.Hour,
		MinRuns:   10,
		FailRatio: 0.9,
	}

	agentID := pickFixtureAgent(t)
	offender := seedAutopilot(t, queries, "Failure monitor: offender", "member", parseUUID(testUserID), agentID)
	innocent := seedAutopilot(t, queries, "Failure monitor: innocent", "member", parseUUID(testUserID), agentID)

	now := time.Now()
	// 12 runs in window, 11 failed → 91.6% > 90% and ≥10 min runs.
	seedAutopilotRuns(t, offender.ID, 12, 11, now.Add(-1*time.Hour))
	// Innocent: also lots of failures, but they fall outside the lookback.
	seedAutopilotRuns(t, innocent.ID, 12, 12, now.Add(-30*24*time.Hour))
	// Innocent: a few recent runs but below min_runs threshold.
	seedAutopilotRuns(t, innocent.ID, 5, 5, now.Add(-1*time.Hour))

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})
	var updateEvents []events.Event
	bus.Subscribe(protocol.EventAutopilotUpdated, func(e events.Event) {
		updateEvents = append(updateEvents, e)
	})

	tickAutopilotFailureMonitor(context.Background(), queries, bus, cfg)

	if got := reloadAutopilotStatus(t, queries, offender.ID); got != "paused" {
		t.Fatalf("expected offender to be paused, got %q", got)
	}
	if got := reloadAutopilotStatus(t, queries, innocent.ID); got != "active" {
		t.Fatalf("expected innocent to stay active, got %q", got)
	}

	if len(updateEvents) != 1 {
		t.Fatalf("expected 1 autopilot:updated event, got %d", len(updateEvents))
	}
	if got := updateEvents[0].Payload.(map[string]any)["reason"]; got != "auto_paused_high_failure_rate" {
		t.Fatalf("expected reason auto_paused_high_failure_rate, got %v", got)
	}

	if len(inboxEvents) != 1 {
		t.Fatalf("expected 1 inbox:new event, got %d", len(inboxEvents))
	}
	item := inboxEvents[0].Payload.(map[string]any)["item"].(map[string]any)
	if got := item["type"]; got != "autopilot_paused" {
		t.Fatalf("expected inbox type autopilot_paused, got %v", got)
	}
	if got := item["severity"]; got != "attention" {
		t.Fatalf("expected severity attention, got %v", got)
	}
	if got := item["recipient_id"]; got != testUserID {
		t.Fatalf("expected recipient %s, got %v", testUserID, got)
	}

	// Confirm the inbox item exists in the DB too.
	items := inboxItemsForRecipient(t, queries, testUserID)
	var found bool
	for _, it := range items {
		if it.Type == "autopilot_paused" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected an autopilot_paused inbox_item in DB for user %s", testUserID)
	}
}

func TestAutopilotFailureMonitor_LeavesAlreadyPausedAlone(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	cfg := failureMonitorConfig{
		Interval:  time.Hour,
		Lookback:  7 * 24 * time.Hour,
		MinRuns:   10,
		FailRatio: 0.9,
	}

	agentID := pickFixtureAgent(t)
	ap := seedAutopilot(t, queries, "Failure monitor: already paused", "member", parseUUID(testUserID), agentID)
	seedAutopilotRuns(t, ap.ID, 12, 11, time.Now().Add(-1*time.Hour))

	// Manually pause first.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot SET status = 'paused' WHERE id = $1`, ap.ID); err != nil {
		t.Fatalf("manual pause: %v", err)
	}

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	tickAutopilotFailureMonitor(context.Background(), queries, bus, cfg)

	if len(inboxEvents) != 0 {
		t.Fatalf("paused autopilots must not generate notifications, got %d", len(inboxEvents))
	}
}

func TestAutopilotFailureMonitor_AgentCreatorRoutesToOwner(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	cfg := failureMonitorConfig{
		Interval:  time.Hour,
		Lookback:  7 * 24 * time.Hour,
		MinRuns:   10,
		FailRatio: 0.9,
	}

	agentID := pickFixtureAgent(t)
	// The fixture agent's owner_id is testUserID (set in setupIntegrationTestFixture).
	ap := seedAutopilot(t, queries, "Failure monitor: agent-created", "agent", agentID, agentID)
	seedAutopilotRuns(t, ap.ID, 11, 10, time.Now().Add(-2*time.Hour))

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	tickAutopilotFailureMonitor(context.Background(), queries, bus, cfg)

	if got := reloadAutopilotStatus(t, queries, ap.ID); got != "paused" {
		t.Fatalf("expected paused, got %q", got)
	}
	if len(inboxEvents) != 1 {
		t.Fatalf("expected 1 inbox event for the agent's owner, got %d", len(inboxEvents))
	}
	item := inboxEvents[0].Payload.(map[string]any)["item"].(map[string]any)
	if got := item["recipient_id"]; got != testUserID {
		t.Fatalf("expected recipient owner %s, got %v", testUserID, got)
	}
	if got := item["recipient_type"]; got != "member" {
		t.Fatalf("expected member recipient_type, got %v", got)
	}
}

func TestAutopilotFailureMonitor_BelowThresholdNoOp(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	cfg := failureMonitorConfig{
		Interval:  time.Hour,
		Lookback:  7 * 24 * time.Hour,
		MinRuns:   10,
		FailRatio: 0.9,
	}

	agentID := pickFixtureAgent(t)
	ap := seedAutopilot(t, queries, "Failure monitor: under threshold", "member", parseUUID(testUserID), agentID)
	// 12 total, 5 failed → 41.6% < 90%.
	seedAutopilotRuns(t, ap.ID, 12, 5, time.Now().Add(-1*time.Hour))

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	tickAutopilotFailureMonitor(context.Background(), queries, bus, cfg)

	if got := reloadAutopilotStatus(t, queries, ap.ID); got != "active" {
		t.Fatalf("expected active, got %q", got)
	}
	if len(inboxEvents) != 0 {
		t.Fatalf("expected no inbox events, got %d", len(inboxEvents))
	}
}
