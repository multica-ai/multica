package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func newChatSupplementFixture(t *testing.T, negotiated bool) (string, string, string) {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "chat-steering", testutil.Cols{"provider": "codex"})
	agentID := dbfx.Agent(t, "Chat steering "+uuid.NewString(), runtimeID)
	sessionID := dbfx.ChatSession(t, agentID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "issue_id": nil, "status": "dispatched"})
	dbfx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1`, taskID)
	dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "task_id": taskID, "role": "user", "content": "original objective"})
	dbfx.Cleanup(t, `DELETE FROM chat_task_supplement WHERE chat_session_id=$1`, sessionID)
	dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id=$1`, taskID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE chat_session_id=$1 AND id<>$2`, sessionID, taskID)
	dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id IN (SELECT id FROM agent_task_queue WHERE chat_session_id=$1)`, sessionID)
	var capabilities []string
	if negotiated {
		capabilities = append(capabilities, protocol.DaemonCapabilityTaskSupplementV1)
	}
	var started AgentTaskResponse
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/start", map[string]any{"capabilities": capabilities}), "taskId", taskID)
	testutil.Call(t, testHandler.StartTask, req).Want(http.StatusOK).JSON(&started)
	if negotiated && started.SupplementCapability != protocol.DaemonCapabilityTaskSupplementV1 {
		t.Fatalf("chat start did not echo its negotiated capability: %q", started.SupplementCapability)
	}
	return sessionID, taskID, agentID
}

func sendChatGuidance(t *testing.T, sessionID string, body map[string]any) SendChatMessageResponse {
	t.Helper()
	var response SendChatMessageResponse
	req := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/messages", body)), "sessionId", sessionID)
	testutil.Call(t, testHandler.SendChatMessage, req).Want(http.StatusCreated).JSON(&response)
	return response
}

func TestChatSupplementStartReplayAndBurst(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	generation := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at=$2 WHERE id=$1`, taskID, generation)
	for range 2 {
		var started AgentTaskResponse
		req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/start", map[string]any{
			"runtime_id": uuidToString(task.RuntimeID), "dispatched_at": generation.Format(time.RFC3339Nano),
			"capabilities": []string{protocol.DaemonCapabilityTaskSupplementV1},
		}), "taskId", taskID)
		testutil.Call(t, testHandler.StartTask, req).Want(http.StatusOK).JSON(&started)
		if started.SupplementCapability != protocol.DaemonCapabilityTaskSupplementV1 {
			t.Fatal("chat start replay lost steering negotiation")
		}
	}
	const count = 5
	var sent []SendChatMessageResponse
	for i := range count {
		sent = append(sent, sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": fmt.Sprintf("guidance %d", i)}))
		if !sent[i].Steered || sent[i].Queued || sent[i].TaskID != taskID {
			t.Fatalf("guidance %d created a separate turn: %+v", i, sent[i])
		}
	}
	for _, message := range sent {
		var claimed struct {
			MessageID string `json:"comment_id"`
		}
		req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/claim", nil), "taskId", taskID)
		testutil.Call(t, testHandler.ClaimTaskSupplement, req).Want(http.StatusOK).JSON(&claimed)
		if claimed.MessageID != message.MessageID {
			t.Fatalf("guidance order: got %s, want %s", claimed.MessageID, message.MessageID)
		}
		req = withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": true}), "taskId", taskID, "commentId", message.MessageID)
		testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
	}
	if w := completeTaskViaHandler(t, taskID, "one answer covering every addition"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 1 {
		t.Fatalf("burst produced %d turns", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND role='assistant'`, sessionID); n != 1 {
		t.Fatalf("burst produced %d replies", n)
	}
}

func TestChatSupplementPendingCapability(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		t.Run(fmt.Sprintf("negotiated=%v", negotiated), func(t *testing.T) {
			sessionID, taskID, _ := newChatSupplementFixture(t, negotiated)
			for _, status := range []string{"running", "dispatched", "queued"} {
				dbfx.Exec(t, `UPDATE agent_task_queue SET status=$2 WHERE id=$1`, taskID, status)
				var pending PendingChatTaskResponse
				req := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/pending-task", nil)), "sessionId", sessionID)
				testutil.Call(t, testHandler.GetPendingChatTask, req).Want(http.StatusOK).JSON(&pending)
				if want := negotiated && status == "running"; pending.CanSupplement != want {
					t.Fatalf("%s: can_supplement=%v, want %v", status, pending.CanSupplement, want)
				}
			}
		})
	}
}

