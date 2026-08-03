package wakeup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
)

type wakeupHookRecorder struct {
	event  workflows.HookEvent
	result workflows.HookResult
	err    error
}

func (r *wakeupHookRecorder) Evaluate(_ context.Context, event workflows.HookEvent) (workflows.HookResult, error) {
	r.event = event
	return r.result, r.err
}

func TestCreateRunsBeforeWakeupCreateHook(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE agent_id=$1 AND issue_id=$2`, wkAgentID, wkOtherIssue); err != nil {
		t.Fatal(err)
	}
	recorder := &wakeupHookRecorder{result: workflows.HookResult{
		Evaluated: true, Decision: workflows.HookRequire,
		Requirements: []string{"Workflow requires a visible result first."},
	}}
	svc := wkService().WithHooks(recorder)

	_, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID: wkAgentID, IssueID: wkOtherIssue, Prompt: "check later", TriggerType: TriggerTime,
		FireAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err == nil || !strings.Contains(err.Error(), "visible result") {
		t.Fatalf("Create error = %v", err)
	}
	if recorder.event.Type != workflows.HookBeforeWakeupCreate {
		t.Fatalf("hook event = %+v", recorder.event)
	}
	var count int
	if err := wkPool.QueryRow(ctx, `SELECT count(*) FROM cerebro_agent_wakeup WHERE agent_id=$1 AND issue_id=$2`, wkAgentID, wkOtherIssue).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("blocked Workflow create persisted %d wakeups", count)
	}
}

func TestWakeupFireDecisionUsesWorkflowModification(t *testing.T) {
	recorder := &wakeupHookRecorder{result: workflows.HookResult{
		Evaluated: true, Decision: workflows.HookModify,
		Modifications: map[string]any{
			"failure_action": "postpone", "postpone_seconds": float64(600), "notify_after": 4,
		},
	}}
	svc := &Service{Hooks: recorder}
	decision, err := svc.wakeupFireDecision(context.Background(), cerebroWakeupFixture(), wakeupDispatchFailure{
		Reason: postponeReasonOffline, Err: errors.New("offline"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "postpone" || decision.PostponeDelay != 10*time.Minute || decision.NotifyAfter != 4 || !decision.WorkflowHandled {
		t.Fatalf("decision = %+v", decision)
	}
	if recorder.event.Type != workflows.HookOnWakeupFireFailure {
		t.Fatalf("hook event = %+v", recorder.event)
	}
}

func cerebroWakeupFixture() cerebrodb.CerebroAgentWakeup {
	return cerebrodb.CerebroAgentWakeup{
		ID:          pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		AgentID:     pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		IssueID:     pgtype.UUID{Bytes: [16]byte{4}, Valid: true},
		TriggerType: TriggerTime,
	}
}
