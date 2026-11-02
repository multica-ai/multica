package service

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestFailTaskWithTransitionStaleGenerationSkipsRetryOverlay(t *testing.T) {
	ctx := context.Background()
	fixture := newRuntimeClaimAccessFixture(t, "public", false, true, "dispatched")
	q := db.New(fixture.pool)
	taskID := util.MustParseUUID(fixture.taskID)

	stale, err := q.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("load stale task generation: %v", err)
	}
	if !stale.DispatchedAt.Valid {
		t.Fatal("fixture task has no stale dispatched_at generation")
	}
	reclaimed, err := q.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{
		RuntimeID:         fixture.runtimeID,
		PrepareLeaseSecs:  60,
		ClaimRecoverySecs: 30,
		RuntimeStaleSecs:  RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		t.Fatalf("production reclaim query: %v", err)
	}
	if !reclaimed.DispatchedAt.Valid || reclaimed.DispatchedAt.Time.Equal(stale.DispatchedAt.Time) {
		t.Fatalf("reclaim generation = %v, stale generation = %v; want a new generation", reclaimed.DispatchedAt, stale.DispatchedAt)
	}
	if !retryEligible("agent_error.provider_network", reclaimed) {
		t.Fatal("reclaimed task is not eligible for the provider_network retry path")
	}

	builder := &stubOverlayBuilder{respIsNil: true}
	svc := &TaskService{
		Queries:      q,
		TxStarter:    fixture.pool,
		Bus:          events.New(),
		Composio:     builder,
		FeatureFlags: composioMCPAppsTestFlags(true),
	}
	_, transitioned, err := svc.FailTaskWithTransition(ctx, taskID,
		"provider unavailable", "", "", "", "agent_error.provider_network", false, "", "", stale.DispatchedAt.Time)
	if !errors.Is(err, ErrTaskClaimGenerationMismatch) {
		t.Fatalf("stale fenced failure error = %v, want ErrTaskClaimGenerationMismatch", err)
	}
	if transitioned {
		t.Fatal("stale failure transitioned the task")
	}
	if builder.calls != 0 {
		t.Fatalf("external overlay builder calls = %d, want 0 for a stale generation", builder.calls)
	}

	current, err := q.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("reload task after stale failure: %v", err)
	}
	if current.Status != "dispatched" || !current.DispatchedAt.Valid || !current.DispatchedAt.Time.Equal(reclaimed.DispatchedAt.Time) {
		t.Fatalf("task after stale failure = status %q generation %v, want dispatched generation %v", current.Status, current.DispatchedAt, reclaimed.DispatchedAt)
	}
}