func TestChatSupplementSendClaimAck(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	key := uuid.NewString()
	body := map[string]any{"steer": true, "content": "use the new location", "submission_id": key}
	sent := sendChatGuidance(t, sessionID, body)
	replay := sendChatGuidance(t, sessionID, body)
	if !sent.Steered || sent.Queued || sent.TaskID != taskID || replay.MessageID != sent.MessageID {
		t.Fatalf("send=%+v replay=%+v", sent, replay)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 1 {
		t.Fatalf("steering created another run: %d", n)
	}
	var claim struct{ CommentID, Content string }
	// JSON wire names are intentionally shared with issue supplement delivery.
	var wire map[string]any
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/claim", nil), "taskId", taskID)
	testutil.Call(t, testHandler.ClaimTaskSupplement, req).Want(http.StatusOK).JSON(&wire)
	claim.CommentID, _ = wire["comment_id"].(string)
	claim.Content, _ = wire["content"].(string)
	if claim.CommentID != sent.MessageID || claim.Content != body["content"] || wire["author_type"] != "member" {
		t.Fatalf("wrong guidance: %+v", wire)
	}
	for range 2 {
		req = withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": true}), "taskId", taskID, "commentId", sent.MessageID)
		testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
	}
	if w := completeTaskViaHandler(t, taskID, "updated reply"); w.Code != http.StatusOK {
		t.Fatalf("completion: %s", w.Body.String())
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 1 {
		t.Fatal("delivered guidance ran twice")
	}
}

func TestChatSupplementFallbacks(t *testing.T) {
	for _, reason := range []string{"completed", "failed", "cancelled", "provider_rejected"} {
		t.Run(reason, func(t *testing.T) {
			sessionID, taskID, _ := newChatSupplementFixture(t, true)
			sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "never lose this"})
			if !sent.Steered {
				t.Fatal("not steered")
			}
			if reason == "provider_rejected" {
				if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(taskID)); err != nil {
					t.Fatal(err)
				}
				req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": false}), "taskId", taskID, "commentId", sent.MessageID)
				testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
				req = withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": false}), "taskId", taskID, "commentId", sent.MessageID)
				testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
			} else {
				tx, err := testPool.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(t.Context())
				q := testHandler.Queries.WithTx(tx)
				if _, err := tx.Exec(t.Context(), `UPDATE agent_task_queue SET status=$2 WHERE id=$1`, taskID, reason); err != nil {
					t.Fatal(err)
				}
				task, err := q.GetAgentTask(t.Context(), parseUUID(taskID))
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if err := service.SettleTerminalTaskState(t.Context(), q, task); err != nil {
						t.Fatal(err)
					}
				}
				if err := tx.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			var owner, status string
			if err := testPool.QueryRow(t.Context(), `SELECT m.task_id::text, t.status FROM chat_message m JOIN agent_task_queue t ON t.id=m.task_id WHERE m.id=$1`, sent.MessageID).Scan(&owner, &status); err != nil {
				t.Fatal(err)
			}
			if owner == taskID || status != "queued" {
				t.Fatalf("fallback owner=%s status=%s", owner, status)
			}
			inputs, err := testHandler.Queries.ListChatInputMessages(t.Context(), parseUUID(owner))
			if err != nil || len(inputs) != 1 || inputs[0].Content != "never lose this" {
				t.Fatalf("fallback input=%+v err=%v", inputs, err)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 2 {
				t.Fatalf("fallback created %d tasks", n)
			}
		})
	}
}

func TestChatSupplementQueueChoiceAndOldDaemon(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		sessionID, taskID, _ := newChatSupplementFixture(t, negotiated)
		sent := sendChatGuidance(t, sessionID, map[string]any{"content": "next turn", "steer": !negotiated})
		if sent.Steered || !sent.Queued || sent.TaskID == taskID {
			t.Fatalf("queue fallback: %+v", sent)
		}
	}
}

