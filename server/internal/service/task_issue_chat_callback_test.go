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
	dbfx            *testutil.Fixture
	userID          string
	mikaAgentID     string
	mikaRuntimeID   string
	workerAgentID   string
	workerRuntimeID string
	squadID         string
	chatSessionID   string
	originTaskID    string
	issueID         string
	leaderTaskID    string
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
		dbfx:            dbfx,
		userID:          userID,
		mikaAgentID:     mikaAgentID,
		mikaRuntimeID:   mikaRuntimeID,
		workerAgentID:   workerAgentID,
		workerRuntimeID: workerRuntimeID,
		squadID:         squadID,
		chatSessionID:   chatSessionID,
		originTaskID:    originTaskID,
		issueID:         issueID,
		leaderTaskID:    leaderTaskID,
	}
}

func TestCompleteSquadLeaderTaskFindsOriginalMikaChatThroughSpecAndTicketCards(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	specIssueID := fx.dbfx.Issue(t, "Spec card created from Mika chat", testutil.Cols{
		"origin_type": "agent_create",
		"origin_id":   fx.originTaskID,
	})
	specCardTaskID := fx.dbfx.Task(t, fx.mikaAgentID, testutil.Cols{
		"runtime_id":          fx.mikaRuntimeID,
		"issue_id":            specIssueID,
		"status":              "completed",
		"started_at":          testutil.Raw("now() - interval '1 minute'"),
		"completed_at":        testutil.Raw("now()"),
		"initiator_user_id":   fx.userID,
		"originator_user_id":  fx.userID,
		"accountable_user_id": fx.userID,
	})
	ticketIssueID := fx.dbfx.Issue(t, "Ticket card created from the Spec card", testutil.Cols{
		"origin_type": "agent_create",
		"origin_id":   specCardTaskID,
	})
	ticketCardTaskID := fx.dbfx.Task(t, fx.mikaAgentID, testutil.Cols{
		"runtime_id":          fx.mikaRuntimeID,
		"issue_id":            ticketIssueID,
		"status":              "completed",
		"started_at":          testutil.Raw("now() - interval '30 seconds'"),
		"completed_at":        testutil.Raw("now()"),
		"initiator_user_id":   fx.userID,
		"originator_user_id":  fx.userID,
		"accountable_user_id": fx.userID,
	})
	fx.dbfx.Exec(t, `UPDATE issue SET origin_id = $2 WHERE id = $1`, fx.issueID, ticketCardTaskID)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	if _, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		[]byte(`{"output":"implementation card completed through its parent Spec card"}`),
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete Spec/Ticket-origin Squad leader task: %v", err)
	} else if !transitioned {
		t.Fatal("nested-origin Squad leader completion did not transition")
	}
	if got := issueCallbackCount(t, fx); got != 1 {
		t.Fatalf("callbacks through parent card ancestry = %d, want 1", got)
	}
	var callbackSessionID string
	fx.dbfx.QueryRow(t, `
		SELECT chat_session_id
		FROM agent_task_queue
		WHERE trigger_evidence_kind = 'issue_task_callback'
		  AND trigger_evidence_ref_id = $1
	`, fx.leaderTaskID).Scan(&callbackSessionID)
	if callbackSessionID != fx.chatSessionID {
		t.Fatalf("callback session = %s, want original Mika chat %s", callbackSessionID, fx.chatSessionID)
	}
}

