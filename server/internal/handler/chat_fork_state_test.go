package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatForkReservationAndPublication(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	sourceID := parseUUID(dbfx.ChatSession(t, agentID, testutil.Cols{"runtime_id": runtimeID, "session_id": "source-session"}))
	destinationID := parseUUID("019fe3de-56a2-7792-8913-ee53664dfa0e")
	taskID := parseUUID("019fe3de-56a2-7792-8913-ee53664dfa0f")
	tx, err := dbfx.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	if _, err := q.LockChatSessionForEnqueue(ctx, sourceID); err != nil {
		t.Fatal(err)
	}

	reserve := db.ReserveChatForkSnapshotParams{SourceChatID: sourceID, TaskID: taskID,
		WorkspaceID: parseUUID(dbfx.WorkspaceID), CreatorID: parseUUID(dbfx.UserID)}
	source, err := q.ReserveChatForkSnapshot(ctx, reserve)
	if err != nil || source.ForkSnapshotTaskID != taskID || source.SessionID.String != "source-session" {
		t.Fatalf("reserve source=%+v error=%v", source, err)
	}
	if _, err := q.ReserveChatForkSnapshot(ctx, reserve); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate reservation returned %v", err)
	}
	child, err := q.CreatePendingChatFork(ctx, db.CreatePendingChatForkParams{SourceChatID: sourceID, DestinationChatID: destinationID, TaskID: taskID})
	if err != nil || child.SessionID.Valid || child.ForkedFromChatID != sourceID {
		t.Fatalf("pending child=%+v error=%v", child, err)
	}
	public := db.GetPublicChatSessionInWorkspaceParams{ID: destinationID, WorkspaceID: reserve.WorkspaceID}
	if _, err := q.GetPublicChatSessionInWorkspace(ctx, public); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pending destination was exposed: %v", err)
	}
	if _, err := q.GetPendingChatForkSource(ctx, db.GetPendingChatForkSourceParams{DestinationChatID: destinationID, TaskID: taskID}); err != nil {
		t.Fatal(err)
	}
	handler := Handler{Queries: q}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	runtime := db.AgentRuntime{ID: parseUUID(runtimeID), Provider: "codex", RuntimeMode: "local"}
	claimed := db.AgentTaskQueue{ID: taskID, RuntimeID: runtime.ID, AgentID: parseUUID(agentID)}
	var wire AgentTaskResponse
	if _, failure := handler.applyChatForkClaim(req, &claimed, runtime, child, &wire); failure == nil {
		t.Fatal("legacy daemon was given the pending fork")
	}
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCodexChatForkV1)
	if handled, failure := handler.applyChatForkClaim(req, &claimed, runtime, child, &wire); !handled || failure != nil ||
		wire.CodexChatFork == nil || wire.CodexChatFork.SourceSessionID != "source-session" {
		t.Fatalf("fork claim handled=%v failure=%v descriptor=%+v", handled, failure, wire.CodexChatFork)
	}
	ready, err := q.CompletePendingChatFork(ctx, db.CompletePendingChatForkParams{DestinationChatID: destinationID, TaskID: taskID,
		SessionID: pgtype.Text{String: "child-session", Valid: true}})
	if err != nil || ready.ForkInitializationTaskID.Valid || ready.SessionID.String != "child-session" {
		t.Fatalf("published chat=%+v error=%v", ready, err)
	}
	wire = AgentTaskResponse{}
	if handled, failure := handler.applyChatForkClaim(req, &claimed, runtime, ready, &wire); handled || failure != nil || !wire.RequireNativeResume {
		t.Fatalf("published continuation handled=%v failure=%v strict=%v", handled, failure, wire.RequireNativeResume)
	}
	if _, err := q.GetPublicChatSessionInWorkspace(ctx, public); err != nil {
		t.Fatal(err)
	}
	if count, err := q.ReleaseChatForkSnapshot(ctx, db.ReleaseChatForkSnapshotParams{SourceChatID: sourceID, TaskID: taskID}); err != nil || count != 1 {
		t.Fatalf("release count=%d error=%v", count, err)
	}
	original, err := q.GetChatSession(ctx, sourceID)
	if err != nil || original.ForkSnapshotTaskID.Valid || original.SessionID.String != "source-session" {
		t.Fatalf("original source=%+v error=%v", original, err)
	}
}

