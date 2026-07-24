package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression for the child-done squad routing race: archival can commit after
// the handler's active-squad check but before task creation. Task creation must
// serialize with archival so an archive that wins the row lock also wins the
// enqueue decision.
func TestCreateAgentTaskWithSquadGuard_ConcurrentArchiveWins(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	suffix := time.Now().UnixNano()
	var squadID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		SELECT workspace_id, $2, id, owner_id
		FROM agent
		WHERE id = $1
		RETURNING id
	`, util.UUIDToString(fx.agentID), fmt.Sprintf("archive-race-%d", suffix)).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	archiveTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin archive transaction: %v", err)
	}
	defer archiveTx.Rollback(ctx)
	if _, err := archiveTx.Exec(ctx, `
		UPDATE squad SET archived_at = now(), updated_at = now() WHERE id = $1
	`, squadID); err != nil {
		t.Fatalf("archive squad: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := svc.createAgentTaskWithSquadGuard(ctx, db.CreateAgentTaskParams{
			AgentID:   fx.agentID,
			RuntimeID: fx.runtimeID,
			IssueID:   fx.issueID,
			Priority:  0,
			SquadID:   util.MustParseUUID(squadID),
			IsLeaderTask: pgtype.Bool{
				Bool:  true,
				Valid: true,
			},
		})
		result <- err
	}()
	<-started

	select {
	case err := <-result:
		t.Fatalf("enqueue returned before the archive transaction committed: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: the active-squad guard is waiting on the archive row lock.
	}

	if err := archiveTx.Commit(ctx); err != nil {
		t.Fatalf("commit archive transaction: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, errSquadUnavailableForTask) {
			t.Fatalf("expected archived-squad error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue stayed blocked after archive commit")
	}

	var taskCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue WHERE squad_id = $1
	`, squadID).Scan(&taskCount); err != nil {
		t.Fatalf("count squad tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no task for archived squad, got %d", taskCount)
	}
}