func TestSquadLeaderDoesNotReturnToDifferentOriginChatAgent(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	workerLedSquadID := fx.dbfx.Squad(t, "Worker-led callback squad", fx.workerAgentID)
	fx.dbfx.Exec(t, `
		UPDATE issue
		SET assignee_id = $2
		WHERE id = $1
	`, fx.issueID, workerLedSquadID)
	fx.dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET agent_id = $2, runtime_id = $3, squad_id = $4
		WHERE id = $1
	`, fx.leaderTaskID, fx.workerAgentID, fx.workerRuntimeID, workerLedSquadID)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	if _, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		[]byte(`{"output":"a different Squad leader completed the card"}`),
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete different-agent Squad leader task: %v", err)
	} else if !transitioned {
		t.Fatal("different-agent Squad leader completion did not transition")
	}
	if got := issueCallbackCount(t, fx); got != 0 {
		t.Fatalf("callbacks into a different Agent's origin chat = %d, want 0", got)
	}
}

func TestSquadLeaderDoesNotReturnAfterChatCreatorLosesInvokePermission(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	chatCreatorID := fx.dbfx.User(t, "Revoked callback member", "revoked-callback-"+uuid.NewString()+"@multica.test")
	fx.dbfx.Member(t, fx.dbfx.WorkspaceID, chatCreatorID, "member")
	fx.dbfx.Exec(t, `UPDATE agent SET permission_mode = 'public_to' WHERE id = $1`, fx.mikaAgentID)
	invocationTargetID := fx.dbfx.Insert(t, "agent_invocation_target", testutil.Cols{
		"agent_id":    fx.mikaAgentID,
		"target_type": "workspace",
		"target_id":   fx.dbfx.WorkspaceID,
		"created_by":  fx.userID,
	})
	fx.dbfx.Exec(t, `UPDATE chat_session SET creator_id = $2 WHERE id = $1`, fx.chatSessionID, chatCreatorID)
	fx.dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET initiator_user_id = $2, originator_user_id = $2, accountable_user_id = $2
		WHERE id IN ($1, $3)
	`, fx.originTaskID, chatCreatorID, fx.leaderTaskID)

	// The member could invoke Mika when the session and card were created, but
	// loses that permission before the leader finishes.
	fx.dbfx.Exec(t, `DELETE FROM agent_invocation_target WHERE id = $1`, invocationTargetID)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())
	if _, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		[]byte(`{"output":"must not re-enter a session after permission revocation"}`),
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete leader after callback permission revocation: %v", err)
	} else if !transitioned {
		t.Fatal("leader completion after permission revocation did not transition")
	}
	if got := issueCallbackCount(t, fx); got != 0 {
		t.Fatalf("callbacks after chat creator permission revocation = %d, want 0", got)
	}
}

