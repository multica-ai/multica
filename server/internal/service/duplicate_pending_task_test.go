package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestIsDuplicatePendingTaskErr locks the driver-code detection (#5914): only a
// 23505 on either pending-task index name used during the v1/v2 rolling-deploy
// window is a benign duplicate-pending-task race. A different unique constraint
// (e.g. the agent name) or a plain error must not be mistaken for it, and the
// wrapped sentinel must remain detectable via errors.Is by callers. This runs
// without a database.
func TestIsDuplicatePendingTaskErr(t *testing.T) {
	for _, indexName := range []string{
		"idx_one_pending_task_per_issue_agent",
		"idx_one_pending_task_per_issue_agent_v2",
	} {
		dup := &pgconn.PgError{Code: "23505", ConstraintName: indexName}
		if !isDuplicatePendingTaskErr(dup) {
			t.Errorf("expected pending-task index %q to be recognized", indexName)
		}
	}
	if isDuplicatePendingTaskErr(&pgconn.PgError{Code: "23505", ConstraintName: "agent_workspace_name_unique"}) {
		t.Fatal("a different unique constraint must not be treated as a duplicate pending task")
	}
	if isDuplicatePendingTaskErr(&pgconn.PgError{Code: "40001", ConstraintName: "idx_one_pending_task_per_issue_agent_v2"}) {
		t.Fatal("a non-unique-violation SQLSTATE must not be treated as a duplicate pending task")
	}
	if isDuplicatePendingTaskErr(errors.New("boom")) {
		t.Fatal("a non-pg error must not be treated as a duplicate pending task")
	}

	wrapped := fmt.Errorf("%w: duplicate pending task", ErrDuplicatePendingTask)
	if !errors.Is(wrapped, ErrDuplicatePendingTask) {
		t.Fatal("the wrapped sentinel must stay detectable via errors.Is")
	}
}

// TestEnqueueTaskForMentionCoalescesDuplicatePendingTask is the service-level
// regression for #5914: a second mention enqueue for the same (issue, agent)
// that loses the race to an existing pending task must return the typed
// ErrDuplicatePendingTask sentinel — not a raw create-task error carrying the
// Postgres constraint name — and must leave exactly one pending task behind.
func TestEnqueueTaskForMentionCoalescesDuplicatePendingTask(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	issueStruct := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	// First mention creates the pending task.
	if _, err := svc.EnqueueTaskForMention(ctx, issueStruct, util.MustParseUUID(agentID), pgtype.UUID{}); err != nil {
		t.Fatalf("first EnqueueTaskForMention: %v", err)
	}

	// Second mention for the same (issue, agent) collides on the unique index.
	_, err := svc.EnqueueTaskForMention(ctx, issueStruct, util.MustParseUUID(agentID), pgtype.UUID{})
	if !errors.Is(err, ErrDuplicatePendingTask) {
		t.Fatalf("second EnqueueTaskForMention: err = %v, want ErrDuplicatePendingTask", err)
	}
	// The returned error must NOT carry the raw Postgres constraint name or
	// SQLSTATE — those used to leak into upper-layer warning logs (#5914).
	for _, leak := range []string{"idx_one_pending_task_per_issue_agent", "23505", "SQLSTATE", "duplicate key"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("duplicate error leaked %q: %v", leak, err)
		}
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched')`,
		issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	if n != 1 {
		t.Fatalf("pending task count = %d, want exactly 1", n)
	}
}
