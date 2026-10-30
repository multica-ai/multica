package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestOpenCodeClockSkewResumeQueries(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "clock-skew resume filtering")
	chatID := dbfx.ChatSession(t, agentID)
	for _, scope := range []struct{ name, column, id string }{
		{"issue", "issue_id", issueID}, {"chat", "chat_session_id", chatID},
	} {
		t.Run(scope.name, func(t *testing.T) {
			for _, history := range []struct{ session, status, reason, age string }{
				{"healthy", "completed", "", "4 minutes"},
				{"skewed", "completed", "", "3 minutes"},
				{"skewed", "failed", "opencode_clock_skew", "2 minutes"},
			} {
				dbfx.Task(t, agentID, testutil.Cols{scope.column: scope.id, "runtime_id": runtimeID, "session_id": history.session, "status": history.status, "failure_reason": history.reason, "work_dir": "/clock-workdir", "completed_at": testutil.Raw("now() - interval '" + history.age + "'")})
			}
			if scope.name == "issue" {
				got, err := testHandler.Queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{AgentID: parseUUID(agentID), IssueID: parseUUID(issueID)})
				if err != nil || got.SessionID.String != "healthy" {
					t.Fatalf("issue resumed poisoned history: %+v, %v", got, err)
				}
			} else {
				got, err := testHandler.Queries.GetLastChatTaskSession(ctx, db.GetLastChatTaskSessionParams{ChatSessionID: parseUUID(chatID)})
				if err != nil || got.SessionID.String != "healthy" {
					t.Fatalf("chat resumed poisoned history: %+v, %v", got, err)
				}
			}
		})
	}
}

func TestOpenCodeClockSkewFailureClearsChatPointer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	chatID := dbfx.ChatSession(t, agentID, testutil.Cols{"session_id": "skewed", "work_dir": "/clock-workdir", "runtime_id": runtimeID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"chat_session_id": chatID, "runtime_id": runtimeID, "status": "running", "session_id": "skewed"})
	_, err := testHandler.TaskService.FailTask(ctx, parseUUID(taskID), "opencode clock skew: session ordering is unreliable", "skewed", "/clock-workdir", "", "opencode_clock_skew", false, "skewed", "")
	if err != nil {
		t.Fatal(err)
	}
	var pointer pgtype.Text
	dbfx.QueryRow(t, `SELECT session_id FROM chat_session WHERE id = $1`, chatID).Scan(&pointer)
	if pointer.Valid {
		t.Fatalf("failed callback restored retired chat pointer: %+v", pointer)
	}
}

func TestOpenCodeClockRetirementSurvivesManualRetryOfOldTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "manual retry after clock preflight retirement")
	sourceID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "completed", "session_id": "ses-old", "work_dir": "/clock-workdir"})
	dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "completed", "session_id": "ses-new", "retired_session_id": "ses-old"})
	dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "rerun_of_task_id": sourceID, "force_fresh_session": true, "priority": 1000})
	got := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if got.PriorSessionID != "" || got.PriorWorkDir != "/clock-workdir" {
		t.Fatalf("manual retry bypassed retirement: %+v", got)
	}
}

func TestOpenCodeClockFailureReleasesIssueQueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "clock-skew terminal releases queue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running", "session_id": "skewed"})
	nextID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "priority": 1000})
	_, err := testHandler.TaskService.FailTask(ctx, parseUUID(taskID), "fresh OpenCode launch failed", "", "/clock-workdir", "", "agent_error.process_failure", false, "skewed", "")
	if err != nil {
		t.Fatal(err)
	}
	got := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, nextID).Scan(&status)
	if status != "dispatched" || got.PriorSessionID != "" {
		t.Fatalf("queue did not recover with a clean session: %+v", got)
	}
}