func TestSquadLeaderDoesNotReturnAnotherMembersRunToChatCreator(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	otherUserID := fx.dbfx.User(t, "Different callback originator", "different-callback-"+uuid.NewString()+"@multica.test")
	fx.dbfx.Member(t, fx.dbfx.WorkspaceID, otherUserID, "member")
	fx.dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET initiator_user_id = $2, originator_user_id = $2, accountable_user_id = $2
		WHERE id = $1
	`, fx.leaderTaskID, otherUserID)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	if _, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		[]byte(`{"output":"result triggered by a different member"}`),
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete different-originator Squad leader task: %v", err)
	} else if !transitioned {
		t.Fatal("different-originator Squad leader completion did not transition")
	}
	if got := issueCallbackCount(t, fx); got != 0 {
		t.Fatalf("callbacks from another member into chat creator's session = %d, want 0", got)
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
	var initiatorUserID, originatorUserID, accountableUserID string
	fx.dbfx.QueryRow(t, `
		SELECT id, agent_id, runtime_id, chat_session_id, status,
		       chat_input_task_id, delegated_from_task_id,
		       initiator_user_id, originator_user_id, accountable_user_id
		FROM agent_task_queue
		WHERE trigger_evidence_kind = 'issue_task_callback'
		  AND trigger_evidence_ref_id = $1
	`, fx.leaderTaskID).Scan(
		&callbackTaskID, &agentID, &runtimeID, &chatSessionID, &status,
		&inputOwnerID, &delegatedFromID,
		&initiatorUserID, &originatorUserID, &accountableUserID,
	)
	if agentID != fx.mikaAgentID || runtimeID != fx.mikaRuntimeID || chatSessionID != fx.chatSessionID {
		t.Fatalf("callback target = agent %s runtime %s chat %s, want Mika %s/%s/%s",
			agentID, runtimeID, chatSessionID, fx.mikaAgentID, fx.mikaRuntimeID, fx.chatSessionID)
	}
	if status != "queued" || inputOwnerID != callbackTaskID || delegatedFromID != fx.leaderTaskID {
		t.Fatalf("callback lineage = status %q owner %s delegated_from %s, want queued/%s/%s",
			status, inputOwnerID, delegatedFromID, callbackTaskID, fx.leaderTaskID)
	}
	if initiatorUserID != fx.userID || originatorUserID != fx.userID || accountableUserID != fx.userID {
		t.Fatalf("callback principal = initiator %s originator %s accountable %s, want chat creator %s",
			initiatorUserID, originatorUserID, accountableUserID, fx.userID)
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

func TestCompleteSquadLeaderCallbackIncludesFinalOutputAfterProgressComment(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

	fx.dbfx.Comment(t, fx.issueID, "Progress only; the final result is not ready yet.", testutil.Cols{
		"author_type": "agent",
		"author_id":   fx.mikaAgentID,
		"created_at":  testutil.Raw("now() + interval '1 second'"),
	})
	const finalOutput = "FINAL-RESULT-7f8c: the implementation and focused acceptance test passed."
	if _, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		[]byte(`{"output":"`+finalOutput+`"}`),
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete Squad leader after progress comment: %v", err)
	} else if !transitioned {
		t.Fatal("Squad leader completion did not transition")
	}

	if got := fx.dbfx.Count(t, `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND content = $2
	`, fx.issueID, finalOutput); got != 0 {
		t.Fatalf("final-output fallback comments = %d, want 0 because progress already existed", got)
	}
	var callbackContent string
	fx.dbfx.QueryRow(t, `
		SELECT message.content
		FROM chat_message AS message
		JOIN agent_task_queue AS task ON task.id = message.task_id
		WHERE task.trigger_evidence_kind = 'issue_task_callback'
		  AND task.trigger_evidence_ref_id = $1
	`, fx.leaderTaskID).Scan(&callbackContent)
	if !strings.Contains(callbackContent, finalOutput) {
		t.Fatalf("callback input %q does not contain the leader's final output", callbackContent)
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

func TestManualSquadLeaderSuccessorSuppressesStaleFailureCallback(t *testing.T) {
	fx := seedIssueChatCallbackFixture(t)
	svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())
	successorID := fx.dbfx.Task(t, fx.mikaAgentID, testutil.Cols{
		"runtime_id":          fx.mikaRuntimeID,
		"issue_id":            fx.issueID,
		"status":              "queued",
		"is_leader_task":      true,
		"squad_id":            fx.squadID,
		"rerun_of_task_id":    fx.leaderTaskID,
		"initiator_user_id":   fx.userID,
		"originator_user_id":  fx.userID,
		"accountable_user_id": fx.userID,
	})

	if _, transitioned, err := svc.FailTaskWithTransition(
		context.Background(), parseTestUUID(t, fx.leaderTaskID),
		"the superseded run failed", "", "", "", "agent_error.unknown", false, "", "",
	); err != nil {
		t.Fatalf("fail superseded Squad leader task: %v", err)
	} else if !transitioned {
		t.Fatal("superseded failure did not transition")
	}
	if got := issueCallbackCount(t, fx); got != 0 {
		t.Fatalf("callbacks for superseded failure = %d, want 0", got)
	}

	fx.dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, successorID)
	if _, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), parseTestUUID(t, successorID),
		[]byte(`{"output":"the manual successor succeeded"}`),
		"", "", "", false, "", "",
	); err != nil {
		t.Fatalf("complete manual Squad leader successor: %v", err)
	} else if !transitioned {
		t.Fatal("manual successor completion did not transition")
	}
	if got := fx.dbfx.Count(t, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'issue_task_callback'
		  AND trigger_evidence_ref_id = $1
	`, successorID); got != 1 {
		t.Fatalf("callbacks for successful successor = %d, want 1", got)
	}
}

func TestSquadLeaderFailureBeforeRunningStillQueuesOriginalMikaChat(t *testing.T) {
	for _, status := range []string{"dispatched", "waiting_local_directory"} {
		t.Run(status, func(t *testing.T) {
			fx := seedIssueChatCallbackFixture(t)
			fx.dbfx.Exec(t, `UPDATE agent_task_queue SET status = $2 WHERE id = $1`, fx.leaderTaskID, status)
			svc := NewTaskService(db.New(fx.dbfx.Pool), fx.dbfx.Pool, nil, events.New())

			if _, transitioned, err := svc.FailTaskWithTransition(
				context.Background(), parseTestUUID(t, fx.leaderTaskID),
				"the leader failed before reaching running", "", "", "", "agent_error.unknown", false, "", "",
			); err != nil {
				t.Fatalf("fail %s Squad leader task: %v", status, err)
			} else if !transitioned {
				t.Fatalf("%s failure did not transition", status)
			}
			if got := issueCallbackCount(t, fx); got != 1 {
				t.Fatalf("callbacks after %s failure = %d, want 1", status, got)
			}
		})
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