func TestChatSupplementLegacySendRemainsQueued(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	sent := sendChatGuidance(t, sessionID, map[string]any{"content": "legacy queued message"})
	if sent.Steered || !sent.Queued || sent.TaskID == taskID {
		t.Fatalf("legacy send changed into guidance: %+v", sent)
	}
}

func TestChatSupplementTerminalFallbackBroadcastsQueued(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			sessionID, taskID, _ := newChatSupplementFixture(t, true)
			sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "next turn"})
			queued := subscribeChatTaskQueued(t, sessionID)
			switch terminal {
			case "completed":
				if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
					t.Fatal(w.Body.String())
				}
			case "failed":
				if _, err := testHandler.TaskService.FailTask(t.Context(), parseUUID(taskID), "failed", "", "", "", "agent_error", false, "", ""); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if _, err := testHandler.TaskService.CancelTask(t.Context(), parseUUID(taskID)); err != nil {
					t.Fatal(err)
				}
			}
			var owner string
			dbfx.QueryRow(t, `SELECT task_id::text FROM chat_message WHERE id=$1`, sent.MessageID).Scan(&owner)
			select {
			case id := <-queued:
				if id != owner {
					t.Fatalf("queued %s, want %s", id, owner)
				}
			default:
				t.Fatal("terminal fallback did not broadcast task:queued")
			}
		})
	}
}

func TestChatSupplementStopWaitsForDeliveryResult(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		t.Run(map[bool]string{true: "delivered", false: "rejected"}[delivered], func(t *testing.T) {
			sessionID, taskID, _ := newChatSupplementFixture(t, true)
			first := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "first guidance"})
			if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(taskID)); err != nil {
				t.Fatal(err)
			}
			sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "second guidance"})
			queued := subscribeChatTaskQueued(t, sessionID)
			if _, err := testHandler.TaskService.CancelTask(t.Context(), parseUUID(taskID)); err != nil {
				t.Fatal(err)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND status='queued'`, sessionID); n != 0 {
				t.Fatal("Stop requeued guidance before its delivery result")
			}
			for range 2 {
				req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": delivered}), "taskId", taskID, "commentId", first.MessageID)
				testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
			}
			for range 2 {
				req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/cancel-ack", nil), "taskId", taskID)
				testutil.Call(t, testHandler.AckTaskCancelled, req).Want(http.StatusOK)
			}
			want := 1
			if !delivered {
				want++
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND status='queued'`, sessionID); n != want {
				t.Fatalf("queued %d turns, want %d", n, want)
			}
			if n := len(queued); n != want {
				t.Fatalf("queued events=%d, want %d", n, want)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE task_id=$1 AND role='assistant'`, taskID); n > 1 {
				t.Fatal("Stop wrote duplicate outcomes")
			}
		})
	}
}

func TestChatSupplementStopGraceRequeuesUnconfirmedGuidance(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "delivery unconfirmed"})
	if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.TaskService.CancelTask(t.Context(), parseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	// The existing sweeper invokes this finalizer when the daemon never acks.
	if !testHandler.TaskService.FinalizeDeferredCancelledChat(t.Context(), parseUUID(taskID)) {
		t.Fatal("unconfirmed guidance was stranded")
	}
	if testHandler.TaskService.FinalizeDeferredCancelledChat(t.Context(), parseUUID(taskID)) {
		t.Fatal("duplicate finalize changed state")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND status='queued'`, sessionID); n != 1 {
		t.Fatalf("fallback created %d turns", n)
	}
}

