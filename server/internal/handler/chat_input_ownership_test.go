package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// setupDirectChatSession creates a runtime-guard agent (with its registered
// runtime + daemon) and a non-intro chat session for direct (web/mobile) chat
// ownership tests.
func setupDirectChatSession(t *testing.T, ctx context.Context, title string) (agentID, sessionID, runtimeID, daemonID string) {
	t.Helper()
	agentID, runtimeID, daemonID = createRuntimeGuardAgent(t, ctx)
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, title).Scan(&sessionID); err != nil {
		t.Fatalf("setup: create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, sessionID) })
	return agentID, sessionID, runtimeID, daemonID
}

// sendDirectChat drives the transactional direct-send service path and returns
// the owning task id. The user message is created inside the same transaction
// with task_id = the new task, and the task owns its own input batch.
func sendDirectChat(t *testing.T, ctx context.Context, agentID, sessionID, content string) string {
	t.Helper()
	sess, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	ag, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	res, err := testHandler.TaskService.SendDirectChatMessage(ctx, sess, ag, parseUUID(testUserID), content, nil, "member", parseUUID(testUserID))
	if err != nil {
		t.Fatalf("SendDirectChatMessage: %v", err)
	}
	return uuidToString(res.Task.ID)
}

func markTaskRunning(t *testing.T, ctx context.Context, taskID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'running', started_at = now(), dispatched_at = now()
		WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
}

func insertTaskTranscriptRow(t *testing.T, ctx context.Context, taskID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_message (task_id, seq, type, content)
		VALUES ($1, 1, 'text', 'partial output')
	`, taskID); err != nil {
		t.Fatalf("insert task transcript row: %v", err)
	}
}

func completeResult(t *testing.T, output string) []byte {
	t.Helper()
	b, err := json.Marshal(TaskCompleteRequest{Output: output})
	if err != nil {
		t.Fatalf("marshal complete result: %v", err)
	}
	return b
}

func assertChatTranscriptContents(t *testing.T, messages []db.ChatMessage, want []string) {
	t.Helper()
	if len(messages) != len(want) {
		t.Fatalf("transcript length = %d, want %d: %+v", len(messages), len(want), messages)
	}
	for i, content := range want {
		if messages[i].Content != content {
			t.Fatalf("transcript[%d] = %q, want %q; transcript=%+v", i, messages[i].Content, content, messages)
		}
	}
}

// TestDirectChat_TaskOwnsItsOwnInputBatch is the core input-boundary contract
// (MUL-4351): each direct send owns exactly the user message it created. When
// U1→T1 and U2→T2 are both queued, T1's claim must deliver ONLY U1 (never
// "U1\n\nU2" the way the trailing-message selector would), and after T1
// completes, T2 delivers ONLY U2.
func TestDirectChat_TaskOwnsItsOwnInputBatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "ownership chat")

	t1 := sendDirectChat(t, ctx, agentID, sessionID, "看上海天气")
	t2 := sendDirectChat(t, ctx, agentID, sessionID, "还有青岛")

	// Both tasks own their own input batch.
	assertTaskInputOwner(t, ctx, t1, t1)
	assertTaskInputOwner(t, ctx, t2, t2)

	claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if claimed.ChatMessage != "看上海天气" {
		t.Fatalf("first claim must deliver ONLY its owned message; got %q", claimed.ChatMessage)
	}

	// Complete the first task (the older T1, claimed first by created_at order),
	// then the next claim must deliver only the other message rather than a
	// coalesced pair. The claim leaves T1 dispatched; move it to running so the
	// completion CAS (WHERE status='running') applies.
	markTaskRunning(t, ctx, t1)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t1), completeResult(t, "上海晴"), "", "", "", false, ""); err != nil {
		t.Fatalf("complete first task: %v", err)
	}
	claimed2 := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if claimed2.ChatMessage != "还有青岛" {
		t.Fatalf("second claim must deliver ONLY the second owned message; got %q", claimed2.ChatMessage)
	}
}

// TestDirectChat_ClaimKeepsQueuedTurnsPairedWithReplies covers the follow-up
// transcript regression from MUL-5751. The idle positional head is visible
// before claim, while later queued user rows remain hidden. When a predecessor
// settles, its reply and the newly-visible follow-up must become atomically
// ordered during the completion-to-claim window; claiming that already-visible
// head must not move it again. The same server-authoritative order must drive
// both the legacy full list and every cursor page.
func TestDirectChat_ClaimKeepsQueuedTurnsPairedWithReplies(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "queued transcript order")

	t1 := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	t2 := sendDirectChat(t, ctx, agentID, sessionID, "user B")
	t3 := sendDirectChat(t, ctx, agentID, sessionID, "user C")

	inputABeforeClaim, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t1))
	if err != nil || len(inputABeforeClaim) != 1 {
		t.Fatalf("load A input before claim: messages=%+v err=%v", inputABeforeClaim, err)
	}
	transcript, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list transcript before claiming A: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A"})

	claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	inputAAfterClaim, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t1))
	if err != nil || len(inputAAfterClaim) != 1 {
		t.Fatalf("load A input after claim: messages=%+v err=%v", inputAAfterClaim, err)
	}
	if !inputAAfterClaim[0].CreatedAt.Time.Equal(inputABeforeClaim[0].CreatedAt.Time) {
		t.Fatalf("claim moved already-visible A from %s to %s", inputABeforeClaim[0].CreatedAt.Time, inputAAfterClaim[0].CreatedAt.Time)
	}
	markTaskRunning(t, ctx, t1)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t1), completeResult(t, "assistant A"), "", "", "", false, ""); err != nil {
		t.Fatalf("complete turn A: %v", err)
	}
	transcript, err = testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list transcript before claiming B: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "assistant A", "user B"})

	queuedBeforeClaim, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(t2))
	if err != nil {
		t.Fatalf("load queued turn B: %v", err)
	}
	inputBBeforeClaim, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t2))
	if err != nil || len(inputBBeforeClaim) != 1 {
		t.Fatalf("load B input before claim: messages=%+v err=%v", inputBBeforeClaim, err)
	}
	claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	claimedAfter, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(t2))
	if err != nil {
		t.Fatalf("load claimed turn B: %v", err)
	}
	if !claimedAfter.CreatedAt.Time.Equal(queuedBeforeClaim.CreatedAt.Time) {
		t.Fatalf("claim changed task queue created_at from %s to %s", queuedBeforeClaim.CreatedAt.Time, claimedAfter.CreatedAt.Time)
	}
	inputBAfterClaim, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t2))
	if err != nil || len(inputBAfterClaim) != 1 {
		t.Fatalf("load B input after claim: messages=%+v err=%v", inputBAfterClaim, err)
	}
	if !inputBAfterClaim[0].CreatedAt.Time.Equal(inputBBeforeClaim[0].CreatedAt.Time) {
		t.Fatalf("claim moved already-visible B from %s to %s", inputBBeforeClaim[0].CreatedAt.Time, inputBAfterClaim[0].CreatedAt.Time)
	}

	transcript, err = testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list transcript after claiming B: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "assistant A", "user B"})

	// Lost claim responses are re-delivered from dispatched without creating a
	// new transcript turn. Refreshing dispatched_at must not move user B again.
	inputBeforeReclaim, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t2))
	if err != nil || len(inputBeforeReclaim) != 1 {
		t.Fatalf("load B input before reclaim: messages=%+v err=%v", inputBeforeReclaim, err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET dispatched_at = now() - interval '1 hour',
		    prepare_lease_expires_at = now() - interval '1 minute'
		WHERE id = $1
	`, t2); err != nil {
		t.Fatalf("age B dispatch for reclaim: %v", err)
	}
	reclaimed, err := testHandler.Queries.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{
		RuntimeID:         parseUUID(runtimeID),
		ClaimRecoverySecs: 0,
		PrepareLeaseSecs:  60,
	})
	if err != nil {
		t.Fatalf("reclaim B dispatch: %v", err)
	}
	if uuidToString(reclaimed.ID) != t2 {
		t.Fatalf("reclaimed task = %s, want %s", uuidToString(reclaimed.ID), t2)
	}
	inputAfterReclaim, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t2))
	if err != nil || len(inputAfterReclaim) != 1 {
		t.Fatalf("load B input after reclaim: messages=%+v err=%v", inputAfterReclaim, err)
	}
	if !inputAfterReclaim[0].CreatedAt.Time.Equal(inputBeforeReclaim[0].CreatedAt.Time) {
		t.Fatalf("stale reclaim moved B from %s to %s", inputBeforeReclaim[0].CreatedAt.Time, inputAfterReclaim[0].CreatedAt.Time)
	}

	markTaskRunning(t, ctx, t2)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t2), completeResult(t, "assistant B"), "", "", "", false, ""); err != nil {
		t.Fatalf("complete turn B: %v", err)
	}
	transcript, err = testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list transcript before claiming C: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "assistant A", "user B", "assistant B", "user C"})

	claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	transcript, err = testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list transcript after claiming C: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "assistant A", "user B", "assistant B", "user C"})

	markTaskRunning(t, ctx, t3)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t3), completeResult(t, "assistant C"), "", "", "", false, ""); err != nil {
		t.Fatalf("complete turn C: %v", err)
	}
	transcript, err = testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list completed transcript: %v", err)
	}
	want := []string{"user A", "assistant A", "user B", "assistant B", "user C", "assistant C"}
	assertChatTranscriptContents(t, transcript, want)

	// Rebuild the same transcript from two-message cursor pages, detecting both
	// missing rows and duplicate ids while crossing every turn boundary.
	var before *ChatMessagesCursorResponse
	var paged []string
	seen := map[string]bool{}
	for {
		params := url.Values{"limit": {"2"}}
		if before != nil {
			params.Set("before_created_at", before.CreatedAt)
			params.Set("before_id", before.ID)
		}
		page := fetchChatMessagesPageForTest(t, sessionID, params)
		contents := make([]string, 0, len(page.Messages))
		for _, message := range page.Messages {
			if seen[message.ID] {
				t.Fatalf("cursor pagination returned duplicate message %s", message.ID)
			}
			seen[message.ID] = true
			contents = append(contents, message.Content)
		}
		paged = append(contents, paged...)
		if !page.HasMore {
			break
		}
		if page.NextCursor == nil {
			t.Fatal("page has_more without next_cursor")
		}
		before = page.NextCursor
	}
	if len(paged) != len(want) {
		t.Fatalf("paged transcript length = %d, want %d: %v", len(paged), len(want), paged)
	}
	for i, content := range want {
		if paged[i] != content {
			t.Fatalf("paged transcript[%d] = %q, want %q; transcript=%v", i, paged[i], content, paged)
		}
	}
}

