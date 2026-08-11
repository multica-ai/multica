package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// TestExecutionLedgerProducedOnIssueEnqueue is the P1 producer acceptance test:
// EnqueueTaskForIssue must write the agent_task_queue row AND the
// execution_ledger row in ONE transaction, stamping the frozen execution_id /
// memoryhub_run_id onto the queue row so the claim-time gate commit and the
// ledger agree.
//
// Before the P1 wiring, EnqueueTaskForIssue created only the queue row; the
// ledger row and the execution_id stamp were absent (fail). After the wiring,
// both exist with a consistent identity (pass).
func TestExecutionLedgerProducedOnIssueEnqueue(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	// The queue row must carry the stamped execution identity.
	var execID pgtype.UUID
	var runID pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT execution_id, memoryhub_run_id FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&execID, &runID); err != nil {
		t.Fatalf("read queue row execution identity: %v", err)
	}
	if !execID.Valid {
		t.Fatal("queue row has no execution_id: P1 producer did not stamp the execution snapshot")
	}
	if !runID.Valid || runID.String == "" {
		t.Fatal("queue row has no memoryhub_run_id: P1 producer did not stamp the run identity")
	}

	// The ledger row must exist in state queued with the SAME execution_id and
	// the frozen idempotency key sha256("enqueue|"+task_id).
	ledger, err := q.GetExecutionLedgerByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetExecutionLedgerByTaskID: %v (P1 producer did not insert the ledger row)", err)
	}
	if ledger.ExecutionID.Bytes != execID.Bytes {
		t.Fatalf("ledger execution_id %s != queue execution_id %s", util.UUIDToString(ledger.ExecutionID), util.UUIDToString(execID))
	}
	if ledger.State != "queued" {
		t.Fatalf("ledger state = %s, want queued", ledger.State)
	}
	if ledger.IdempotencyKey != executionIdempotencyKey("enqueue", task.ID) {
		t.Fatalf("ledger idempotency_key = %q, want sha256(\"enqueue|%s\")", ledger.IdempotencyKey, util.UUIDToString(task.ID))
	}
	if ledger.ScopeKind != "workspace" {
		t.Fatalf("ledger scope_kind = %s, want workspace (projectless)", ledger.ScopeKind)
	}
}
