package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

var chatSupplementFixtureSeq atomic.Int32

type chatSupplementFixture struct{ agent, runtime, session, task string }

func newChatSupplementFixture(t *testing.T, provider string, negotiate bool) chatSupplementFixture {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "chat-supplement", testutil.Cols{"provider": provider, "owner_id": testUserID})
	agentID := dbfx.Agent(t, fmt.Sprintf("Chat supplement %d", chatSupplementFixtureSeq.Add(1)), runtimeID, testutil.Cols{"owner_id": testUserID})
	sessionID := dbfx.Insert(t, "chat_session", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentID, "creator_id": testUserID, "title": "supplement", "explicitly_created_at": testutil.Raw("now()")})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "dispatched"})
	if _, err := testHandler.TaskService.StartTask(t.Context(), parseUUID(taskID), service.TaskStartCapabilities{Supplement: negotiate}); err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id=$1`, taskID)
	dbfx.Cleanup(t, `DELETE FROM chat_supplement WHERE chat_session_id=$1`, sessionID)
	return chatSupplementFixture{agentID, runtimeID, sessionID, taskID}
}

func chatSupplementAPIRequest(t *testing.T, method, path string, body any) *http.Request {
	return withChatTestWorkspaceCtx(t, newRequest(method, path, body))
}

func chatSupplementRequest(t *testing.T, f chatSupplementFixture, requestID, content string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.CreateChatSupplement, withURLParams(chatSupplementAPIRequest(t, http.MethodPost, "/supplements", map[string]string{"content": content, "client_request_id": requestID}), "sessionId", f.session, "taskId", f.task))
}

func TestChatSupplementNegotiation(t *testing.T) {
	for _, tc := range []struct {
		provider    string
		offer, want bool
	}{{"codex", true, true}, {"codex", false, false}, {"claude", true, false}, {"opencode", true, false}} {
		t.Run(fmt.Sprintf("%s/%t", tc.provider, tc.offer), func(t *testing.T) {
			f := newChatSupplementFixture(t, tc.provider, tc.offer)
			var pending PendingChatTaskResponse
			testutil.Call(t, testHandler.GetPendingChatTask, withURLParam(chatSupplementAPIRequest(t, "GET", "/pending-task", nil), "sessionId", f.session)).Want(200).JSON(&pending)
			if pending.CanSupplement != tc.want {
				t.Fatalf("can_supplement=%v want %v", pending.CanSupplement, tc.want)
			}
			want := 409
			if tc.want {
				want = 201
			}
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "guidance").Want(want)
		})
	}
}

func TestChatSupplementHistoryIdempotenceAndNoReplay(t *testing.T) {
	f := newChatSupplementFixture(t, "codex", true)
	const id = "0199a4e8-22ce-7b01-bba5-111111111111"
	var first, duplicate ChatMessageResponse
	chatSupplementRequest(t, f, id, "marker-one").Want(201).JSON(&first)
	chatSupplementRequest(t, f, id, "marker-one").Want(200).JSON(&duplicate)
	if first.ID != duplicate.ID || first.Supplement == nil || first.Supplement.Status != "pending" {
		t.Fatalf("receipts: %+v %+v", first, duplicate)
	}
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-222222222222", "marker-two").Want(201)
	for _, content := range []string{"marker-one", "marker-two"} {
		row, err := testHandler.Queries.ClaimNextChatSupplement(t.Context(), parseUUID(f.task))
		if err != nil || row.Content != content {
			t.Fatalf("claim: %+v %v", row, err)
		}
		testutil.Call(t, testHandler.AckTaskSupplement, withURLParams(newOwnerDaemonRequest("POST", "/ack", map[string]bool{"delivered": true}), "taskId", f.task, "commentId", uuidToString(row.MessageID))).Want(200)
	}
	if _, err := testHandler.Queries.ClaimNextChatSupplement(t.Context(), parseUUID(f.task)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("replayed: %v", err)
	}
	input, err := testHandler.Queries.ListChatInputMessages(t.Context(), parseUUID(f.task))
	if err != nil || len(input) != 0 {
		t.Fatalf("supplements entered initial input: %v %v", input, err)
	}
	legacy, err := testHandler.Queries.ListChatMessagesForLegacyTask(t.Context(), parseUUID(f.session))
	if err != nil || len(legacy) != 0 {
		t.Fatalf("supplements entered legacy input: %v %v", legacy, err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, f.session); n != 1 {
		t.Fatalf("created extra tasks: %d", n)
	}
	var history []ChatMessageResponse
	testutil.Call(t, testHandler.ListChatMessages, withURLParam(chatSupplementAPIRequest(t, "GET", "/messages", nil), "sessionId", f.session)).Want(200).JSON(&history)
	if len(history) != 2 || history[0].Supplement.Status != "delivered" || history[1].Supplement.Status != "delivered" {
		t.Fatalf("history: %+v", history)
	}
}

func TestChatSupplementHistoryDistinguishesSendingAndUncertain(t *testing.T) {
	f := newChatSupplementFixture(t, "codex", true)
	var sent ChatMessageResponse
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "guidance").Want(201).JSON(&sent)
	assertHistory := func(want string) {
		t.Helper()
		var history []ChatMessageResponse
		testutil.Call(t, testHandler.ListChatMessages, withURLParam(chatSupplementAPIRequest(t, "GET", "/messages", nil), "sessionId", f.session)).Want(200).JSON(&history)
		if len(history) != 1 || history[0].Supplement == nil || history[0].Supplement.Status != want {
			t.Fatalf("history=%+v, want receipt status %s", history, want)
		}
	}
	assertHistory("pending")
	testutil.Call(t, testHandler.ClaimTaskSupplement, withURLParam(newOwnerDaemonRequest("POST", "/claim", map[string]any{}), "taskId", f.task)).Want(200)
	assertHistory("delivering")
	testutil.Call(t, testHandler.AckTaskSupplement, withURLParams(newOwnerDaemonRequest("POST", "/ack", map[string]any{"delivered": false, "error": "delivery_unknown"}), "taskId", f.task, "commentId", sent.ID)).Want(200)
	assertHistory("unknown")
	testutil.Call(t, testHandler.AckTaskSupplement, withURLParams(newOwnerDaemonRequest("POST", "/ack", map[string]bool{"delivered": true}), "taskId", f.task, "commentId", sent.ID)).Want(200)
	assertHistory("delivered")
}

func TestChatSupplementLostAckAndTerminalSettlement(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			f := newChatSupplementFixture(t, "codex", true)
			var sent ChatMessageResponse
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "claimed").Want(201).JSON(&sent)
			claimed, err := testHandler.Queries.ClaimNextChatSupplement(t.Context(), parseUUID(f.task))
			if err != nil {
				t.Fatal(err)
			}
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-222222222222", "not claimed").Want(201)
			// Reconnection cannot claim a previously attempted delivery again.
			next, err := testHandler.Queries.ClaimNextChatSupplement(t.Context(), parseUUID(f.task))
			if err != nil || next.MessageID == claimed.MessageID {
				t.Fatalf("replayed claim: %+v %v", next, err)
			}
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-333333333333", "still pending").Want(201)
			tx, err := testPool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			q := testHandler.Queries.WithTx(tx)
			if _, err = tx.Exec(t.Context(), `UPDATE agent_task_queue SET status=$2 WHERE id=$1`, f.task, terminal); err != nil {
				t.Fatal(err)
			}
			task, err := q.GetAgentTask(t.Context(), parseUUID(f.task))
			if err != nil {
				t.Fatal(err)
			}
			if err = service.SettleTerminalTaskState(t.Context(), q, task); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE task_id=$1 AND status='unknown'`, f.task); n != 2 {
				t.Fatalf("uncertain receipts=%d", n)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE task_id=$1 AND status='failed'`, f.task); n != 1 {
				t.Fatalf("failed receipts=%d", n)
			}
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-444444444444", "too late").Want(409)
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "claimed").Want(200)
			testutil.Call(t, testHandler.AckTaskSupplement, withURLParams(newOwnerDaemonRequest("POST", "/ack", map[string]bool{"delivered": true}), "taskId", f.task, "commentId", sent.ID)).Want(200)
			var count int
			if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM chat_message WHERE task_id=$1`, f.task).Scan(&count); err != nil || count != 3 {
				t.Fatalf("history changed: %d %v", count, err)
			}
		})
	}
}

