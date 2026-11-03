package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRecoveryExhaustionHonorsAgentFailureRoute: the failure notification
// written when delegated recovery gives up follows the recipient's
// agent_failures route like any other task_failed.
func TestRecoveryExhaustionHonorsAgentFailureRoute(t *testing.T) {
	for _, route := range []string{"activity", "muted"} {
		t.Run(route, func(t *testing.T) {
			f, svc := seedDelegatedFailureFixture(t)
			ctx := context.Background()
			fx := testutil.New(f.pool, f.workspaceID, f.userID)
			fx.Insert(t, "notification_preference", testutil.Cols{
				"workspace_id": f.workspaceID, "user_id": f.userID,
				"preferences": fmt.Sprintf(`{"agent_failures":%q}`, route),
			})
			failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
			fx.Exec(t, `UPDATE agent_task_queue SET failure_reason = 'agent_error.process_failure',
				error = 'worker exited', completed_at = now() WHERE id = $1`, failedID)
			failed, err := svc.Queries.GetAgentTask(ctx, failedID)
			if err != nil {
				t.Fatal(err)
			}
			if handled, err := svc.recoverDelegatedTaskFailure(ctx, failed); err != nil || !handled {
				t.Fatalf("initial recovery: handled=%v err=%v", handled, err)
			}
			for attempt := 1; attempt <= delegatedFailureRecoveryMaxTaskAttempts; attempt++ {
				var currentTaskID pgtype.UUID
				if err := f.pool.QueryRow(ctx, `SELECT id FROM agent_task_queue
					WHERE trigger_evidence_kind = 'delegated_failure' AND trigger_evidence_ref_id = $1
					AND status = 'queued' ORDER BY created_at DESC, id DESC LIMIT 1`, failedID).Scan(&currentTaskID); err != nil {
					t.Fatal(err)
				}
				fx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', completed_at = now(),
					failure_reason = 'queued_expired', error = 'task expired in queue',
					delivered_comment_ids = '{}' WHERE id = $1`, currentTaskID)
				result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
				if err != nil {
					t.Fatal(err)
				}
				if attempt == delegatedFailureRecoveryMaxTaskAttempts && result.Exhausted != 1 {
					t.Fatalf("final recovery result = %+v, want exhausted", result)
				}
			}
			items, err := svc.Queries.ListInboxItems(ctx, db.ListInboxItemsParams{
				WorkspaceID: util.MustParseUUID(f.workspaceID), RecipientType: "member", RecipientID: util.MustParseUUID(f.userID),
			})
			if err != nil {
				t.Fatal(err)
			}
			failures := 0
			for _, item := range items {
				if item.Type != "task_failed" {
					continue
				}
				failures++
				if route == "muted" {
					t.Errorf("agent_failures=muted still creates task_failed notification")
				}
				if route == "activity" && inbox.ItemSection(item.Section, item.Type) != "activity" {
					t.Errorf("agent_failures=activity produced section %q", inbox.ItemSection(item.Section, item.Type))
				}
			}
			if route == "activity" && failures != 1 {
				t.Fatalf("failure notifications = %d, want 1", failures)
			}
		})
	}
}