// Retry tasks inherit the original chat_input_task_id and must never make the
// already-visible root user turn look new again when the child is claimed.
func TestDirectChat_RetryClaimDoesNotMoveOriginalInput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "retry transcript order")
	rootID := sendDirectChat(t, ctx, agentID, sessionID, "retry me")
	inputBefore, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(rootID))
	if err != nil || len(inputBefore) != 1 {
		t.Fatalf("load root input: messages=%+v err=%v", inputBefore, err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'failed', failure_reason = 'agent_error', completed_at = now()
		WHERE id = $1
	`, rootID); err != nil {
		t.Fatalf("fail root task: %v", err)
	}
	retry, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parseUUID(rootID)})
	if err != nil {
		t.Fatalf("create retry task: %v", err)
	}
	if !retry.ChatInputTaskID.Valid || uuidToString(retry.ChatInputTaskID) != rootID {
		t.Fatalf("retry input owner = %s, want root %s", uuidToString(retry.ChatInputTaskID), rootID)
	}
	claimed, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID))
	if err != nil || claimed == nil {
		t.Fatalf("claim retry task: task=%+v err=%v", claimed, err)
	}
	if uuidToString(claimed.ID) != uuidToString(retry.ID) {
		t.Fatalf("claimed task = %s, want retry %s", uuidToString(claimed.ID), uuidToString(retry.ID))
	}
	inputAfter, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(rootID))
	if err != nil || len(inputAfter) != 1 {
		t.Fatalf("reload root input: messages=%+v err=%v", inputAfter, err)
	}
	if !inputAfter[0].CreatedAt.Time.Equal(inputBefore[0].CreatedAt.Time) {
		t.Fatalf("retry claim moved root input from %s to %s", inputBefore[0].CreatedAt.Time, inputAfter[0].CreatedAt.Time)
	}
}

// TestDirectChat_RunningTaskDoesNotAbsorbNewMessage pins the acceptance case:
// while T1 is running, a message sent from another surface lands on T2 and is
// never folded into T1's already-sealed input batch.
func TestDirectChat_RunningTaskDoesNotAbsorbNewMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "no-absorb chat")

	t1 := sendDirectChat(t, ctx, agentID, sessionID, "first")
	markTaskRunning(t, ctx, t1)
	// A second message arrives mid-run and gets its own task.
	t2 := sendDirectChat(t, ctx, agentID, sessionID, "second")
	if t1 == t2 {
		t.Fatal("second send must create a distinct task")
	}

	// T1's owned batch is still exactly {first}; the mid-run message belongs to T2.
	owned1, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t1))
	if err != nil {
		t.Fatalf("list T1 owned input: %v", err)
	}
	if len(owned1) != 1 || owned1[0].Content != "first" {
		t.Fatalf("T1 must own only its own message; got %+v", msgContents(owned1))
	}
	owned2, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(t2))
	if err != nil {
		t.Fatalf("list T2 owned input: %v", err)
	}
	if len(owned2) != 1 || owned2[0].Content != "second" {
		t.Fatalf("T2 must own the mid-run message; got %+v", msgContents(owned2))
	}
}

// TestCompleteTask_ChatEmptyOutputWritesNoResponse: an empty final output is a
// visible, terminal no_response outcome — exactly one assistant row with
// message_kind='no_response' and a non-empty fallback body, task completed, and
// no auto-retry.
func TestCompleteTask_ChatEmptyOutputWritesNoResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "no-response chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "do a tool-only thing")
	markTaskRunning(t, ctx, taskID)

	// Whitespace-only output trims to empty → no_response.
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), completeResult(t, "   "), "", "", "", false, ""); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one assistant outcome, got %d", len(rows))
	}
	if rows[0].MessageKind != protocol.ChatMessageKindNoResponse {
		t.Fatalf("expected message_kind=no_response, got %q", rows[0].MessageKind)
	}
	if rows[0].Content == "" {
		t.Fatal("no_response row must carry a non-empty fallback body for old clients")
	}
	assertTaskStatus(t, ctx, taskID, "completed")
	assertNoRetryChild(t, ctx, taskID)
}

// TestCompleteTask_ChatNonEmptyOutputWritesMessage: a normal reply becomes a
// single ordinary assistant message.
func TestCompleteTask_ChatNonEmptyOutputWritesMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "reply chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "hello")
	markTaskRunning(t, ctx, taskID)

	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), completeResult(t, "hi there"), "sess-1", "/tmp/wd", "", false, ""); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one assistant message, got %d", len(rows))
	}
	if rows[0].MessageKind != protocol.ChatMessageKindMessage {
		t.Fatalf("expected message_kind=message, got %q", rows[0].MessageKind)
	}
	if rows[0].Content != "hi there" {
		t.Fatalf("expected content 'hi there', got %q", rows[0].Content)
	}
}

// TestCompleteTask_ChatCallbackIdempotent: a replayed completion callback must
// not write a second assistant outcome.
func TestCompleteTask_ChatCallbackIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "idempotent chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "hey")
	markTaskRunning(t, ctx, taskID)

	res := completeResult(t, "reply")
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), res, "", "", "", false, ""); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	// Replay: the status CAS fails, so this is an idempotent no-op success.
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), res, "", "", "", false, ""); err != nil {
		t.Fatalf("replayed complete must be idempotent success, got %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 1 {
		t.Fatalf("expected exactly one assistant outcome after replay, got %d", len(rows))
	}
}

// TestFailTask_ChatRetryInheritsInputOwnerAndPriority: a transient failure of a
// task-owned direct task creates a retry child that reuses the SAME input owner
// (so it reads the same user messages) and is queued at a bumped priority so it
// is claimed ahead of fresh chat tasks.
func TestFailTask_ChatRetryInheritsInputOwnerAndPriority(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "retry chat")
	rootID := sendDirectChat(t, ctx, agentID, sessionID, "root question")
	markTaskRunning(t, ctx, rootID)

	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(rootID), "runtime went away", "", "", "", "runtime_offline", false, ""); err != nil {
		t.Fatalf("fail task: %v", err)
	}

	var childID, childOwner string
	var childPriority, childAttempt int
	var childStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT id, chat_input_task_id, priority, status, attempt
		FROM agent_task_queue
		WHERE parent_task_id = $1
	`, rootID).Scan(&childID, &childOwner, &childPriority, &childStatus, &childAttempt); err != nil {
		t.Fatalf("expected a retry child, got: %v", err)
	}
	if childOwner != rootID {
		t.Fatalf("retry child must inherit the root input owner %s, got %s", rootID, childOwner)
	}
	if childPriority < 3 {
		t.Fatalf("chat retry must be bumped above fresh chat priority (2); got %d", childPriority)
	}
	if childStatus != "queued" {
		t.Fatalf("retry child must be queued, got %q", childStatus)
	}
	// The root direct task starts at attempt 1, so its first retry is attempt 2.
	var rootAttempt int
	if err := testPool.QueryRow(ctx, `SELECT attempt FROM agent_task_queue WHERE id = $1`, rootID).Scan(&rootAttempt); err != nil {
		t.Fatalf("read root attempt: %v", err)
	}
	if childAttempt != rootAttempt+1 {
		t.Fatalf("retry child attempt must be root+1 (%d), got %d", rootAttempt+1, childAttempt)
	}

	// The child reads the same input batch: the root's user message.
	owned, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(childOwner))
	if err != nil {
		t.Fatalf("list child owned input: %v", err)
	}
	if len(owned) != 1 || owned[0].Content != "root question" {
		t.Fatalf("retry child must read the root input batch; got %+v", msgContents(owned))
	}
}

