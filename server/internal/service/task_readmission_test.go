package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func readmissionFixture(t *testing.T, cap int) (*TaskService, *testutil.Fixture, string, string) {
	t.Helper()
	pool := newTaskClaimRacePool(t)
	fx := testutil.New(pool, "", "")
	id := fmt.Sprintf("readmission-%d", time.Now().UnixNano())
	fx.UserID = fx.User(t, "test owner", id+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "readmission", id)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtime := fx.Runtime(t, "runtime", testutil.Cols{"visibility": "public", "owner_id": fx.UserID})
	agent := fx.Agent(t, "same specialist", runtime, testutil.Cols{"owner_id": fx.UserID, "max_concurrent_tasks": cap})
	return NewTaskService(db.New(pool), pool, nil, events.New()), fx, agent, runtime
}

func TestTaskReadmissionConcurrentWakeHonorsProfileCap(t *testing.T) {
	svc, fx, agent, runtime := readmissionFixture(t, 2)
	var tasks []string
	for n := 0; n < 12; n++ {
		issue := fx.Issue(t, fmt.Sprintf("independent %d", n))
		tasks = append(tasks, fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue, "status": "waiting_local_directory", "context": `{"execution_capacity_released":true}`}))
	}
	var success, capacity atomic.Int32
	var wg sync.WaitGroup
	for _, id := range tasks {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := svc.StartTask(context.Background(), util.MustParseUUID(id))
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, ErrTaskExecutionCapacity) {
				capacity.Add(1)
			} else {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	if success.Load() != 2 || capacity.Load() != 10 {
		t.Fatalf("starts=%d capacity waits=%d, want 2/10", success.Load(), capacity.Load())
	}
	t.Log("12 atomic wakes: 2 starts and 10 genuine capacity waits, no oversubscription")
}

func TestTaskReadmissionFreesCapacityWithoutSameIssueDuplicate(t *testing.T) {
	svc, fx, agent, runtime := readmissionFixture(t, 1)
	ctx := context.Background()
	issue := fx.Issue(t, "one conversation")
	firstID := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue, "status": "dispatched"})
	if _, err := svc.MarkTaskWaitingLocalDirectory(ctx, util.MustParseUUID(firstID), "shared folder", true); err != nil {
		t.Fatal(err)
	}
	duplicateID := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue, "status": "queued"})
	otherID := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": fx.Issue(t, "independent issue"), "status": "queued"})

	got, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(runtime)}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || util.UUIDToString(got[0].ID) != otherID {
		t.Fatalf("independent issue did not use released capacity: %+v", got)
	}
	duplicate, err := svc.Queries.GetAgentTask(ctx, util.MustParseUUID(duplicateID))
	if err != nil || duplicate.Status != "queued" {
		t.Fatal("same-issue dedupe lost", err)
	}
	if _, err := svc.StartTask(ctx, util.MustParseUUID(firstID)); !errors.Is(err, ErrTaskExecutionCapacity) {
		t.Fatalf("wake must reacquire profile: %v", err)
	}
	if _, err := svc.CancelTaskWithReason(ctx, util.MustParseUUID(otherID), "fixture complete", "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartTask(ctx, util.MustParseUUID(firstID)); err != nil {
		t.Fatal("automatic successor could not readmit", err)
	}
}

func TestTaskReadmissionLegacyWaitKeepsReservation(t *testing.T) {
	svc, fx, agent, runtime := readmissionFixture(t, 1)
	id := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": fx.Issue(t, "legacy"), "status": "dispatched"})
	if _, err := svc.MarkTaskWaitingLocalDirectory(context.Background(), util.MustParseUUID(id), "folder"); err != nil {
		t.Fatal(err)
	}
	count, err := svc.Queries.CountRunningTasks(context.Background(), util.MustParseUUID(agent))
	if err != nil || count != 1 {
		t.Fatalf("legacy reservation count=%d error=%v", count, err)
	}
}
