package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Characterizes #8551, not a fairness guarantee: strict priority lets every
// newer priority-3 arrival overtake an eligible priority-0 run. Replace this
// expectation when an aging policy is agreed. Fixed timestamps avoid sleeps.
func TestClaimPriorityRepeatedOvertaking(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%t", batch), func(t *testing.T) {
			ctx := context.Background()
			pool := sharedTestPool(t)
			bootstrap := testutil.New(pool, "", "")
			suffix := time.Now().UnixNano()
			userID := bootstrap.User(t, "Priority test", fmt.Sprintf("priority-%d@example.com", suffix))
			workspaceID := bootstrap.Workspace(t, "Priority test", fmt.Sprintf("priority-%d", suffix))
			fx := testutil.New(pool, workspaceID, userID)
			runtimeID := fx.Runtime(t, "Shared runtime")
			lowAgent := fx.Agent(t, "Low priority", runtimeID, testutil.Cols{"max_concurrent_tasks": 6})
			highAgent := fx.Agent(t, "High priority", runtimeID, testutil.Cols{"max_concurrent_tasks": 6})
			base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			low := fx.Task(t, lowAgent, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": fx.Issue(t, "Old eligible run"),
				"priority": 0, "created_at": base,
			})
			svc := NewTaskService(db.New(pool), pool, nil, events.New())
			rid := util.MustParseUUID(runtimeID)
			claim := func() string {
				t.Helper()
				if batch {
					// The daemon has one free execution slot and requests one run.
					got, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rid}, 1)
					if err != nil || len(got) != 1 {
						t.Fatalf("claim: got %d runs, error %v", len(got), err)
					}
					return util.UUIDToString(got[0].ID)
				}
				got, err := svc.ClaimTaskForRuntime(ctx, rid)
				if err != nil || got == nil {
					t.Fatalf("claim: got %v, error %v", got, err)
				}
				return util.UUIDToString(got.ID)
			}
			for arrival := 1; arrival <= 8; arrival++ {
				high := fx.Task(t, highAgent, testutil.Cols{
					"runtime_id": runtimeID, "issue_id": fx.Issue(t, fmt.Sprintf("Arrival %d", arrival)),
					"priority": 3, "created_at": base.Add(time.Duration(arrival) * time.Hour),
				})
				if got := claim(); got != high {
					t.Fatalf("arrival %d: claimed %s, want newer priority-3 run %s", arrival, got, high)
				}
				var status string
				fx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id = $1", low).Scan(&status)
				if status != "queued" {
					t.Fatalf("old run status = %s, want queued", status)
				}
				// Complete before freeing the sole daemon slot for the next claim.
				fx.Exec(t, "UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1", high)
			}
			// Once arrivals stop, the old run is claimable: it was not blocked by
			// agent capacity, authorization, runtime health, or issue serialization.
			if got := claim(); got != low {
				t.Fatalf("after arrivals stop: claimed %s, want old run %s", got, low)
			}
		})
	}
}
