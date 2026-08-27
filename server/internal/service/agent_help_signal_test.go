package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestFailTask_AgentRequestedHelpSignal(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := pool.Exec(ctx, ` + '`UPDATE agent_task_queue SET status=\'running\', started_at=now(), attempt=3, max_attempts=3 WHERE id=$1`' + `, task.ID); err != nil {
		t.Fatalf("set running/exhausted: %v", err)
	}

	blocked := "cannot reach the staging deploy API"
	conf := 0.3
	needs := []string{"a deploy token", "review of the schema diff"}
	help := HelpSignal{BlockedReason: &blocked, Needs: needs, Confidence: &conf}

	failed, err := svc.FailTask(ctx, task.ID, "I am stuck and need help", "", "", "", string(taskfailure.ReasonAgentRequestedHelp), false, "", "", help)
	if err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("failed status = %q, want failed", failed.Status)
	}

	got, err := q.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if got.FailureReason.String != string(taskfailure.ReasonAgentRequestedHelp) {
		t.Fatalf("failure_reason = %q, want %q", got.FailureReason.String, taskfailure.ReasonAgentRequestedHelp)
	}
	if len(got.HelpSignal) == 0 {
		t.Fatalf("help_signal not persisted")
	}
	var sig map[string]any
	if err := json.Unmarshal(got.HelpSignal, &sig); err != nil {
		t.Fatalf("unmarshal help_signal: %v", err)
	}
	if got, ok := sig["blocked_reason"].(string); !ok || got != blocked {
		t.Fatalf("help_signal.blocked_reason = %v, want %q", sig["blocked_reason"], blocked)
	}
	if got, ok := sig["needs"].([]any); !ok || len(got) != 2 {
		t.Fatalf("help_signal.needs = %v, want 2 entries", sig["needs"])
	}
	if got, ok := sig["confidence"].(float64); !ok || got != conf {
		t.Fatalf("help_signal.confidence = %v, want %v", sig["confidence"], conf)
	}

	var children int
	if err := pool.QueryRow(ctx, ` + '`SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`' + `, task.ID).Scan(&children); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if children != 0 {
		t.Fatalf("auto-rerun children = %d, want 0 (help signal must not auto-retry)", children)
	}
}

func TestFailTask_HelpSignalWithoutExplicitReason(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	needs := []string{"a second opinion on the regex"}
	help := HelpSignal{Needs: needs}

	failed, err := svc.FailTask(ctx, task.ID, "not sure how to proceed", "", "", "", "", false, "", "", help)
	if err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("failed status = %q, want failed", failed.Status)
	}

	got, err := q.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if got.FailureReason.String != string(taskfailure.ReasonAgentRequestedHelp) {
		t.Fatalf("failure_reason = %q, want %q", got.FailureReason.String, taskfailure.ReasonAgentRequestedHelp)
	}
	if len(got.HelpSignal) == 0 {
		t.Fatalf("help_signal not persisted")
	}
	var sig map[string]any
	if err := json.Unmarshal(got.HelpSignal, &sig); err != nil {
		t.Fatalf("unmarshal help_signal: %v", err)
	}
	if got, ok := sig["needs"].([]any); !ok || len(got) != 1 {
		t.Fatalf("help_signal.needs = %v, want 1 entry", sig["needs"])
	}
}