func TestQueueChatForkReservesSourceAndQueuesOriginalRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	sourceID := parseUUID(dbfx.ChatSession(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "session_id": "source-session",
	}))
	source, err := testHandler.Queries.GetChatSession(ctx, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := testHandler.queueChatFork(ctx, source, source.CreatorID); !errors.Is(err, errChatForkUnavailable) {
		t.Fatalf("unsupported runtime accepted fork request: %v", err)
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET provider='codex', metadata=$1 WHERE id=$2`,
		[]byte(`{"capabilities":["codex-chat-fork-v1"]}`), runtimeID)
	child, task, err := testHandler.queueChatFork(ctx, source, source.CreatorID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE id=$1`, task.ID)
		dbfx.Exec(t, `DELETE FROM chat_session WHERE id=$1`, child.ID)
	})
	if task.Status != "queued" || task.RuntimeID != source.RuntimeID || task.ChatSessionID != child.ID ||
		child.ForkInitializationTaskID != task.ID {
		t.Fatalf("child=%+v queued task=%+v", child, task)
	}
	if _, _, err := testHandler.queueChatFork(ctx, source, source.CreatorID); !errors.Is(err, errChatForkUnavailable) {
		t.Fatalf("overlapping fork accepted: %v", err)
	}
	if _, err := testHandler.TaskService.EnqueueChatTask(ctx, source, source.CreatorID, false); !errors.Is(err, service.ErrChatSessionForkPending) {
		t.Fatalf("source task enqueue during snapshot returned %v", err)
	}
}

