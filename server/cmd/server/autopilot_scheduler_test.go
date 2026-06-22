package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestClaimDueScheduleTriggersReturnsScheduledFireAt(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	ap := seedAutopilot(t, queries, "Scheduler claim scheduled fire time", "member", parseUUID(testUserID), pickFixtureAgent(t))
	fireAt := time.Date(2020, 1, 2, 10, 0, 0, 0, time.UTC)
	trigger := createScheduleTrigger(t, queries, ap.ID, fireAt, "0 10 * * *")

	claimed, err := queries.ClaimDueScheduleTriggers(ctx)
	if err != nil {
		t.Fatalf("ClaimDueScheduleTriggers: %v", err)
	}

	var got *db.ClaimDueScheduleTriggersRow
	for i := range claimed {
		if claimed[i].ID == trigger.ID {
			got = &claimed[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("claimed triggers did not include test trigger %v", trigger.ID)
	}
	if !got.ScheduledFireAt.Valid || !got.ScheduledFireAt.Time.Equal(fireAt) {
		t.Fatalf("expected scheduled_fire_at %s, got %+v", fireAt, got.ScheduledFireAt)
	}
	if !got.ClaimedAt.Valid {
		t.Fatal("expected claimed_at to be set")
	}
	if got.NextRunAt.Valid {
		t.Fatalf("expected claimed trigger next_run_at to be cleared, got %s", got.NextRunAt.Time)
	}
}

func TestAdvanceNextRunUsesClaimTime(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	ap := seedAutopilot(t, queries, "Scheduler advance from occurrence", "member", parseUUID(testUserID), pickFixtureAgent(t))
	fireAt := time.Date(2099, 1, 2, 10, 0, 0, 0, time.UTC)
	trigger := createScheduleTrigger(t, queries, ap.ID, fireAt, "0 10 * * *")

	advanceNextRun(ctx, queries, db.ClaimDueScheduleTriggersRow{
		ID:              trigger.ID,
		CronExpression:  pgtype.Text{String: "0 10 * * *", Valid: true},
		Timezone:        pgtype.Text{String: "UTC", Valid: true},
		ScheduledFireAt: pgtype.Timestamptz{Time: fireAt, Valid: true},
		ClaimedAt:       pgtype.Timestamptz{Time: fireAt, Valid: true},
	})

	updated, err := queries.GetAutopilotTrigger(ctx, trigger.ID)
	if err != nil {
		t.Fatalf("GetAutopilotTrigger: %v", err)
	}
	want := time.Date(2099, 1, 3, 10, 0, 0, 0, time.UTC)
	if !updated.NextRunAt.Valid || !updated.NextRunAt.Time.Equal(want) {
		t.Fatalf("expected next_run_at %s, got %+v", want, updated.NextRunAt)
	}
}

func TestAdvanceNextRunCollapsesMissedOccurrencesAtClaimTime(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	ap := seedAutopilot(t, queries, "Scheduler advance skips missed occurrences", "member", parseUUID(testUserID), pickFixtureAgent(t))
	fireAt := time.Date(2020, 1, 2, 10, 0, 0, 0, time.UTC)
	claimTime := fireAt.Add(2 * time.Hour)
	trigger := createScheduleTrigger(t, queries, ap.ID, fireAt, "*/5 * * * *")

	advanceNextRun(ctx, queries, db.ClaimDueScheduleTriggersRow{
		ID:              trigger.ID,
		CronExpression:  pgtype.Text{String: "*/5 * * * *", Valid: true},
		Timezone:        pgtype.Text{String: "UTC", Valid: true},
		ScheduledFireAt: pgtype.Timestamptz{Time: fireAt, Valid: true},
		ClaimedAt:       pgtype.Timestamptz{Time: claimTime, Valid: true},
	})

	updated, err := queries.GetAutopilotTrigger(ctx, trigger.ID)
	if err != nil {
		t.Fatalf("GetAutopilotTrigger: %v", err)
	}
	want := claimTime.Add(5 * time.Minute)
	if !updated.NextRunAt.Valid || !updated.NextRunAt.Time.Equal(want) {
		t.Fatalf("expected next_run_at %s, got %+v", want, updated.NextRunAt)
	}
}

func TestScheduledAutopilotDispatchIsIdempotentPerOccurrence(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	ap := seedAutopilot(t, queries, "Scheduled dispatch occurrence idempotency", "member", parseUUID(testUserID), pickFixtureAgent(t))
	fireAt := time.Date(2099, 1, 2, 10, 0, 0, 0, time.UTC)
	trigger := createScheduleTrigger(t, queries, ap.ID, fireAt, "0 10 * * *")
	scheduledFireAt := pgtype.Timestamptz{Time: fireAt, Valid: true}

	run, err := autopilotSvc.DispatchScheduledAutopilot(ctx, ap, trigger.ID, scheduledFireAt)
	if err != nil {
		t.Fatalf("first DispatchScheduledAutopilot: %v", err)
	}
	t.Cleanup(func() {
		if run.TaskID.Valid {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, run.TaskID)
		}
	})
	if !run.ScheduledFireAt.Valid || !run.ScheduledFireAt.Time.Equal(fireAt) {
		t.Fatalf("expected run scheduled_fire_at %s, got %+v", fireAt, run.ScheduledFireAt)
	}

	if _, err := autopilotSvc.DispatchScheduledAutopilot(ctx, ap, trigger.ID, scheduledFireAt); err == nil {
		t.Fatal("expected duplicate scheduled occurrence to fail")
	} else if !strings.Contains(err.Error(), "create run") {
		t.Fatalf("expected duplicate to fail while creating run, got %v", err)
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM autopilot_run
		WHERE trigger_id = $1
		  AND source = 'schedule'
		  AND scheduled_fire_at = $2
	`, trigger.ID, fireAt).Scan(&count); err != nil {
		t.Fatalf("count scheduled runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one run for the scheduled occurrence, got %d", count)
	}
}

func createScheduleTrigger(t *testing.T, queries *db.Queries, autopilotID pgtype.UUID, nextRunAt time.Time, cronExpr string) db.AutopilotTrigger {
	t.Helper()
	trigger, err := queries.CreateAutopilotTrigger(context.Background(), db.CreateAutopilotTriggerParams{
		AutopilotID:    autopilotID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: cronExpr, Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
		NextRunAt:      pgtype.Timestamptz{Time: nextRunAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}
	return trigger
}