// A non-retried failure is an assistant outcome just like a normal completion.
// The failure row and the next queued head must commit in transcript order so
// the successor cannot be claimed or rendered before the failure it follows.
func TestFailTask_ChatFailureKeepsNextTurnAfterOutcome(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "failed turn order")
	t1 := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	sendDirectChat(t, ctx, agentID, sessionID, "user B")
	markTaskRunning(t, ctx, t1)

	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(t1), "assistant failure", "", "", "", "agent_error.unknown", false, ""); err != nil {
		t.Fatalf("fail turn A: %v", err)
	}
	transcript, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list failed-turn transcript: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "assistant failure", "user B"})
}

// Cancellation finalization intentionally happens after the cancellation
// transaction (#5219), so this test pins the eventual transcript order rather
// than claiming cursor stability during that post-commit window. When output
// is already durable, finalization is synchronous and the visible result must
// still pair the cancelled turn with Stopped. before exposing its successor.
func TestCancelTask_ChatStoppedKeepsNextTurnAfterOutcome(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "cancelled turn order")
	t1 := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	sendDirectChat(t, ctx, agentID, sessionID, "user B")
	markTaskRunning(t, ctx, t1)
	insertTaskTranscriptRow(t, ctx, t1)

	if _, err := testHandler.TaskService.CancelTaskWithResult(ctx, parseUUID(t1), service.CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel turn A: %v", err)
	}
	transcript, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list synchronously cancelled transcript: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "Stopped.", "user B"})
}

