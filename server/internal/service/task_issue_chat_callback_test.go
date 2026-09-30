package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueChatCallbackFixture struct {
	dbfx          *testutil.Fixture
	mikaAgentID   string
	mikaRuntimeID string
	chatSessionID string
	issueID       string
	leaderTaskID  string
}

func seedIssueChatCallbackFixture(t *testing.T) issueChatCallbackFixture {
	t.Helper()
	pool := sharedTestPool(t)
	seed := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	userID := seed.User(t, "Issue callback user", "issue-callback-"+suffix+"@multica.test")
	workspaceID := seed.Workspace(t, "Issue callback workspace", "issue-callback-"+suffix)
	dbfx := testutil.New(pool, workspaceID, userID)
	dbfx.Member(t, workspaceID, userID, "owner")
	mikaRuntimeID := dbfx.Runtime(t, "Mika callback runtime")
	workerRuntimeID := dbfx.Runtime(t, "Squad worker runtime")
	mikaAgentID := dbfx.Agent(t, "Mika callback agent", mikaRuntimeID)
	workerAgentID := dbfx.Agent(t, "Squad worker agent", workerRuntimeID)
	squadID := dbfx.Squad(t, "Callback squad", mikaAgentID)
	dbfx.SquadMember(t, squadID, "agent", workerAgentID)
	chatSessionID := dbfx.ChatSession(t, mikaAgentID)

	originTaskID := dbfx.Task(t, mikaAgentID, testutil.Cols{
		"runtime_id":          mikaRuntimeID,
		"chat_session_id":     chatSessionID,
		"status":              "completed",
		"initiator_user_id":   userID,
		"originator_user_id":  userID,
		"accountable_user_id": userID,
	})
	issueID := dbfx.Issue(t, "Return the Squad result to Mika", testutil.Cols{
		"assignee_type": "squad",
		"assignee_id":   squadID,
		"origin_type":   "agent_create",
		"origin_id":     originTaskID,
	})
	leaderTaskID := dbfx.Task(t, mikaAgentID, testutil.Cols{
		"runtime_id":          mikaRuntimeID,
		"issue_id":            issueID,
		"status":              "running",
		"started_at":          testutil.Raw("now()"),
		"is_leader_task":      true,
		"squad_id":            squadID,
		"initiator_user_id":   userID,
		"originator_user_id":  userID,
		"accountable_user_id": userID,
	})

	return issueChatCallbackFixture{
		dbfx:          dbfx,
		mikaAgentID:   mikaAgentID,
		mikaRuntimeID: mikaRuntimeID,
		chatSessionID: chatSessionID,
		issueID:       issueID,
		leaderTaskID:  leaderTaskID,
	}
}

func TestCompleteSquadLeaderTaskQueuesOriginalMikaChat(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	completed, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(),
		parseTestUUID(t, fx.leaderTaskID),
		[]byte(`{"output":"The Squad leader finished the card."}`),
		"", "", "", false, "", "",
	)
	if err != nil {
		t.Fatalf("complete Squad leader task: %v", err)
	}
	if !transitioned || completed.Status != "completed" {
		t.Fatalf("completion = status %q transitioned %v, want completed/true", completed.Status, transitioned)
	}

	if got := fx.dbfx.Count(t, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE trigger_evidence_kind = 'issue_task_callback'
		  AND trigger_evidence_ref_id = $1
	`, fx.leaderTaskID); got != 1 {
		t.Fatalf("queued Mika chat callbacks = %d, want 1", got)
	}

	var callbackTaskID, agentID, runtimeID, chatSessionID, status, inputOwnerID, delegatedFromID string
	fx.dbfx.QueryRow(t, `
		SELECT id, agent_id, runtime_id, chat_session_id, status,
		       chat_input_task_id, delegated_from_task_id
		FROM agent_task_queue
		WHERE trigger_evidence_kind = 'issue_task_callback'
		  AND trigger_evidence_ref_id = $1
	`, fx.leaderTaskID).Scan(
		&callbackTaskID, &agentID, &runtimeID, &chatSessionID, &status,
		&inputOwnerID, &delegatedFromID,
	)
	if agentID != fx.mikaAgentID || runtimeID != fx.mikaRuntimeID || chatSessionID != fx.chatSessionID {
		t.Fatalf("callback target = agent %s runtime %s chat %s, want Mika %s/%s/%s",
			agentID, runtimeID, chatSessionID, fx.mikaAgentID, fx.mikaRuntimeID, fx.chatSessionID)
	}
	if status != "queued" || inputOwnerID != callbackTaskID || delegatedFromID != fx.leaderTaskID {
		t.Fatalf("callback lineage = status %q owner %s delegated_from %s, want queued/%s/%s",
			status, inputOwnerID, delegatedFromID, callbackTaskID, fx.leaderTaskID)
	}

	var role, kind, content string
	fx.dbfx.QueryRow(t, `
		SELECT role, message_kind, content
		FROM chat_message
		WHERE task_id = $1
	`, callbackTaskID).Scan(&role, &kind, &content)
	if role != "user" || kind != "issue_callback" {
		t.Fatalf("callback input = role %q kind %q, want user/issue_callback", role, kind)
	}
	if !strings.Contains(content, fx.issueID) || !strings.Contains(content, "completed") {
		t.Fatalf("callback input %q does not identify the completed Squad card", content)
	}
	inputs, err := svc.Queries.ListChatInputMessages(context.Background(), parseTestUUID(t, callbackTaskID))
	if err != nil {
		t.Fatalf("load callback input for Mika: %v", err)
	}
	if len(inputs) != 1 || inputs[0].Content != content {
		t.Fatalf("Mika callback input = %#v, want the hidden callback message", inputs)
	}

	_, replayTransitioned, err := svc.CompleteTaskWithTransition(
		context.Background(),
		parseTestUUID(t, fx.leaderTaskID),
		[]byte(`{"output":"replayed completion"}`),
		"", "", "", false, "", "",
	)
	if err != nil {
		t.Fatalf("replay Squad leader completion: %v", err)
	}
	if replayTransitioned {
		t.Fatal("replayed completion reported a new transition")
	}
	if got := issueCallbackCount(t, fx); got != 1 {
		t.Fatalf("callbacks after completion replay = %d, want 1", got)
	}

	// Finish the queued callback turn as the daemon would. The hidden platform
	// input stays out of the member transcript; Mika's answer is the visible row.
	fx.dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET status = 'running', started_at = now()
		WHERE id = $1
	`, callbackTaskID)
	callbackResult := "Mika reports the important Squad result in the original chat."
	if _, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, callbackTaskID),
		[]byte(`{"output":"`+callbackResult+`"}`),
		"mika-callback-session", "/tmp/mika-callback", "", false, "", "",
	); err != nil {
		t.Fatalf("complete Mika callback task: %v", err)
	} else if !transitioned {
		t.Fatal("Mika callback completion did not transition")
	}
	transcript, err := svc.Queries.ListChatMessages(context.Background(), parseTestUUID(t, fx.chatSessionID))
	if err != nil {
		t.Fatalf("load member transcript after Mika callback: %v", err)
	}
	if len(transcript) != 1 || transcript[0].Role != "assistant" || transcript[0].Content != callbackResult {
		t.Fatalf("member transcript after Mika callback = %#v, want only Mika's visible answer", transcript)
	}
}