func TestChatSupplementPermissionsAndIsolation(t *testing.T) {
	f := newChatSupplementFixture(t, "codex", true)
	other := newChatSupplementFixture(t, "codex", true)
	wrong := f
	wrong.task = other.task
	chatSupplementRequest(t, wrong, "0199a4e8-22ce-7b01-bba5-111111111111", "wrong chat").Want(404)
	otherUser := dbfx.User(t, "Other owner", "chat-supplement-other@example.test")
	dbfx.Exec(t, `UPDATE chat_session SET creator_id=$2 WHERE id=$1`, f.session, otherUser)
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "wrong owner").Want(403)
	dbfx.Exec(t, `UPDATE chat_session SET creator_id=$2 WHERE id=$1`, f.session, testUserID)
	dbfx.Exec(t, `UPDATE agent SET visibility='private',owner_id=$2 WHERE id=$1`, f.agent, otherUser)
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "revoked").Want(403)
	dbfx.Exec(t, `UPDATE agent SET visibility='workspace',owner_id=$2 WHERE id=$1`, f.agent, testUserID)
	for _, body := range []map[string]string{{"content": " ", "client_request_id": "0199a4e8-22ce-7b01-bba5-111111111111"}, {"content": "text", "client_request_id": "invalid"}} {
		testutil.Call(t, testHandler.CreateChatSupplement, withURLParams(chatSupplementAPIRequest(t, "POST", "/supplements", body), "sessionId", f.session, "taskId", f.task)).Want(400)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE task_id=$1`, f.task); n != 0 {
		t.Fatalf("unauthorized receipt: %d", n)
	}
}

func TestChatSupplementConcurrentDuplicateAndCompletion(t *testing.T) {
	f := newChatSupplementFixture(t, "codex", true)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() {
			req := withURLParams(chatSupplementAPIRequest(t, "POST", "/supplements", map[string]string{"content": "once", "client_request_id": "0199a4e8-22ce-7b01-bba5-111111111111"}), "sessionId", f.session, "taskId", f.task)
			w := httptest.NewRecorder()
			testHandler.CreateChatSupplement(w, req)
			codes <- w.Code
		})
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 && code != 201 {
			t.Fatalf("duplicate status: %d", code)
		}
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1`, f.session); n != 1 {
		t.Fatalf("duplicate messages=%d", n)
	}
	tx, err := testPool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err = tx.Exec(t.Context(), `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		testHandler.CreateChatSupplement(w, withURLParams(chatSupplementAPIRequest(t, "POST", "/supplements", map[string]string{"content": "late", "client_request_id": "0199a4e8-22ce-7b01-bba5-222222222222"}), "sessionId", f.session, "taskId", f.task))
		done <- w
	}()
	task, err := testHandler.Queries.WithTx(tx).GetAgentTask(t.Context(), parseUUID(f.task))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.SettleTerminalTaskState(t.Context(), testHandler.Queries.WithTx(tx), task); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-done:
		if w.Code != 409 {
			t.Fatalf("completion race status=%d %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("completion race deadlocked")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1`, f.session); n != 1 {
		t.Fatalf("race created messages=%d", n)
	}
}

