package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestShutdownSetupFailureRetryClassification(t *testing.T) {
	parent := db.AgentTaskQueue{Attempt: 1, MaxAttempts: 2, IssueID: pgtype.UUID{Valid: true}}
	for _, tc := range []struct {
		reason string
		want   bool
	}{
		{"agent_error.unknown", false},
		{"environment_prepare_failed", false},
		{"runtime_recovery", true},
		{"cancelled", false},
		{"cleanup_unconfirmed", false},
		{"local_directory_error", false},
	} {
		got := retryEligible(tc.reason, parent)
		t.Logf("callback reason=%s attempt=1 max_attempts=2 retryEligible=%v", tc.reason, got)
		if got != tc.want {
			t.Errorf("retryEligible(%s) = %v, want %v", tc.reason, got, tc.want)
		}
	}
}

func TestShutdownRecoveryRespectsTaskState(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	fx := dbfx.New(pool, workspaceID, userID)
	var runtimeID string
	fx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id=$1`, agentID).Scan(&runtimeID)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	for _, tc := range []struct {
		status, reason string
		wantTransition bool
	}{
		{"waiting_local_directory", "", true},
		{"cancelled", "cancelled", false},
		{"completed", "", false},
		{"failed", "cleanup_unconfirmed", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			cols := dbfx.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": tc.status,
				"attempt": 1, "max_attempts": 2,
				"failure_reason": tc.reason, "error": "original diagnostic", "session_id": "original-session",
			}
			if !tc.wantTransition {
				cols["completed_at"] = dbfx.Raw("now()")
			}
			taskID := fx.Task(t, agentID, cols)
			got, transitioned, err := svc.FailTaskWithTransition(ctx, util.MustParseUUID(taskID),
				"daemon shut down while waiting for local_directory", "late-session", "", "", "runtime_recovery", false, "", "")
			if err != nil {
				t.Fatalf("late shutdown failure: %v", err)
			}
			wantStatus, wantReason, wantDiagnostic, wantSession := tc.status, tc.reason, "original diagnostic", "original-session"
			if tc.wantTransition {
				wantStatus, wantReason = "failed", "runtime_recovery"
				wantDiagnostic, wantSession = "daemon shut down while waiting for local_directory", "late-session"
			}
			if transitioned != tc.wantTransition || got == nil || got.Status != wantStatus {
				t.Fatalf("shutdown transition: task=%+v transitioned=%v, want status=%s transitioned=%v", got, transitioned, wantStatus, tc.wantTransition)
			}
			var status string
			var reason, diagnostic, session pgtype.Text
			fx.QueryRow(t, `SELECT status, failure_reason, error, session_id FROM agent_task_queue WHERE id=$1`, taskID).
				Scan(&status, &reason, &diagnostic, &session)
			if status != wantStatus || reason.String != wantReason || diagnostic.String != wantDiagnostic || session.String != wantSession {
				t.Fatalf("unexpected persisted task: status=%q reason=%q error=%q session=%q", status, reason.String, diagnostic.String, session.String)
			}
			wantRetries := 0
			if tc.wantTransition {
				wantRetries = 1
				fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE parent_task_id=$1`, taskID)
				var childStatus string
				var attempt int
				fx.QueryRow(t, `SELECT status, attempt FROM agent_task_queue WHERE parent_task_id=$1`, taskID).Scan(&childStatus, &attempt)
				if childStatus != "queued" || attempt != 2 {
					t.Fatalf("waiting task retry: status=%s attempt=%d, want queued attempt 2", childStatus, attempt)
				}
			}
			if count := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, taskID); count != wantRetries {
				t.Fatalf("shutdown callback created %d retries, want %d", count, wantRetries)
			}
		})
	}
}