// A started task with no durable output defers its empty/non-empty judgment
// until the daemon ack or sweeper. During that intentional transient, the next
// queued turn can be visible at its enqueue position. Once late output lands,
// deferred finalization must produce the same stable final order as the
// synchronous path.
func TestCancelTask_DeferredStoppedKeepsNextTurnAfterOutcome(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "deferred cancelled turn order")
	t1 := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	sendDirectChat(t, ctx, agentID, sessionID, "user B")
	markTaskRunning(t, ctx, t1)

	if _, err := testHandler.TaskService.CancelTaskWithResult(ctx, parseUUID(t1), service.CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel turn A: %v", err)
	}
	transcript, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list deferred cancellation transient: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "user B"})

	// Simulate output flushed after the cancellation commit, then settle the
	// deferred marker as the daemon acknowledgement or sweeper would.
	insertTaskTranscriptRow(t, ctx, t1)
	testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(t1))

	transcript, err = testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list deferred cancelled transcript: %v", err)
	}
	assertChatTranscriptContents(t, transcript, []string{"user A", "Stopped.", "user B"})
}

// ---- helpers ----

func msgContents(msgs []db.ChatMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func assistantRows(t *testing.T, ctx context.Context, sessionID string) []db.ChatMessage {
	t.Helper()
	all, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	var out []db.ChatMessage
	for _, m := range all {
		if m.Role == "assistant" {
			out = append(out, m)
		}
	}
	return out
}

func assertTaskInputOwner(t *testing.T, ctx context.Context, taskID, wantOwner string) {
	t.Helper()
	var owner string
	if err := testPool.QueryRow(ctx, `SELECT chat_input_task_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&owner); err != nil {
		t.Fatalf("read chat_input_task_id: %v", err)
	}
	if owner != wantOwner {
		t.Fatalf("task %s input owner = %s, want %s", taskID, owner, wantOwner)
	}
}

func assertTaskStatus(t *testing.T, ctx context.Context, taskID, want string) {
	t.Helper()
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != want {
		t.Fatalf("task %s status = %q, want %q", taskID, status, want)
	}
}

func assertNoRetryChild(t *testing.T, ctx context.Context, taskID string) {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, taskID).Scan(&n); err != nil {
		t.Fatalf("count retry children: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected no retry child for a completed no_response turn, got %d", n)
	}
}

// insertChannelChatTask creates a running chat task with chat_input_task_id NULL
// — the legacy/channel (Slack/Lark) shape — directly, bypassing the task-owned
// direct-send path.
func insertChannelChatTask(t *testing.T, ctx context.Context, agentID, runtimeID, sessionID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, dispatched_at)
		VALUES ($1, $2, $3, 'running', 2, now(), now())
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create channel chat task: %v", err)
	}
	return taskID
}

// insertSealedChannelChatTask creates a running channel-shaped chat task the
// way EnqueueChatTask now does: the task owns its input batch
// (chat_input_task_id = id) and the sealed user message carries the immutable
// channel_ingested stamp.
func insertSealedChannelChatTask(t *testing.T, ctx context.Context, agentID, runtimeID, sessionID, content string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, dispatched_at)
		VALUES ($1, $2, $3, 'running', 2, now(), now())
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create sealed channel chat task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("setup: set input owner: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id, channel_ingested)
		VALUES ($1, 'user', $2, $3, TRUE)
	`, sessionID, content, taskID); err != nil {
		t.Fatalf("setup: seal channel user message: %v", err)
	}
	return taskID
}

// TestCompleteTask_ChannelEmptyOutputWritesNoRow pins the MUL-4351 review fix
// for the LEGACY channel shape (chat_input_task_id NULL): an empty completion
// must NOT write an assistant row — so chat:done carries empty content and the
// Slack/Lark outbound keeps silently dropping it. The no_response fallback
// body must never reach an external channel. A non-empty channel completion
// still writes an ordinary message.
func TestCompleteTask_ChannelEmptyOutputWritesNoRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "channel-like chat")

	// Empty output → no row at all.
	emptyTask := insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(emptyTask), completeResult(t, "   "), "", "", "", false, ""); err != nil {
		t.Fatalf("complete channel task (empty): %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 0 {
		t.Fatalf("channel empty completion must write NO assistant row (Slack/Lark silent-drop), got %d", len(rows))
	}

	// Non-empty output → one ordinary message (kind 'message', not no_response).
	textTask := insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(textTask), completeResult(t, "channel reply"), "", "", "", false, ""); err != nil {
		t.Fatalf("complete channel task (text): %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("channel non-empty completion must write exactly one message, got %d", len(rows))
	}
	if rows[0].MessageKind != protocol.ChatMessageKindMessage || rows[0].Content != "channel reply" {
		t.Fatalf("channel message = kind %q content %q, want message/'channel reply'", rows[0].MessageKind, rows[0].Content)
	}
}