func TestChatSupplementAckFailuresAreHonest(t *testing.T) {
	for _, tc := range []struct{ reason, status string }{{"provider_rejected", "failed"}, {"turn_not_started", "failed"}, {"timeout", "unknown"}, {"delivery_unknown", "unknown"}} {
		t.Run(tc.reason, func(t *testing.T) {
			f := newChatSupplementFixture(t, "codex", true)
			var sent ChatMessageResponse
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "guidance").Want(201).JSON(&sent)
			if _, err := testHandler.Queries.ClaimNextChatSupplement(t.Context(), parseUUID(f.task)); err != nil {
				t.Fatal(err)
			}
			var receipt ChatSupplementReceipt
			testutil.Call(t, testHandler.AckTaskSupplement, withURLParams(newOwnerDaemonRequest("POST", "/ack", map[string]any{"delivered": false, "error": tc.reason}), "taskId", f.task, "commentId", sent.ID)).Want(200).JSON(&receipt)
			if receipt.Status != tc.status {
				t.Fatalf("receipt=%+v", receipt)
			}
			if _, err := testHandler.Queries.ClaimNextChatSupplement(t.Context(), parseUUID(f.task)); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("failure replayed: %v", err)
			}
		})
	}
}

func TestChatSupplementDeleteAndArchive(t *testing.T) {
	for _, action := range []string{"archive", "delete"} {
		t.Run(action, func(t *testing.T) {
			f := newChatSupplementFixture(t, "codex", true)
			chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "keep a receipt").Want(201)
			if action == "archive" {
				dbfx.Exec(t, `UPDATE chat_session SET status='archived' WHERE id=$1`, f.session)
				chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-222222222222", "reject after archive").Want(409)
				if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1`, f.session); n != 1 {
					t.Fatalf("archive created input: %d", n)
				}
			} else {
				testutil.Call(t, testHandler.DeleteChatSession, withURLParam(chatSupplementAPIRequest(t, "DELETE", "/session", nil), "sessionId", f.session)).Want(204)
				if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE chat_session_id=$1`, f.session); n != 0 {
					t.Fatalf("orphan receipts: %d", n)
				}
			}
		})
	}
}

func TestChatSupplementStopRestoresOnlyOriginalInput(t *testing.T) {
	f := newChatSupplementFixture(t, "codex", true)
	dbfx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1`, f.task)
	primary := dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": f.session, "task_id": f.task, "role": "user", "content": "original prompt"})
	var sent ChatMessageResponse
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "supplement remains historical").Want(201).JSON(&sent)
	var cancelled CancelTaskByUserResponse
	testutil.Call(t, testHandler.CancelTaskByUser, cancelTaskByUserRequest(t, testUserID, f.task)).Want(200).JSON(&cancelled)
	if cancelled.CancelledChatMessage == nil || cancelled.CancelledChatMessage.MessageID != primary || cancelled.CancelledChatMessage.Content != "original prompt" {
		t.Fatalf("wrong restored input: %+v", cancelled.CancelledChatMessage)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE id=$1`, sent.ID); n != 1 {
		t.Fatal("cancellation deleted guidance history")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE message_id=$1 AND status='failed'`, sent.ID); n != 1 {
		t.Fatal("pending receipt was not settled")
	}
}