func TestChatSupplementStopWithTranscriptSettlesDeliveryWithoutDuplicateOutcome(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "already injected"})
	if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, `INSERT INTO task_message (task_id,seq,type,content) VALUES ($1,1,'assistant','working')`, taskID)
	if _, err := testHandler.TaskService.CancelTaskWithResult(t.Context(), parseUUID(taskID), service.CancelTaskOptions{}); err != nil {
		t.Fatal(err)
	}
	req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": true}), "taskId", taskID, "commentId", sent.MessageID)
	testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
	testHandler.TaskService.FinalizeDeferredCancelledChat(t.Context(), parseUUID(taskID))
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE task_id=$1 AND role='assistant'`, taskID); n != 1 {
		t.Fatalf("Stop outcomes=%d", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 1 {
		t.Fatal("acknowledged guidance ran again")
	}
}

func TestChatSupplementAccessRevocationWithdrawsGuidance(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "in flight"})
	if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "pending"})
	tx, err := testPool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	q := testHandler.Queries.WithTx(tx)
	if _, err := q.LockChatSessionForTask(t.Context(), parseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	task, err := q.CancelAgentTask(t.Context(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PrepareRuntimeAccessCancellation(t.Context(), q, []db.AgentTaskQueue{task}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	testHandler.TaskService.FinalizeDeferredCancelledChat(t.Context(), task.ID)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 1 {
		t.Fatal("revocation started more work")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_task_supplement WHERE task_id=$1 AND status='withdrawn'`, taskID); n != 2 {
		t.Fatal("revocation left guidance deliverable")
	}
}

func TestChatSupplementRetryClaimKeepsColdInputAndOmitsResumedGuidance(t *testing.T) {
	for _, sameSession := range []bool{true, false} {
		t.Run(map[bool]string{true: "same provider session", false: "different provider session"}[sameSession], func(t *testing.T) {
			sessionID, taskID, _ := newChatSupplementFixture(t, true)
			sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "delivered guidance"})
			if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(taskID)); err != nil {
				t.Fatal(err)
			}
			req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": true}), "taskId", taskID, "commentId", sent.MessageID)
			testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
			sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "pending guidance"})
			if _, err := testHandler.TaskService.FailTask(t.Context(), parseUUID(taskID), "connection reset", "provider-session", "/work", "", "agent_error.provider_network", false, "", ""); err != nil {
				t.Fatal(err)
			}
			if !sameSession {
				dbfx.Exec(t, `UPDATE chat_session SET session_id='other-session' WHERE id=$1`, sessionID)
			}
			var childID string
			dbfx.QueryRow(t, `SELECT id::text FROM agent_task_queue WHERE parent_task_id=$1`, taskID).Scan(&childID)
			child, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(childID))
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := testHandler.Queries.GetAgentRuntime(t.Context(), child.RuntimeID)
			if err != nil {
				t.Fatal(err)
			}
			resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(newOwnerDaemonRequest(http.MethodPost, "/claim", nil), &child, runtime, uuidToString(child.RuntimeID), uuidToString(runtime.WorkspaceID))
			if failure != nil {
				t.Fatalf("claim failed: %+v", failure)
			}
			if !strings.Contains(resp.ChatMessage, "delivered guidance") || !strings.Contains(resp.ChatMessage, "pending guidance") {
				t.Fatalf("cold input lost guidance: %s", resp.ChatMessage)
			}
			if sameSession {
				if resp.ChatResumeMessage == nil || *resp.ChatResumeMessage != "original objective\n\npending guidance" {
					t.Fatalf("resumed input: %+v", resp.ChatResumeMessage)
				}
			} else if resp.ChatResumeMessage != nil {
				t.Fatal("omitted guidance from a different provider session")
			}
		})
	}
}

