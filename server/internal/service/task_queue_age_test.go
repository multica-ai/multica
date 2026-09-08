package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestTaskQueueAgeSurvivesRestartAndRetry(t *testing.T) {
	svc, fx, agent, runtime := readmissionFixture(t, 2)
	ctx := context.Background()
	id := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": fx.Issue(t, "parked before restart"), "status": "waiting_local_directory", "max_attempts": 2, "created_at": testutil.Raw("now() - interval '2 hours'"), "context": `{"execution_capacity_released":true}`})
	failed, err := svc.RecoverOrphanedTasksForRuntime(ctx, util.MustParseUUID(runtime))
	if err != nil || len(failed) != 1 {
		t.Fatalf("restart recovery: %d %v", len(failed), err)
	}
	if _, err := svc.StartTask(ctx, util.MustParseUUID(id)); err == nil {
		t.Fatal("stale waiter launched after recovery")
	}
	child, err := svc.MaybeRetryFailedTask(ctx, failed[0])
	if err != nil || child == nil {
		t.Fatalf("recovery retry missing: %v", err)
	}
	if child.ID == failed[0].ID || !child.OriginalQueuedAt.Valid || !child.OriginalQueuedAt.Time.Equal(failed[0].CreatedAt.Time) {
		t.Fatal("retry lost original fairness age/attempt identity")
	}
	if child.CreatedAt.Time.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("retry's own creation timestamp was overwritten")
	}
	if again, err := svc.MaybeRetryFailedTask(ctx, failed[0]); err != nil || again != nil {
		t.Fatalf("duplicate retry created: %+v %v", again, err)
	}
	// New service instance, same durable queue: aging survives controller restart.
	fresh := NewTaskService(svc.Queries, svc.TxStarter, nil, svc.Bus)
	claimed, err := fresh.ClaimTasksForRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(runtime)}, 2)
	if err != nil || len(claimed) != 1 || claimed[0].ID != child.ID {
		t.Fatalf("retry did not automatically readmit: %+v %v", claimed, err)
	}
	t.Log("restart fenced the old waiter; exactly one fresh retry retained original queue age and was reclaimed")
}

func TestTaskQueueAgingPreservesEmergencyPrecedence(t *testing.T) {
	svc, fx, agent, runtime := readmissionFixture(t, 3)
	ctx := context.Background()
	makeTask := func(title string, priority int, age string) string {
		return fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": fx.Issue(t, title), "priority": priority, "status": "queued", "created_at": testutil.Raw(age)})
	}
	aged := makeTask("aged", 0, "now() - interval '2 hours'")
	emergency := makeTask("emergency", 4, "now()")
	high := makeTask("high", 3, "now()")
	got, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(runtime)}, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{emergency, aged, high} {
		if len(got) <= i || util.UUIDToString(got[i].ID) != id {
			t.Fatalf("priority/age order: %+v", got)
		}
	}
}
