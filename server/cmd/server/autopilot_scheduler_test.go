package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAdvanceNextRun(t *testing.T) {
	if testPool == nil {
		t.Skip("integration DB not available")
	}

	ctx := context.Background()
	queries := db.New(testPool)
	plannedAt := time.Date(2020, time.January, 1, 23, 0, 0, 0, time.UTC)

	cases := []struct {
		name            string
		mutateClaim     func(*db.ClaimDueScheduleTriggersRow)
		expectNextValid bool
		expectNext      time.Time
	}{
		{
			name:            "uses claimed planned_at as reference",
			mutateClaim:     nil,
			expectNextValid: true,
			expectNext:      time.Date(2020, time.January, 2, 23, 0, 0, 0, time.UTC),
		},
		{
			name: "planned_at invalid leaves next_run_at NULL",
			mutateClaim: func(c *db.ClaimDueScheduleTriggersRow) {
				c.PlannedAt.Valid = false
			},
			expectNextValid: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID := pickFixtureAgent(t)
			ap := seedAutopilot(t, queries, "Scheduler planned_at regression "+tc.name, "member", parseUUID(testUserID), agentID)

			trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
				AutopilotID:    ap.ID,
				Kind:           "schedule",
				Enabled:        true,
				CronExpression: pgtype.Text{String: "0 23 * * *", Valid: true},
				Timezone:       pgtype.Text{String: "UTC", Valid: true},
				NextRunAt:      pgtype.Timestamptz{Time: plannedAt, Valid: true},
			})
			if err != nil {
				t.Fatalf("CreateAutopilotTrigger: %v", err)
			}

			claims, err := queries.ClaimDueScheduleTriggers(ctx)
			if err != nil {
				t.Fatalf("ClaimDueScheduleTriggers: %v", err)
			}

			var claimed db.ClaimDueScheduleTriggersRow
			found := false
			for _, c := range claims {
				if c.ID == trigger.ID {
					claimed = c
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected trigger %v to be claimed; got %d claimed rows", trigger.ID, len(claims))
			}
			if !claimed.PlannedAt.Valid {
				t.Fatal("expected planned_at to be valid on claimed row")
			}
			if !claimed.PlannedAt.Time.Equal(plannedAt) {
				t.Fatalf("expected planned_at %v, got %v", plannedAt, claimed.PlannedAt.Time)
			}

			if tc.mutateClaim != nil {
				tc.mutateClaim(&claimed)
			}

			advanceNextRun(ctx, queries, claimed)

			updated, err := queries.GetAutopilotTrigger(ctx, trigger.ID)
			if err != nil {
				t.Fatalf("GetAutopilotTrigger: %v", err)
			}

			if tc.expectNextValid {
				if !updated.NextRunAt.Valid {
					t.Fatal("expected next_run_at to be set after advanceNextRun")
				}
				if !updated.NextRunAt.Time.Equal(tc.expectNext) {
					t.Fatalf("expected next_run_at %v, got %v", tc.expectNext, updated.NextRunAt.Time)
				}
				return
			}

			if updated.NextRunAt.Valid {
				t.Fatalf("expected next_run_at to remain NULL, got %v", updated.NextRunAt.Time)
			}
		})
	}
}
