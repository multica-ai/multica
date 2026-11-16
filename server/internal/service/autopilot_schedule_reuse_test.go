package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestAutopilotScheduleQuotaReuseWaitsForDownstream(t *testing.T) {
	f := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 5)
	ctx := context.Background()
	fx := testutil.New(f.pool, util.UUIDToString(f.workspaceID), util.UUIDToString(f.publisherID))
	triggerID := fx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id": f.autopilotID, "kind": "schedule", "enabled": true, "cron_expression": "* * * * *",
		"execution_limit": 1, "created_by_type": "member", "created_by_id": f.publisherID,
	})
	planned := pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Second), Valid: true}
	params := f.createRunArgs
	params.TriggerID = util.MustParseUUID(triggerID)
	params.PlannedAt = planned
	params.Source = "schedule"
	const key = "schedule-racing-admission"
	first, reused, err := f.service.createAutopilotRunWithQuota(ctx, f.workspaceID, "schedule", key, params)
	if err != nil || reused {
		t.Fatalf("first admission: reused=%v %v", reused, err)
	}
	ap, err := f.queries.GetAutopilot(ctx, f.autopilotID)
	if err != nil {
		t.Fatal(err)
	}
	// Model the losing scheduler's quota admission after its earlier lookup
	// found no run: it sees the winner's persisted but unlinked partial row.
	run, _, err := f.service.dispatchAutopilot(ctx, ap, params.TriggerID, "schedule", nil, planned, pgtype.UUID{}, pgtype.UUID{}, key)
	if err == nil || run != nil {
		t.Fatalf("partial run incorrectly returned as accepted: %#v %v", run, err)
	}
	stored, err := f.queries.GetAutopilotRun(ctx, first.ID)
	if err != nil || stored.TaskID.Valid {
		t.Fatalf("pending winner altered: %#v %v", stored, err)
	}
	if n := fx.Count(t, `SELECT execution_count FROM autopilot_trigger WHERE id=$1`, triggerID); n != 0 {
		t.Fatal("pending admission consumed finite count")
	}
}