// TestCompleteTask_SealedChannelEmptyOutputWritesNoRow pins the silent-drop
// contract for the NEW channel shape: sealed channel tasks own their input
// batch (chat_input_task_id = id), so the discriminator is the immutable
// channel_ingested stamp, not a NULL owner. An empty completion must write no
// assistant row — the outbound patcher forwards any non-empty content
// verbatim, so a no_response fallback row would be pushed to Feishu/Slack.
func TestCompleteTask_SealedChannelEmptyOutputWritesNoRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "sealed channel chat")

	emptyTask := insertSealedChannelChatTask(t, ctx, agentID, runtimeID, sessionID, "[Image]")
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(emptyTask), completeResult(t, "   "), "", "", "", false, ""); err != nil {
		t.Fatalf("complete sealed channel task (empty): %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 0 {
		t.Fatalf("sealed channel empty completion must write NO assistant row, got %d", len(rows))
	}

	// Non-empty output still writes one ordinary message.
	textTask := insertSealedChannelChatTask(t, ctx, agentID, runtimeID, sessionID, "hello")
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(textTask), completeResult(t, "sealed channel reply"), "", "", "", false, ""); err != nil {
		t.Fatalf("complete sealed channel task (text): %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 || rows[0].MessageKind != protocol.ChatMessageKindMessage || rows[0].Content != "sealed channel reply" {
		t.Fatalf("sealed channel non-empty completion = %+v, want one ordinary message", rows)
	}
}

