package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Seed a persisted rule and input from a server that supported PR reviews.
func persistedReviewWakeup(t *testing.T, event string) (principalFixture, *IssueWakeupService, db.IssueWakeup) {
	t.Helper()
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Handle external review",
		Condition: condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "in_review"})})
	f.Exec(t, "UPDATE issue_wakeup SET condition=$2,event_types='{}' WHERE id=$1", w.ID,
		condition(t, map[string]any{"type": "pull_request", "event": event}))
	var err error
	w, err = f.q.LocklessWakeup(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.q.RecordWakeupReceipt(context.Background(), db.RecordWakeupReceiptParams{
		ID: dbid.NewV7(), WakeupID: w.ID, Revision: w.Revision, EventKey: "review:1", EventType: wakeupConditionEventType,
		Payload: []byte(`{"observed":{"pull_requests":[{"review_id":1}]}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, s, w
}

func TestRetiredPRReviewWakeupsRejectCreation(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	for _, event := range []string{"reviewed", "approved", "changes_requested"} {
		_, err := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, WakeupInput{
			AgentID: agent, Kind: "event", Instruction: "Handle external review",
			Condition: condition(t, map[string]any{"type": "pull_request", "event": event}),
		})
		if !errors.Is(err, ErrWakeupInput) {
			t.Errorf("%s creation: %v, want ErrWakeupInput", event, err)
		}
	}
}

func TestRetiredPRReviewWakeupsDiscardInputsAndCancelQueuedRuns(t *testing.T) {
	for _, event := range []string{"reviewed", "approved", "changes_requested"} {
		t.Run(event, func(t *testing.T) {
			f, s, w := persistedReviewWakeup(t, event)
			queued := wakeWaitingRun(t, f, w.IssueID, util.UUIDToString(w.AgentID), f.UserID)
			f.Exec(t, "UPDATE agent_task_queue SET context=jsonb_build_object('wakeup_id',$2::text,'wakeup_revision',$3::bigint) WHERE id=$1", queued, util.UUIDToString(w.ID), w.Revision)
			got := wakeTick(t, f, s, w.ID)
			if got.Enabled || !got.DisabledAt.Valid || !got.LastError.Valid {
				t.Fatalf("retired rule is still active: enabled=%t disabled=%t error=%+v", got.Enabled, got.DisabledAt.Valid, got.LastError)
			}
			if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
				t.Fatalf("retired rule retained %d inputs", n)
			}
			task, err := f.q.GetAgentTask(context.Background(), parseTestUUID(t, queued))
			if err != nil || task.Status != "cancelled" || wakeRuns(t, f, w.ID) != 1 {
				t.Fatalf("queued review run was not withdrawn: status=%q err=%v", task.Status, err)
			}
		})
	}
}

func TestRetiredPRReviewWakeupsCannotJoinOrClaimBeforeSchedulerCleanup(t *testing.T) {
	for _, event := range []string{"reviewed", "approved", "changes_requested"} {
		t.Run(event, func(t *testing.T) {
			f, s, w := persistedReviewWakeup(t, event)
			waiting := wakeWaitingRun(t, f, w.IssueID, util.UUIDToString(w.AgentID), f.UserID)
			if notes := wakeClaim(t, f, s, waiting); notes != "" {
				t.Fatalf("retired review input joined an ordinary run: %s", notes)
			}
			task, err := f.q.GetAgentTask(context.Background(), parseTestUUID(t, waiting))
			if err != nil {
				t.Fatal(err)
			}
			task.Context, err = json.Marshal(map[string]any{"wakeup_id": util.UUIDToString(w.ID), "wakeup_revision": w.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CheckClaim(context.Background(), task); !errors.Is(err, ErrWakeupForbidden) {
				t.Fatalf("retired review run claim: %v, want ErrWakeupForbidden", err)
			}
		})
	}
}

func TestRetiredPRReviewWakeupsDropPreviouslyJoinedInputs(t *testing.T) {
	for _, busy := range []bool{false, true} {
		name := "available rule"
		if busy {
			name = "busy rule"
		}
		t.Run(name, func(t *testing.T) {
			f, s, w := persistedReviewWakeup(t, "approved")
			waiting := wakeWaitingRun(t, f, w.IssueID, util.UUIDToString(w.AgentID), f.UserID)
			// Reserve the input as a supported rule, then simulate the old server's review condition.
			f.Exec(t, "UPDATE issue_wakeup SET condition=$2 WHERE id=$1", w.ID, condition(t, map[string]any{"type": "pull_request", "event": "merged"}))
			if notes := wakeClaim(t, f, s, waiting); notes == "" {
				t.Fatal("input was not reserved before retirement")
			}
			f.Exec(t, "UPDATE issue_wakeup SET condition=$2 WHERE id=$1", w.ID, condition(t, map[string]any{"type": "pull_request", "event": "approved"}))
			if busy {
				tx, err := f.Pool.Begin(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				if _, err := tx.Exec(context.Background(), "SELECT id FROM issue_wakeup WHERE id=$1 FOR UPDATE", w.ID); err != nil {
					t.Fatal(err)
				}
			}
			if notes := wakeClaim(t, f, s, waiting); notes != "" {
				t.Fatalf("retired input survived a repeated claim: %s", notes)
			}
		})
	}
}