func TestNativeChatForkTerminalReportsSettleReservation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	dbfx.Exec(t, `UPDATE agent_runtime SET provider='codex', metadata=$1 WHERE id=$2`,
		[]byte(`{"capabilities":["codex-chat-fork-v1"]}`), runtimeID)
	for _, name := range []string{"completed", "failed", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			sourceID := parseUUID(dbfx.ChatSession(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "session_id": "source-session",
			}))
			source, err := testHandler.Queries.GetChatSession(ctx, sourceID)
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Exec(t, `INSERT INTO chat_message (chat_session_id, role, content, created_at) VALUES
                ($1, 'user', 'original question', now() - interval '2 seconds'),
                ($1, 'assistant', 'original reply', now() - interval '1 second')`, sourceID)
			dbfx.Exec(t, `INSERT INTO chat_message (chat_session_id, role, content, message_kind) VALUES
                ($1, 'user', 'internal channel command', 'channel_command')`, sourceID)
			attachmentID := dbfx.Insert(t, "attachment", testutil.Cols{
				"workspace_id": source.WorkspaceID, "chat_session_id": sourceID,
				"uploader_type": "member", "uploader_id": source.CreatorID,
				"filename": "context.txt", "url": "https://cdn.example/context.txt",
				"content_type": "text/plain", "size_bytes": 12,
			})
			dbfx.Exec(t, `UPDATE attachment SET chat_message_id = (
				SELECT id FROM chat_message WHERE chat_session_id=$2 AND content='original question'
			) WHERE id=$1`, attachmentID, sourceID)
			child, task, err := testHandler.queueChatFork(ctx, source, source.CreatorID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE id=$1`, task.ID)
				dbfx.Exec(t, `DELETE FROM chat_session WHERE id=$1`, child.ID)
			})
			dbfx.Exec(t, `INSERT INTO chat_message (chat_session_id, role, content, created_at) VALUES
                ($1, 'user', 'later source-only input', now() + interval '1 second')`, sourceID)
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, task.ID)
			const childSession = "019fe3de-56a2-7792-8913-ee53664dfa0f"
			switch name {
			case "completed":
				_, transitioned, err := testHandler.TaskService.CompleteTaskWithTransition(ctx, task.ID, nil, childSession, "", "", false, "", "")
				if err != nil || !transitioned {
					t.Fatalf("complete transitioned=%v error=%v", transitioned, err)
				}
				stored, err := testHandler.Queries.GetChatSession(ctx, child.ID)
				if err != nil || stored.ForkInitializationTaskID.Valid || stored.SessionID.String != childSession {
					t.Fatalf("published=%+v error=%v", stored, err)
				}
				messages, err := testHandler.Queries.ListChatMessages(ctx, child.ID)
				if err != nil || len(messages) != 2 || messages[0].Content != "original question" ||
					messages[1].Content != "original reply" || messages[1].TaskID.Valid {
					t.Fatalf("branch history=%+v error=%v", messages, err)
				}
				files := testHandler.groupChatMessageAttachments(ctx, uuidToString(source.WorkspaceID), messages)[uuidToString(messages[0].ID)]
				if len(files) != 1 || files[0].ID != attachmentID || files[0].Filename != "context.txt" {
					t.Fatalf("branch attachment references=%+v, want original attachment %s", files, attachmentID)
				}
				dbfx.Exec(t, `DELETE FROM attachment WHERE id=$1`, attachmentID)
				if files := testHandler.groupChatMessageAttachments(ctx, uuidToString(source.WorkspaceID), messages); len(files) != 0 {
					t.Fatalf("branch retained deleted attachment: %+v", files)
				}
				if _, err := testHandler.Queries.GetChatMessageByTaskAssistant(ctx, task.ID); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("fork added an assistant outcome: %v", err)
				}
			case "failed":
				_, transitioned, err := testHandler.TaskService.FailTaskWithTransition(ctx, task.ID, "fork failed", "", "", "", "", false, "", "")
				if err != nil || !transitioned {
					t.Fatalf("fail transitioned=%v error=%v", transitioned, err)
				}
				if _, err := testHandler.Queries.GetChatSession(ctx, child.ID); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("failed destination is still present: %v", err)
				}
			case "cancelled":
				result, err := testHandler.TaskService.CancelTaskWithResult(ctx, task.ID, service.CancelTaskOptions{})
				if err != nil || result.Task.Status != "cancelled" {
					t.Fatalf("cancel result=%+v error=%v", result, err)
				}
				if _, err := testHandler.Queries.GetChatSession(ctx, child.ID); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("cancelled destination is still present: %v", err)
				}
				_, transitioned, err := testHandler.TaskService.CompleteTaskWithTransition(ctx, task.ID, nil, childSession, "", "", false, "", "")
				if err != nil || transitioned {
					t.Fatalf("late completion transitioned=%v error=%v", transitioned, err)
				}

			}
			original, err := testHandler.Queries.GetChatSession(ctx, sourceID)
			if err != nil || original.ForkSnapshotTaskID.Valid || original.SessionID.String != "source-session" {
				t.Fatalf("original=%+v error=%v", original, err)
			}
		})
	}
}

func TestChatForkEndpointsExposeOnlyPublishedDestination(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	dbfx.Exec(t, `UPDATE agent_runtime SET provider='codex', metadata=$1 WHERE id=$2`,
		[]byte(`{"capabilities":["codex-chat-fork-v1"]}`), runtimeID)
	sourceID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "session_id": "source-session",
	})
	dbfx.Exec(t, `UPDATE chat_session SET explicitly_created_at=now() WHERE id=$1`, sourceID)

	post := chatPendingCtxAs(t, withURLParam(newRequest(http.MethodPost, "/forks", nil), "sessionId", sourceID), testUserID)
	var accepted ChatForkResponse
	testutil.Call(t, testHandler.CreateChatFork, post).Want(http.StatusAccepted).JSON(&accepted)
	if accepted.TaskID == "" || accepted.Status != "queued" || accepted.ChatID != "" {
		t.Fatalf("acceptance=%+v", accepted)
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(accepted.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE id=$1`, task.ID)
		dbfx.Exec(t, `DELETE FROM chat_session WHERE id=$1`, task.ChatSessionID)
	})
	statusRequest := func(taskID string) *http.Request {
		req := withURLParam(newRequest(http.MethodGet, "/forks/"+taskID, nil), "sessionId", sourceID)
		chi.RouteContext(req.Context()).URLParams.Add("taskId", taskID)
		return chatPendingCtxAs(t, req, testUserID)
	}
	var pending ChatForkResponse
	testutil.Call(t, testHandler.GetChatFork, statusRequest(accepted.TaskID)).Want(http.StatusOK).JSON(&pending)
	if pending.Status != "queued" || pending.ChatID != "" {
		t.Fatalf("pending status exposed destination: %+v", pending)
	}
	testutil.Call(t, testHandler.GetChatFork, statusRequest("invalid")).Want(http.StatusBadRequest)

	dbfx.Exec(t, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, task.ID)
	const childSession = "019fe3de-56a2-7792-8913-ee53664dfa0f"
	if _, transitioned, err := testHandler.TaskService.CompleteTaskWithTransition(ctx, task.ID, nil, childSession, "", "", false, "", ""); err != nil || !transitioned {
		t.Fatalf("complete transitioned=%v error=%v", transitioned, err)
	}
	var ready ChatForkResponse
	testutil.Call(t, testHandler.GetChatFork, statusRequest(accepted.TaskID)).Want(http.StatusOK).JSON(&ready)
	if ready.Status != "completed" || ready.ChatID != uuidToString(task.ChatSessionID) {
		t.Fatalf("ready status=%+v", ready)
	}
}