// TestCompleteTask_SealedChannelRetryEmptyOutputWritesNoRow: an auto-retry
// clone inherits its parent's chat_input_task_id while the sealed messages
// stay tagged with the parent's id. The provenance check must follow the
// inherited owner — keying it off the retry's own id would misread the task
// as direct and push the no_response fallback to the external channel.
func TestCompleteTask_SealedChannelRetryEmptyOutputWritesNoRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "sealed channel retry chat")

	parentTask := insertSealedChannelChatTask(t, ctx, agentID, runtimeID, sessionID, "[Video]")
	var retryTask string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, dispatched_at, parent_task_id, retry_of_task_id, chat_input_task_id)
		VALUES ($1, $2, $3, 'running', 2, now(), now(), $4, $4, $4)
		RETURNING id
	`, agentID, runtimeID, sessionID, parentTask).Scan(&retryTask); err != nil {
		t.Fatalf("setup: create retry clone: %v", err)
	}

	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(retryTask), completeResult(t, ""), "", "", "", false, ""); err != nil {
		t.Fatalf("complete sealed channel retry (empty): %v", err)
	}
	if rows := assistantRows(t, ctx, sessionID); len(rows) != 0 {
		t.Fatalf("sealed channel retry empty completion must write NO assistant row, got %d", len(rows))
	}
}

func TestCompleteTask_ChatQuickActions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "quick-actions chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "what next?")
	markTaskRunning(t, ctx, taskID)

	output := "Here is the plan.\n\n```quick-actions\n" +
		`[{"label":"Draft it","prompt":"Draft the complete plan","primary":true},` +
		`{"label":"Make a checklist","prompt":"Turn this into a checklist"}]` +
		"\n```"
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), completeResult(t, output), "", "", "", false, ""); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected one assistant message, got %d", len(rows))
	}
	if rows[0].Content != "Here is the plan." || rows[0].MessageKind != protocol.ChatMessageKindMessage {
		t.Fatalf("persisted reply = kind %q content %q", rows[0].MessageKind, rows[0].Content)
	}
	// The footer's actions are DISCARDED, not persisted: suggestions come from
	// the server-side pass now. Honouring an in-band footer would pin a
	// pre-upgrade provider session to the retired, lower-quality suggestions and
	// suppress the pass that replaces them.
	var actions []protocol.ChatQuickAction
	if err := json.Unmarshal(rows[0].QuickActions, &actions); err != nil {
		t.Fatalf("decode quick actions: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("in-band footer actions must not be persisted, got %+v", actions)
	}

	// An actions-only turn (quick-actions footer with no visible text) must NOT
	// become an empty-content message: older Desktop / mobile clients ignore
	// quick_actions and would render an empty bubble. It falls through to the
	// visible no_response fallback instead (MUL-4351).
	actionsOnlyTask := sendDirectChat(t, ctx, agentID, sessionID, "give me options only")
	markTaskRunning(t, ctx, actionsOnlyTask)
	actionsOnly := "```quick-actions\n[{\"label\":\"Continue\",\"prompt\":\"Continue the plan\"}]\n```"
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(actionsOnlyTask), completeResult(t, actionsOnly), "", "", "", false, ""); err != nil {
		t.Fatalf("complete actions-only task: %v", err)
	}
	rows = assistantRows(t, ctx, sessionID)
	if len(rows) != 2 || rows[1].MessageKind != protocol.ChatMessageKindNoResponse {
		t.Fatalf("actions-only outcome = %+v", rows)
	}
	if rows[1].Content == "" {
		t.Fatal("actions-only no_response row must carry a non-empty fallback body for old clients")
	}
	var droppedActions []protocol.ChatQuickAction
	if err := json.Unmarshal(rows[1].QuickActions, &droppedActions); err != nil {
		t.Fatalf("decode quick actions: %v", err)
	}
	if len(droppedActions) != 0 {
		t.Fatalf("actions-only no_response row must not carry quick actions, got %+v", droppedActions)
	}
}

func TestCompleteTask_ChatQuickActionsSupplement(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "suggest-pass chat")
	taskID := sendDirectChat(t, ctx, agentID, sessionID, "what next?")
	markTaskRunning(t, ctx, taskID)

	output := "Main reply.\n\n```quick-actions\n" +
		`[{"label":"From footer","prompt":"in-band fallback"}]` + "\n```"
	req := TaskCompleteRequest{Output: output}
	result, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal complete request: %v", err)
	}
	task, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), result, "", "", "", false, "")
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}

	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected one assistant message, got %d", len(rows))
	}
	if rows[0].Content != "Main reply." {
		t.Fatalf("footer must still be stripped from content, got %q", rows[0].Content)
	}

	raw := "```json\n" + `[{"label":"From pass","prompt":"suggested prompt","primary":true}]` + "\n```"
	if err := testHandler.TaskService.SupplementChatQuickActions(ctx, *task, raw, false); err != nil {
		t.Fatalf("supplement quick actions: %v", err)
	}
	rows = assistantRows(t, ctx, sessionID)
	var actions []protocol.ChatQuickAction
	if err := json.Unmarshal(rows[0].QuickActions, &actions); err != nil {
		t.Fatalf("decode quick actions: %v", err)
	}
	if len(actions) != 1 || actions[0].Label != "From pass" || !actions[0].Primary {
		t.Fatalf("supplement must attach the pass's actions, got %+v", actions)
	}

	// An empty supplement must not clobber existing actions (it only resolves
	// the pending placeholder client-side).
	if err := testHandler.TaskService.SupplementChatQuickActions(ctx, *task, "", false); err != nil {
		t.Fatalf("empty supplement: %v", err)
	}
	rows = assistantRows(t, ctx, sessionID)
	actions = nil
	if err := json.Unmarshal(rows[0].QuickActions, &actions); err != nil {
		t.Fatalf("decode quick actions after empty supplement: %v", err)
	}
	if len(actions) != 1 || actions[0].Label != "From pass" {
		t.Fatalf("empty supplement must keep existing actions, got %+v", actions)
	}
}

