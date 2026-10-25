package service

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestDispatchReworkReceiptFailureRollsBackQueueAndEvent proves that the
// queue row is not observable when its durable idempotency receipt cannot be
// recorded. The trigger fails the real receipt INSERT after CreateAgentTask
// has already executed in the same transaction.
func TestDispatchReworkReceiptFailureRollsBackQueueAndEvent(t *testing.T) {
	svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	ctx := context.Background()
	const functionName = "test_rework_dispatch_receipt_failure"
	const triggerName = "test_rework_dispatch_receipt_failure_trigger"

	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION test_rework_dispatch_receipt_failure()
		RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.operation_key = 'receipt-rollback' THEN
				RAISE EXCEPTION 'forced rework receipt failure';
			END IF;
			RETURN NEW;
		END;
		$$`); err != nil {
		t.Fatalf("create receipt failure function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TRIGGER test_rework_dispatch_receipt_failure_trigger
		BEFORE INSERT ON issue_rework_dispatch_receipt
		FOR EACH ROW EXECUTE FUNCTION test_rework_dispatch_receipt_failure()`); err != nil {
		t.Fatalf("create receipt failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS "+triggerName+" ON issue_rework_dispatch_receipt")
		_, _ = pool.Exec(context.Background(), "DROP FUNCTION IF EXISTS "+functionName+"()")
	})

	queuedEvents := 0
	svc.Bus.Subscribe(protocol.EventTaskQueued, func(events.Event) { queuedEvents++ })
	req := reworkRequest(agentID, "receipt-rollback", "the receipt write must fail")
	req.SourceTaskID = reworkSourceTask(t, pool, agentID, runtimeID, issueID)
	req.ActorUserID = util.MustParseUUID(creatorID)
	workspaceID := reworkWorkspaceID(t, svc, issueID)
	if _, err := svc.DispatchRework(ctx, util.MustParseUUID(issueID), workspaceID, req, nil); err == nil || !strings.Contains(err.Error(), "forced rework receipt failure") {
		t.Fatalf("DispatchRework error = %v, want forced receipt failure", err)
	}

	var tasks, receipts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND id <> $2`, issueID, req.SourceTaskID).Scan(&tasks); err != nil {
		t.Fatalf("count queue rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_rework_dispatch_receipt WHERE issue_id = $1`, issueID).Scan(&receipts); err != nil {
		t.Fatalf("count receipt rows: %v", err)
	}
	if tasks != 0 || receipts != 0 {
		t.Fatalf("failed receipt write committed tasks=%d receipts=%d", tasks, receipts)
	}
	if queuedEvents != 0 {
		t.Fatalf("failed receipt write emitted %d queued events", queuedEvents)
	}
}
