package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestKagentExpandedRuntimeGC(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	for _, enrolled := range []bool{true, false} {
		t.Run(map[bool]string{true: "orphan-tombstone", false: "unenrolled"}[enrolled], func(t *testing.T) {
			ctx := context.Background()
			ws := createRuntimeGCFixtureWorkspace(t, ctx, "kagent-guard")
			f := testutil.New(testPool, ws, testUserID)
			rt := f.Runtime(t, "GC guard", testutil.Cols{"provider": "claude", "status": "offline", "last_seen_at": testutil.Raw("now() - interval '8 days'")})
			a := f.Agent(t, "Archived GC agent", rt, testutil.Cols{"archived_at": testutil.Raw("now()")})
			task := f.Task(t, a, testutil.Cols{"runtime_id": rt, "status": "completed", "completed_at": testutil.Raw("now()")})
			if enrolled {
				orphan := uuid.NewString()
				f.InsertNoID(t, "kagent_enrollment", testutil.Cols{"workspace_id": ws, "runtime_id": rt, "agent_id": orphan, "desired_state": "absent", "phase": "deleted", "applied_generation": 1}, "agent_id=$1", orphan)
			}
			result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(rt))
			if enrolled {
				if result.deleted || !errors.Is(err, service.ErrRuntimeKagentEnrollment) {
					t.Fatalf("deleted=%v err=%v", result.deleted, err)
				}
			} else if err != nil || !result.deleted {
				t.Fatalf("deleted=%v err=%v", result.deleted, err)
			}
			var exists bool
			f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM agent_runtime WHERE id=$1)`, rt).Scan(&exists)
			if exists != enrolled {
				t.Fatalf("runtime retained=%v enrolled=%v", exists, enrolled)
			}
			var agentBound, taskBound bool
			f.QueryRow(t, `SELECT runtime_id IS NOT NULL FROM agent WHERE id=$1`, a).Scan(&agentBound)
			f.QueryRow(t, `SELECT runtime_id IS NOT NULL FROM agent_task_queue WHERE id=$1`, task).Scan(&taskBound)
			if agentBound != enrolled || taskBound != enrolled {
				t.Fatalf("agent bound=%v task bound=%v enrolled=%v", agentBound, taskBound, enrolled)
			}
		})
	}
}
