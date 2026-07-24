package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestBuildQuickCreateContext_ChatSessionID(t *testing.T) {
	in := quickCreateEnqueueInput{
		WorkspaceID:   testUUID(1),
		RequesterID:   testUUID(2),
		ChatSessionID: testUUID(3),
		Prompt:        "analyze the branch",
	}
	got := buildQuickCreateContext(in)
	if got.Type != QuickCreateContextType {
		t.Fatalf("type = %q, want %q", got.Type, QuickCreateContextType)
	}
	if got.ChatSessionID != util.UUIDToString(testUUID(3)) {
		t.Fatalf("chat session id = %q, want %q", got.ChatSessionID, util.UUIDToString(testUUID(3)))
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"chat_session_id"`) {
		t.Fatalf("marshaled context missing chat_session_id: %s", raw)
	}
}

func TestBuildQuickCreateContext_ChatSessionWithAttachments(t *testing.T) {
	got := buildQuickCreateContext(quickCreateEnqueueInput{
		WorkspaceID:   testUUID(1),
		RequesterID:   testUUID(2),
		ChatSessionID: testUUID(3),
		Prompt:        "broken layout [image: image-1.png]",
		AttachmentIDs: []pgtype.UUID{testUUID(4), testUUID(5)},
	})
	if len(got.AttachmentIDs) != 2 ||
		got.AttachmentIDs[0] != util.UUIDToString(testUUID(4)) ||
		got.AttachmentIDs[1] != util.UUIDToString(testUUID(5)) {
		t.Fatalf("attachment ids = %v", got.AttachmentIDs)
	}
	if got.ChatSessionID != util.UUIDToString(testUUID(3)) {
		t.Fatalf("chat session id = %q", got.ChatSessionID)
	}
}

func TestBuildQuickCreateContext_OmitsEmptyChatSessionID(t *testing.T) {
	got := buildQuickCreateContext(quickCreateEnqueueInput{
		WorkspaceID: testUUID(1),
		RequesterID: testUUID(2),
		Prompt:      "p",
	})
	if got.ChatSessionID != "" {
		t.Fatalf("chat session id = %q, want empty", got.ChatSessionID)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "chat_session_id") {
		t.Fatalf("marshaled context should omit chat_session_id: %s", raw)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("短文本", 10); got != "短文本" {
		t.Fatalf("no-op truncate: %q", got)
	}
	long := strings.Repeat("析", 1200)
	got := truncateRunes(long, 1000)
	runes := []rune(got)
	if len(runes) != 1001 || runes[1000] != '…' {
		t.Fatalf("truncated len = %d, tail = %q", len(runes), string(runes[len(runes)-1]))
	}
}

func TestBuildQuickCreateDoneReply(t *testing.T) {
	got := buildQuickCreateDoneReply("MUL-42", "Login fix", "https://app.example.com/acme/issues/MUL-42")
	want := "✅ MUL-42 — Login fix\n\nhttps://app.example.com/acme/issues/MUL-42"
	if got != want {
		t.Fatalf("reply = %q, want %q — the issue body must stay off the conversation reply", got, want)
	}
}

func TestBuildQuickCreateDoneReply_MinimalFields(t *testing.T) {
	if got := buildQuickCreateDoneReply("MUL-42", "", ""); got != "✅ MUL-42" {
		t.Fatalf("reply = %q", got)
	}
}

func TestBuildQuickCreateFailedReply(t *testing.T) {
	if got := buildQuickCreateFailedReply("", ""); got != quickCreateChatFailedText {
		t.Fatalf("empty prompt reply = %q", got)
	}
	got := buildQuickCreateFailedReply("login broken\nsteps:\n1. open app", "")
	want := quickCreateChatFailedText + "\n\n> login broken\n> steps:\n> 1. open app"
	if got != want {
		t.Fatalf("multiline reply = %q, want %q — every prompt line needs the blockquote prefix", got, want)
	}
}

func TestBuildQuickCreateFailedReply_WithReason(t *testing.T) {
	got := buildQuickCreateFailedReply("What's up", "Active duplicate issue exists: ND-2 What's up (status: in_review).")
	want := quickCreateChatFailedReasonText +
		"\n\n> What's up" +
		"\n\nActive duplicate issue exists: ND-2 What's up (status: in_review)."
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
	if strings.Contains(got, "Please try again") {
		t.Fatal("a concrete reason must replace the generic retry advice — retrying may be pointless (e.g. duplicate)")
	}
}

func TestPostQuickCreateChatReply_NoSessionIsNoop(t *testing.T) {
	svc := &TaskService{} // nil Queries/Bus: must return before touching either
	svc.postQuickCreateChatReply(context.Background(), db.AgentTaskQueue{}, QuickCreateContext{}, "content")
	svc.postQuickCreateChatReply(context.Background(), db.AgentTaskQueue{}, QuickCreateContext{ChatSessionID: "not-a-uuid"}, "content")
}

// createQuickCreateReplyFixture provisions user → workspace → member →
// runtime → agent → chat_session, mirroring createClaimCapacityFixture.
func createQuickCreateReplyFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (workspaceID, userID, agentID, sessionID string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "QC Reply Test", fmt.Sprintf("qc-reply-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "QC Reply Test", fmt.Sprintf("qc-reply-%d", suffix), "quick-create reply test workspace", "QCR").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'qc_reply_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $3)
		RETURNING id
	`, workspaceID, "QC Reply Runtime", userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, "QC Reply Agent", runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'quick create reply test')
		RETURNING id
	`, workspaceID, agentID, userID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		pool.Exec(cctx, `DELETE FROM chat_message WHERE chat_session_id = $1`, sessionID)
		pool.Exec(cctx, `DELETE FROM chat_session WHERE id = $1`, sessionID)
		pool.Exec(cctx, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(cctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(cctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(cctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(cctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return workspaceID, userID, agentID, sessionID
}

func TestPostQuickCreateChatReply_AppendsAndPublishes(t *testing.T) {
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)
	ctx := context.Background()

	workspaceID, userID, agentID, sessionID := createQuickCreateReplyFixture(t, ctx, pool)

	bus := events.New()
	var got []events.Event
	bus.Subscribe(protocol.EventQuickCreateDone, func(e events.Event) { got = append(got, e) })

	svc := &TaskService{Queries: queries, Bus: bus}
	task := db.AgentTaskQueue{ID: testUUID(9), AgentID: util.MustParseUUID(agentID)}
	qc := QuickCreateContext{
		WorkspaceID:   workspaceID,
		RequesterID:   userID,
		ChatSessionID: sessionID,
	}
	const content = "✅ QCR-1 — done"
	svc.postQuickCreateChatReply(ctx, task, qc, content)

	var count int
	var taskID *string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), min(task_id::text) FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant' AND content = $2
	`, sessionID, content).Scan(&count, &taskID); err != nil {
		t.Fatalf("count chat_message: %v", err)
	}
	if count != 1 {
		t.Fatalf("assistant transcript rows = %d, want 1", count)
	}
	if taskID == nil || *taskID != util.UUIDToString(task.ID) {
		t.Fatalf("task_id = %v, want %s — the reply must link to the quick-create task", taskID, util.UUIDToString(task.ID))
	}
	// Unread is derived from the read cursor (chat_session.last_read_at) vs the
	// messages after it — the reply must land after the cursor so the session
	// shows as unread, like a normal assistant reply.
	var hasUnread bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM chat_message m
			JOIN chat_session s ON s.id = m.chat_session_id
			WHERE m.chat_session_id = $1 AND m.role = 'assistant' AND m.created_at > s.last_read_at
		)
	`, sessionID).Scan(&hasUnread); err != nil {
		t.Fatalf("read derived unread: %v", err)
	}
	if !hasUnread {
		t.Fatal("the reply must land after the read cursor so the session shows as unread")
	}
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	if got[0].ChatSessionID != sessionID {
		t.Fatalf("event chat_session_id = %q, want %q", got[0].ChatSessionID, sessionID)
	}
	payload, ok := got[0].Payload.(protocol.QuickCreateDonePayload)
	if !ok {
		t.Fatalf("payload type %T", got[0].Payload)
	}
	if payload.Content != content || payload.ChatSessionID != sessionID {
		t.Fatalf("payload = %+v", payload)
	}
}

// An inbox write failure must not suppress the conversation reply: the two
// notification channels are independent best-effort paths.
func TestNotifyQuickCreateFailed_InboxWriteFailureStillPostsChatReply(t *testing.T) {
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)
	ctx := context.Background()

	_, userID, agentID, sessionID := createQuickCreateReplyFixture(t, ctx, pool)

	bus := events.New()
	var done []events.Event
	bus.Subscribe(protocol.EventQuickCreateDone, func(e events.Event) { done = append(done, e) })

	svc := &TaskService{Queries: queries, Bus: bus}
	task := db.AgentTaskQueue{ID: testUUID(8), AgentID: util.MustParseUUID(agentID)}
	qc := QuickCreateContext{
		// A syntactically valid but nonexistent workspace: the inbox INSERT
		// fails its workspace FK, exercising the decoupled error branch.
		WorkspaceID:   util.UUIDToString(testUUID(77)),
		RequesterID:   userID,
		ChatSessionID: sessionID,
		Prompt:        "login broken",
	}
	svc.notifyQuickCreateFailed(ctx, task, qc, "agent crashed", "agent crashed")

	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant' AND content LIKE $2
	`, sessionID, quickCreateChatFailedReasonText+"%").Scan(&count); err != nil {
		t.Fatalf("count chat_message: %v", err)
	}
	if count != 1 {
		t.Fatalf("failure reply rows = %d, want 1 — the inbox failure must not gate the chat reply", count)
	}
	if len(done) != 1 {
		t.Fatalf("quick_create:done events = %d, want 1", len(done))
	}
}