func TestChatSupplementRetryKeepsInputAndOrder(t *testing.T) {
	for _, onRetry := range []bool{false, true} {
		for _, delivered := range []bool{false, true} {
			name := map[bool]string{false: "root", true: "retry"}[onRetry] + "/" + map[bool]string{false: "pending", true: "delivered"}[delivered]
			t.Run(name, func(t *testing.T) {
				sessionID, rootID, _ := newChatSupplementFixture(t, true)
				activeID := rootID
				fail := func(taskID string) db.AgentTaskQueue {
					t.Helper()
					if _, err := testHandler.TaskService.FailTask(t.Context(), parseUUID(taskID), "connection reset", "", "", "", "agent_error.provider_network", false, "", ""); err != nil {
						t.Fatal(err)
					}
					var childID string
					if err := testPool.QueryRow(t.Context(), `SELECT id::text FROM agent_task_queue WHERE parent_task_id=$1`, taskID).Scan(&childID); err != nil {
						t.Fatal(err)
					}
					child, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(childID))
					if err != nil {
						t.Fatal(err)
					}
					return child
				}
				if onRetry {
					child := fail(rootID)
					activeID = uuidToString(child.ID)
					dbfx.Exec(t, `UPDATE agent_task_queue SET status='dispatched' WHERE id=$1`, activeID)
					if _, err := testHandler.TaskService.StartTask(t.Context(), child.ID, service.TaskStartCapabilities{Supplement: true}); err != nil {
						t.Fatal(err)
					}
				}
				sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "keep guidance after the original objective"})
				if sent.TaskID != activeID || sent.MessageTaskID != rootID || !sent.Steered {
					t.Fatalf("send: %+v", sent)
				}
				if delivered {
					if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(activeID)); err != nil {
						t.Fatal(err)
					}
					req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": true}), "taskId", activeID, "commentId", sent.MessageID)
					testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
				}
				child := fail(activeID)
				if child.ChatInputTaskID != parseUUID(rootID) {
					t.Fatal("retry lost its root input owner")
				}
				if onRetry && child.Status != "deferred" {
					t.Fatalf("last network retry should retain its backoff: %s", child.Status)
				}
				inputs, err := testHandler.Queries.ListChatInputMessages(t.Context(), child.ChatInputTaskID)
				if err != nil || len(inputs) != 2 || inputs[0].Content != "original objective" || inputs[1].Content != "keep guidance after the original objective" {
					t.Fatalf("retry input order/content: %+v, %v", inputs, err)
				}
				wantTasks := 2
				if onRetry {
					wantTasks++
				}
				if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != wantTasks {
					t.Fatalf("guidance created its own run ahead of retry: %d tasks, want %d", n, wantTasks)
				}
			})
		}
	}
}

func TestChatSupplementExhaustedRetryQueuesGuidance(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	dbfx.Exec(t, `UPDATE agent_task_queue SET attempt=3 WHERE id=$1`, taskID)
	sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "next turn after the failure"})
	if _, err := testHandler.TaskService.FailTask(t.Context(), parseUUID(taskID), "connection reset", "", "", "", "agent_error.provider_network", false, "", ""); err != nil {
		t.Fatal(err)
	}
	var nextID string
	if err := testPool.QueryRow(t.Context(), `SELECT task_id::text FROM chat_message WHERE id=$1`, sent.MessageID).Scan(&nextID); err != nil {
		t.Fatal(err)
	}
	if nextID == taskID {
		t.Fatal("exhausted retry stranded guidance on the failed task")
	}
	inputs, err := testHandler.Queries.ListChatInputMessages(t.Context(), parseUUID(nextID))
	if err != nil || len(inputs) != 1 || inputs[0].Content != "next turn after the failure" {
		t.Fatalf("fallback input=%+v err=%v", inputs, err)
	}
}

func TestChatSupplementSubmissionReplayPreservesSendMode(t *testing.T) {
	sessionID, _, _ := newChatSupplementFixture(t, true)
	key := uuid.NewString()
	sendChatGuidance(t, sessionID, map[string]any{"content": "queued", "submission_id": key})
	req := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/messages", map[string]any{"content": "queued", "submission_id": key, "steer": true})), "sessionId", sessionID)
	testutil.Call(t, testHandler.SendChatMessage, req).Want(http.StatusConflict)
}

