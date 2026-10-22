package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The MUL-6788 batch rewrite replaced the per-message ListAttachmentsByChatMessage
// loop in buildClaimedTaskResponse with one ListAttachmentsByChatMessageIDs read.
// These tests pin the two behaviours the review called out: the grouped output
// must reproduce the previous append order (messages in their own order, each
// message's attachments in created_at order), and a FAILED batch read must fail
// closed — the agent's only handle for user files is the attachment ID, so a
// swallowed error would silently start the run with files missing and no
// redelivery.

// claimAttachmentsResponse decodes just the attachment slice off a claim.
type claimAttachmentsResponse struct {
	Task *struct {
		ChatMessage            string `json:"chat_message"`
		ChatMessageAttachments []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		} `json:"chat_message_attachments"`
	} `json:"task"`
}

// seedLegacyChatUserMessage inserts a user message on a legacy (NULL
// chat_input_task_id) chat task with an explicit created_at so message order is
// deterministic, and returns its id.
func seedLegacyChatUserMessage(t *testing.T, sessionID, content, createdAt string) string {
	t.Helper()
	return dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": sessionID,
		"role":            "user",
		"content":         content,
		"created_at":      createdAt,
	})
}

// seedMessageAttachment binds an attachment to a chat message with an explicit
// created_at, so a test can interleave two messages' attachment timestamps and
// prove the per-message grouping keeps each message's attachments in
// created_at order regardless of the global batch ordering.
func seedMessageAttachment(t *testing.T, sessionID, messageID, filename, createdAt string) string {
	t.Helper()
	return dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"chat_session_id": sessionID,
		"chat_message_id": messageID,
		"uploader_type":   "member",
		"uploader_id":     testUserID,
		"filename":        filename,
		"url":             "https://cdn.example/" + filename,
		"content_type":    "image/png",
		"size_bytes":      10,
		"created_at":      createdAt,
	})
}

// claimForRuntimeExpecting drives the single-runtime claim through handler h and
// returns the response recorder wrapper asserting the wanted status.
func claimForRuntimeExpecting(t *testing.T, h *Handler, runtimeID, daemonID string, want int) *testutil.Response {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	return testutil.Call(t, h.ClaimTaskByRuntime, req).Want(want)
}

// TestClaimChatAttachmentsPreserveMessageThenCreatedAtOrder seeds two unanswered
// messages whose attachment created_at values interleave globally, then claims
// and asserts the response lists attachments in message order first and, within
// a message, in created_at order — the exact append order the per-message loop
// produced before batching.
func TestClaimChatAttachmentsPreserveMessageThenCreatedAtOrder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "attachment order chat")

	// Two user messages, m1 before m2. Attachment timestamps interleave across
	// messages (m1.a=00:01, m2.a=00:02, m1.b=00:03, m2.b=00:04) so a naive
	// "global created_at" flatten would produce m1a,m2a,m1b,m2b — the grouping
	// must instead yield m1a,m1b,m2a,m2b.
	m1 := seedLegacyChatUserMessage(t, sessionID, "first", "2020-01-01T00:00:01Z")
	m2 := seedLegacyChatUserMessage(t, sessionID, "second", "2020-01-01T00:00:05Z")
	m1a := seedMessageAttachment(t, sessionID, m1, "m1a.png", "2020-01-01T00:00:01Z")
	m2a := seedMessageAttachment(t, sessionID, m2, "m2a.png", "2020-01-01T00:00:02Z")
	m1b := seedMessageAttachment(t, sessionID, m1, "m1b.png", "2020-01-01T00:00:03Z")
	m2b := seedMessageAttachment(t, sessionID, m2, "m2b.png", "2020-01-01T00:00:04Z")

	// A legacy (NULL chat_input_task_id) queued chat task for this session.
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":      runtimeID,
		"chat_session_id": sessionID,
		"status":          "queued",
	})

	var resp claimAttachmentsResponse
	claimForRuntimeExpecting(t, testHandler, runtimeID, daemonID, http.StatusOK).JSON(&resp)
	if resp.Task == nil {
		t.Fatal("expected a claimed task")
	}

	gotIDs := make([]string, 0, len(resp.Task.ChatMessageAttachments))
	for _, a := range resp.Task.ChatMessageAttachments {
		gotIDs = append(gotIDs, a.ID)
	}
	want := []string{m1a, m1b, m2a, m2b}
	if strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Errorf("attachment order = %v, want %v (message order, then per-message created_at)", gotIDs, want)
	}
}

// chatAttachmentBatchFailDBTX passes every statement through to the real pool
// EXCEPT the batched chat-message attachment read, which it fails.
type chatAttachmentBatchFailDBTX struct{ inner db.DBTX }

func (f chatAttachmentBatchFailDBTX) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f.inner.Exec(ctx, sql, args...)
}

func (f chatAttachmentBatchFailDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM attachment") && strings.Contains(sql, "chat_message_id = ANY(") {
		return nil, errInjectedChatAttachmentRead
	}
	return f.inner.Query(ctx, sql, args...)
}

func (f chatAttachmentBatchFailDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.inner.QueryRow(ctx, sql, args...)
}

var errInjectedChatAttachmentRead = errors.New("injected chat attachment batch read failure")

func newChatAttachmentReadFailureHandler(t *testing.T) *Handler {
	t.Helper()
	return New(
		db.New(chatAttachmentBatchFailDBTX{inner: testPool}),
		testPool,
		testHandler.Hub,
		testHandler.Bus,
		testHandler.EmailService,
		nil,
		nil,
		analytics.NoopClient{},
		Config{},
	)
}

// TestClaimChatAttachmentBatchReadErrorFailsClosed pins the core review fix on
// the claim path: a failed attachment batch read must reject the claim with 500
// and leave the task eligible for the stale-dispatched reclaim. The claim UPDATE
// moves the task to 'dispatched' with started_at still NULL; the failure branch
// must leave it exactly there (not cancelled, not started) so reclaim can
// redeliver it once the recovery window lapses.
func TestClaimChatAttachmentBatchReadErrorFailsClosed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "attachment error chat")

	m1 := seedLegacyChatUserMessage(t, sessionID, "with a file", "2020-01-01T00:00:01Z")
	seedMessageAttachment(t, sessionID, m1, "doc.png", "2020-01-01T00:00:01Z")

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":      runtimeID,
		"chat_session_id": sessionID,
		"status":          "queued",
	})

	h := newChatAttachmentReadFailureHandler(t)
	claimForRuntimeExpecting(t, h, runtimeID, daemonID, http.StatusInternalServerError)

	// The task must be parked exactly at the claim's dispatched state so the
	// stale-dispatched reclaim can redeliver it — not cancelled, and not moved
	// on to started/running.
	var status string
	var startedAt *string
	dbfx.QueryRow(t, `SELECT status, started_at::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &startedAt)
	if status != "dispatched" {
		t.Errorf("task status = %q, want dispatched (parked for stale-dispatched reclaim)", status)
	}
	if startedAt != nil {
		t.Errorf("task started_at = %v, want NULL (the failed claim must not have started the run)", *startedAt)
	}
}