// stubChatQuickActionsLLM is an enabled ChatQuickActionsLLM that never dials an
// upstream, so the refresh gates below can be exercised on a deployment-shaped
// TaskService (RegenerateChatQuickActions refuses outright when the LLM layer
// is unconfigured).
type stubChatQuickActionsLLM struct {
	// lastPrompt records the rendered conversation the pass was given, so a
	// test can assert WHICH turn the suggestions were built from.
	lastPrompt *string
}

func (stubChatQuickActionsLLM) Enabled() bool { return true }

func (s stubChatQuickActionsLLM) GenerateJSON(_ context.Context, _, _, userPrompt string, _ float64, _ int64) (string, error) {
	if s.lastPrompt != nil {
		*s.lastPrompt = userPrompt
	}
	return `{"actions":[{"label":"Next","prompt":"do the next thing","primary":true}]}`, nil
}

// TestChatQuickActions_ContextAnchorsOnTargetTurn pins the anchoring contract:
// generation runs on a detached goroutine, so by the time it executes the
// session may already carry a newer turn. The context it builds must end at the
// assistant turn the pills are written to — not at whatever is newest when the
// read happens.
//
// Without the anchor, a user who types a follow-up in the second after the
// reply lands leaves the window ending on a user row with no reply to build on,
// and that turn silently never gets pills.
func TestChatQuickActions_ContextAnchorsOnTargetTurn(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "quick-actions anchor")

	taskID := sendDirectChat(t, ctx, agentID, sessionID, "first question")
	markTaskRunning(t, ctx, taskID)
	task, err := testHandler.TaskService.CompleteTask(
		ctx, parseUUID(taskID), completeResult(t, "ANCHOR REPLY"), "", "", "", false, "")
	if err != nil {
		t.Fatalf("complete turn 1: %v", err)
	}

	// The race: a newer user message lands before the pass reads the session.
	sendDirectChat(t, ctx, agentID, sessionID, "NEWER USER MESSAGE")

	var prompt string
	prev := testHandler.TaskService.QuickActions
	testHandler.TaskService.QuickActions = stubChatQuickActionsLLM{lastPrompt: &prompt}
	defer func() { testHandler.TaskService.QuickActions = prev }()

	if err := testHandler.TaskService.GenerateChatQuickActionsForTask(
		ctx, *task, service.ChatQuickActionsAutomatic); err != nil {
		t.Fatalf("generate quick actions: %v", err)
	}

	if !strings.Contains(prompt, "ANCHOR REPLY") {
		t.Fatalf("context must end at the target reply, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "NEWER USER MESSAGE") {
		t.Fatalf("context must exclude turns newer than the target, got:\n%s", prompt)
	}

	// And the pills land on the target turn, not the newer one.
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected one assistant row, got %d", len(rows))
	}
	var actions []protocol.ChatQuickAction
	if err := json.Unmarshal(rows[0].QuickActions, &actions); err != nil {
		t.Fatalf("decode quick actions: %v", err)
	}
	if len(actions) != 1 || actions[0].Label != "Next" {
		t.Fatalf("target turn quick actions = %+v", actions)
	}
}

// installStubQuickActions enables suggestion generation and returns the undo.
// Scoped tightly around the synchronous RegenerateChatQuickActions calls rather
// than installed for a whole test: CompleteTask starts a background pass when
// the feature is on, and a goroutine writing quick_actions mid-test would race
// these assertions.
func installStubQuickActions() func() {
	prev := testHandler.TaskService.QuickActions
	testHandler.TaskService.QuickActions = stubChatQuickActionsLLM{}
	return func() { testHandler.TaskService.QuickActions = prev }
}