func TestChatSupplementDaemonRecoveryRetainsRetryInput(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "guidance before daemon restart"})
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := testHandler.TaskService.RecoverOrphanedTasksForRuntime(t.Context(), task.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	var recovered bool
	for _, parent := range failed {
		if parent.ID != task.ID {
			continue
		}
		child, err := testHandler.TaskService.MaybeRetryFailedTask(t.Context(), parent)
		if err != nil || child == nil {
			t.Fatalf("recovery retry=%+v err=%v", child, err)
		}
		inputs, err := testHandler.Queries.ListChatInputMessages(t.Context(), child.ChatInputTaskID)
		if err != nil || len(inputs) != 2 || inputs[0].Content != "original objective" || inputs[1].Content != "guidance before daemon restart" {
			t.Fatalf("recovery input=%+v err=%v", inputs, err)
		}
		recovered = true
	}
	if !recovered {
		t.Fatal("fixture task was not recovered")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 2 {
		t.Fatalf("guidance escaped the retry batch: %d tasks", n)
	}
}

func TestChatSupplementDeliveredGuidanceSurvivesDraftRestore(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "keep this guidance in history"})
	if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": true}), "taskId", taskID, "commentId", sent.MessageID)
	testutil.Call(t, testHandler.AckTaskSupplement, req).Want(http.StatusOK)
	restored, err := testHandler.Queries.DeleteUserChatMessageByTask(t.Context(), parseUUID(taskID))
	if err != nil || restored.Content != "original objective" {
		t.Fatalf("restored the wrong draft: %+v, %v", restored, err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE id=$1`, sent.MessageID); n != 1 {
		t.Fatal("draft restore deleted delivered guidance")
	}
}

func TestChatSupplementBulkStopDoesNotStartNewWork(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop all", true: "archive"}[archive], func(t *testing.T) {
			sessionID, _, agentID := newChatSupplementFixture(t, true)
			sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "withdraw with all work"})
			var err error
			if archive {
				_, err = testHandler.TaskService.CancelTasksForArchivedAgent(t.Context(), parseUUID(agentID))
			} else {
				_, err = testHandler.TaskService.CancelTasksForAgent(t.Context(), parseUUID(agentID))
			}
			if err != nil {
				t.Fatal(err)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND status='queued'`, sessionID); n != 0 {
				t.Fatal("bulk stop created a new run")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_task_supplement WHERE message_id=$1 AND status='withdrawn'`, sent.MessageID); n != 1 {
				t.Fatal("guidance was not withdrawn")
			}
		})
	}
}

func TestChatSupplementRuntimeTeardownDoesNotStartNewWork(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "withdraw with runtime"})
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := testPool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := service.TeardownRuntime(t.Context(), testHandler.Queries.WithTx(tx), task.RuntimeID, service.RuntimeTeardownOptions{CancelNonTerminalTasks: true}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND status='queued'`, sessionID); n != 0 {
		t.Fatal("runtime teardown created new work")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_task_supplement WHERE message_id=$1 AND status='withdrawn'`, sent.MessageID); n != 1 {
		t.Fatal("runtime teardown left guidance pending")
	}
}

func TestChatSupplementPermissionAndSessionDeletion(t *testing.T) {
	sessionID, taskID, agentID := newChatSupplementFixture(t, true)
	sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "pending"})
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility='public' WHERE id=(SELECT runtime_id FROM agent WHERE id=$1)`, agentID)
	dbfx.Exec(t, `UPDATE agent SET permission_mode='private', owner_id=$2 WHERE id=$1`, agentID, dbfx.User(t, "Other", "other-"+uuid.NewString()+"@test.local"))
	session, err := testHandler.Queries.GetChatSession(t.Context(), parseUUID(sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if testHandler.canSupplementChatTask(newRequest(http.MethodGet, "/pending-task", nil), session, parseUUID(taskID), "running") {
		t.Fatal("revoked caller was offered steering")
	}
	req := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/messages", map[string]any{"steer": true, "content": "forbidden"})), "sessionId", sessionID)
	testutil.Call(t, testHandler.SendChatMessage, req).Want(http.StatusForbidden)
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_task_supplement WHERE task_id=$1`, taskID); n != 1 {
		t.Fatal("revoked send mutated receipts")
	}
	dbfx.Exec(t, `UPDATE agent SET owner_id=$2 WHERE id=$1`, agentID, testUserID)
	req = withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodDelete, "/session", nil)), "sessionId", sessionID)
	testutil.Call(t, testHandler.DeleteChatSession, req).Want(http.StatusNoContent)
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_task_supplement WHERE message_id=$1`, sent.MessageID); n != 0 {
		t.Fatal("deleted session retained guidance")
	}
	if _, err := testHandler.Queries.GetTaskSupplementCapability(t.Context(), parseUUID(taskID)); err == nil {
		t.Fatal("deleted session retained capability")
	}
}