func TestFailSquadLeaderTaskQueuesOriginalMikaChatAfterRetriesEnd(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	failed, transitioned, err := svc.FailTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		"the leader could not finish the card", "", "", "", "agent_error.unknown", false, "", "",
	)
	if err != nil {
		t.Fatalf("fail Squad leader task: %v", err)
	}
	if !transitioned || failed.Status != "failed" {
		t.Fatalf("failure = status %q transitioned %v, want failed/true", failed.Status, transitioned)
	}
	if got := issueCallbackCount(t, fx); got != 1 {
		t.Fatalf("queued Mika chat callbacks = %d, want 1", got)
	}
	var content string
	fx.dbfx.QueryRow(t, `
		SELECT message.content
		FROM chat_message AS message
		JOIN agent_task_queue AS task ON task.id = message.task_id
		WHERE task.trigger_evidence_kind = 'issue_task_callback'
		  AND task.trigger_evidence_ref_id = $1
	`, fx.leaderTaskID).Scan(&content)
	if !strings.Contains(content, "failed") || !strings.Contains(content, "agent_error.unknown") {
		t.Fatalf("failure callback input %q does not identify the terminal failure", content)
	}
}

func TestRetryingSquadLeaderTaskDoesNotReturnToMikaYet(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	_, transitioned, err := svc.FailTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		"runtime went offline", "", "", "", "runtime_offline", false, "", "",
	)
	if err != nil {
		t.Fatalf("fail retryable Squad leader task: %v", err)
	}
	if !transitioned {
		t.Fatal("retryable failure did not transition")
	}
	if got := issueCallbackCount(t, fx); got != 0 {
		t.Fatalf("callbacks while a retry is pending = %d, want 0", got)
	}
	if got := fx.dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, fx.leaderTaskID); got != 1 {
		t.Fatalf("retry children = %d, want 1", got)
	}
}

func TestSquadWorkerTaskDoesNotReturnDirectlyToMika(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	fx.dbfx.Exec(t, `UPDATE agent_task_queue SET is_leader_task = FALSE WHERE id = $1`, fx.leaderTaskID)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	if _, _, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID), nil,
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete Squad worker task: %v", err)
	}
	if got := issueCallbackCount(t, fx); got != 0 {
		t.Fatalf("worker callbacks to original chat = %d, want 0", got)
	}
}

func TestSquadLeaderOnOrdinaryIssueDoesNotInventAChatReturnPath(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	fx.dbfx.Exec(t, `UPDATE issue SET origin_type = NULL, origin_id = NULL WHERE id = $1`, fx.issueID)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	if _, _, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID), nil,
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete Squad leader on ordinary issue: %v", err)
	}
	if got := issueCallbackCount(t, fx); got != 0 {
		t.Fatalf("callbacks without a chat-created card = %d, want 0", got)
	}
}

func issueCallbackCount(t *testing.T, fx issueChatCallbackFixture) int {
	t.Helper()
	return fx.dbfx.Count(t, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE trigger_evidence_kind = 'issue_task_callback'
		  AND trigger_evidence_ref_id = $1
	`, fx.leaderTaskID)
}