// TestRegenerateChatQuickActions_StaleTargetRejected pins the ack-alignment
// contract (MUL-5149 review): a refresh names the turn it is refreshing, and
// the server enqueues it only while that turn is STILL the session's latest.
// Once a newer reply lands, refreshing the older turn is refused, so the
// client's pending marker never points at a turn the resulting
// chat:quick_actions event could not match.
func TestRegenerateChatQuickActions_StaleTargetRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "regen stale chat")

	// Turn 1: a resumable assistant reply (session_id + runtime bound) is latest.
	t1 := sendDirectChat(t, ctx, agentID, sessionID, "first question")
	markTaskRunning(t, ctx, t1)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t1), completeResult(t, "first reply"), "sess-1", "/tmp/wd", "", false, ""); err != nil {
		t.Fatalf("complete turn 1: %v", err)
	}
	session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected one assistant turn, got %d", len(rows))
	}
	m1 := rows[0].ID

	// Refreshing the current latest turn is accepted and targets exactly it.
	restore := installStubQuickActions()
	target, _, err := testHandler.TaskService.RegenerateChatQuickActions(ctx, session, m1)
	restore()
	if err != nil {
		t.Fatalf("regenerate latest turn: %v", err)
	}
	if target != m1 {
		t.Fatalf("regenerate target = %v, want the latest turn %v", target, m1)
	}

	// Turn 2 lands, so m1 is no longer the latest.
	t2 := sendDirectChat(t, ctx, agentID, sessionID, "second question")
	markTaskRunning(t, ctx, t2)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t2), completeResult(t, "second reply"), "sess-2", "/tmp/wd", "", false, ""); err != nil {
		t.Fatalf("complete turn 2: %v", err)
	}
	session, err = testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("reload session after turn 2: %v", err)
	}
	rows = assistantRows(t, ctx, sessionID)
	if len(rows) != 2 {
		t.Fatalf("expected two assistant turns, got %d", len(rows))
	}
	m2 := rows[1].ID

	// Refreshing the now-stale m1 is refused; refreshing the new latest m2 works.
	restore = installStubQuickActions()
	defer restore()
	if _, _, err := testHandler.TaskService.RegenerateChatQuickActions(ctx, session, m1); !errors.Is(err, service.ErrChatQuickActionsStale) {
		t.Fatalf("stale-target regenerate error = %v, want ErrChatQuickActionsStale", err)
	}
	if _, _, err := testHandler.TaskService.RegenerateChatQuickActions(ctx, session, m2); err != nil {
		t.Fatalf("regenerate new latest turn: %v", err)
	}
}

// TestRegenerateChatQuickActions_ActiveTurnRejected pins finding §1 of the
// MUL-5149 review: a newer reply that is queued/running but whose assistant row
// has not landed yet leaves the OLD turn as the latest-persisted one, so the
// stale check still passes on it. Regenerating then would resume the session
// after the newer turn advanced its provider state, attaching suggestions built
// from the newer context to the older turn. The in-flight task must refuse it.
func TestRegenerateChatQuickActions_ActiveTurnRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "regen busy chat")

	// Turn 1 completes with a resumable session → m1 is the latest assistant turn.
	t1 := sendDirectChat(t, ctx, agentID, sessionID, "first")
	markTaskRunning(t, ctx, t1)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t1), completeResult(t, "first reply"), "sess-1", "/tmp/wd", "", false, ""); err != nil {
		t.Fatalf("complete turn 1: %v", err)
	}
	session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected one assistant turn, got %d", len(rows))
	}
	m1 := rows[0].ID

	// Turn 2 is in flight (running, no assistant row yet) so m1 is STILL the
	// latest-persisted turn — the stale check passes — but the session is busy.
	t2 := sendDirectChat(t, ctx, agentID, sessionID, "second")
	markTaskRunning(t, ctx, t2)

	restore := installStubQuickActions()
	defer restore()
	if _, _, err := testHandler.TaskService.RegenerateChatQuickActions(ctx, session, m1); !errors.Is(err, service.ErrChatQuickActionsBusy) {
		t.Fatalf("busy-session regenerate error = %v, want ErrChatQuickActionsBusy", err)
	}
}

// TestRegenerateChatQuickActions_DeferredActiveTurnRejected pins the re-review
// §1 gap: a chat auto-retry armed with a backoff fire_at is inserted 'deferred'
// (CreateRetryTask; provider_network's final attempt waits ~5s that way). During
// that window the failed turn has written no assistant row, so the old turn is
// still latest-persisted and its pills stay clickable — yet the session is about
// to advance when the retry fires. HasActiveChatTaskForSession must count
// 'deferred', else the refresh resumes a session the retry moves past and pins
// the new turn's suggestions onto the old one.
func TestRegenerateChatQuickActions_DeferredActiveTurnRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "regen deferred chat")

	t1 := sendDirectChat(t, ctx, agentID, sessionID, "first")
	markTaskRunning(t, ctx, t1)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(t1), completeResult(t, "first reply"), "sess-1", "/tmp/wd", "", false, ""); err != nil {
		t.Fatalf("complete turn 1: %v", err)
	}
	session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	rows := assistantRows(t, ctx, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected one assistant turn, got %d", len(rows))
	}
	m1 := rows[0].ID

	// Turn 2 failed and is waiting out its retry backoff: a deferred task with a
	// future fire_at and no assistant row yet, so m1 is still latest-persisted.
	t2 := sendDirectChat(t, ctx, agentID, sessionID, "second")
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status='deferred', fire_at=now() + interval '5 seconds' WHERE id=$1`,
		t2); err != nil {
		t.Fatalf("defer turn 2: %v", err)
	}

	restore := installStubQuickActions()
	defer restore()
	if _, _, err := testHandler.TaskService.RegenerateChatQuickActions(ctx, session, m1); !errors.Is(err, service.ErrChatQuickActionsBusy) {
		t.Fatalf("deferred-active regenerate error = %v, want ErrChatQuickActionsBusy", err)
	}
}
