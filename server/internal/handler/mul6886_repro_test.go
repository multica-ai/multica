package handler

import (
	"context"
	"net/url"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Baseline: upstream/main 2e297451001e65f78721efc24e36e7939e0f0ed6 (2026-09-01) + fork HEAD 15d904a99
// Repro command: go test ./internal/handler -run TestMUL6886 -count=1 -v
// Scope: strictly "claimed but non-terminal direct-chat follow-up" (queued,
// dispatched, running, waiting_local_directory). Terminal successors
// (completed/failed/cancelled) are intentionally out of scope for this change
// because correcting them would require rewriting already-finalized turn
// history — the residual ordering (user A -> user B -> assistant B ->
// Stopped.(A)) is a known follow-up candidate, not intended behavior
// (see TestMUL6886_CompletedTerminalNegative_GREEN). Deferred and channel
// batches are likewise never moved.
// Predicate: IN ('queued','dispatched','running','waiting_local_directory') —
// all proven by TestMUL6886_ActiveStates_Table. Retry children
// (chat_input_task_id != id) remain excluded (TestMUL6886_RetryExclusion_GREEN).
// Cursor: only STABLE FINAL SNAPSHOT is contract (fresh cursor after settle).

// TestMUL6886_DeferredCancelClaimedFollowUp_Repro is the deterministic
// regression required by the decision-gate. It reproduces the exact race
// left explicit in #7818.
//
// user A task A starts, A is cancelled (deferred), user B task B is queued
// and then CLAIMED, A's deferred assistant outcome settles late. Current
// main's ReanchorNextQueuedDirectChatInput only moves B while B is queued,
// so the persisted transcript becomes user A -> user B -> assistant A
// instead of user A -> assistant A -> user B.
// The test must FAIL on current main (showing the bug) and PASS after the
// minimal reanchor/settlement fix.
func TestMUL6886_DeferredCancelClaimedFollowUp_Repro(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "MUL-6886 repro")

	tA := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	markTaskRunning(t, ctx, tA)

	if _, err := testHandler.TaskService.CancelTaskWithResult(ctx, parseUUID(tA), service.CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel task A: %v", err)
	}
	var deferredAt *string
	if err := testPool.QueryRow(ctx, `SELECT chat_finalize_deferred_at::text FROM agent_task_queue WHERE id = $1`, tA).Scan(&deferredAt); err != nil {
		t.Fatalf("read deferred marker: %v", err)
	}
	if deferredAt == nil {
		t.Fatalf("chat_finalize_deferred_at should be set after cancelling a started empty task")
	}

	tB := sendDirectChat(t, ctx, agentID, sessionID, "user B")
	var owner string
	if err := testPool.QueryRow(ctx, `SELECT chat_input_task_id::text FROM agent_task_queue WHERE id = $1`, tB).Scan(&owner); err != nil {
		t.Fatalf("read B owner: %v", err)
	}
	if owner != tB {
		t.Fatalf("B input owner = %s, want %s", owner, tB)
	}
	if tB == "" {
		t.Fatalf("tB empty")
	}

	claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	{
		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, tB).Scan(&status); err != nil {
			t.Fatalf("read B status after guard claim: %v", err)
		}
		if status == "queued" {
			t.Logf("B still queued after guard claim (%q), trying direct TaskService.ClaimTask; claimed=%+v", status, claimed)
			direct, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID))
			if err != nil {
				t.Fatalf("direct claim B: %v", err)
			}
			if direct == nil {
				t.Fatalf("claim B returned nil: B not claimable (status %q)", status)
			}
			if uuidToString(direct.ID) != tB {
				t.Logf("direct claim returned task %s, want B %s (checking B status)", uuidToString(direct.ID), tB)
			}
			if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, tB).Scan(&status); err != nil {
				t.Fatalf("read B status after direct claim: %v", err)
			}
			if status == "queued" {
				t.Fatalf("B still queued after both claim paths, status=%q", status)
			}
			t.Logf("B claimed via direct service, status=%q", status)
		} else {
			t.Logf("B claimed via runtime guard, status=%q chat_message=%q", status, claimed.ChatMessage)
			if claimed.ChatMessage != "user B" {
				t.Fatalf("claimed chat_message = %q, want user B (got B via dispatch)", claimed.ChatMessage)
			}
		}
	}

	var bStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, tB).Scan(&bStatus); err != nil {
		t.Fatalf("read B status: %v", err)
	}
	if bStatus == "queued" {
		t.Fatalf("B must be dispatched/running after claim, got queued")
	}
	if bStatus == "completed" || bStatus == "failed" || bStatus == "cancelled" {
		t.Fatalf("B must not be terminal before A settles, got %q", bStatus)
	}
	if bStatus == "" {
		t.Fatalf("bStatus empty")
	}
	t.Logf("B status before A settle: %q", bStatus)

	var bCreatedBefore string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id = $1 AND role='user'`, tB).Scan(&bCreatedBefore); err != nil {
		t.Fatalf("read B input created_at before settle: %v", err)
	}
	if bCreatedBefore == "" {
		t.Fatalf("bCreatedBefore empty")
	}

	insertTaskTranscriptRow(t, ctx, tA)

	changed := testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(tA))
	if !changed {
		t.Fatalf("FinalizeDeferredCancelledChat should have claimed deferred marker")
	}

	transcript, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	t.Logf("TRANSCRIPT actual: %v", msgContents(transcript))
	for i, m := range transcript {
		t.Logf("  [%d] role=%s content=%q created_at=%v id=%s task=%s", i, m.Role, m.Content, m.CreatedAt.Time, uuidToString(m.ID), uuidToString(m.TaskID))
		if uuidToString(m.ID) == "" {
			t.Fatalf("message id empty at %d", i)
		}
		if !m.CreatedAt.Valid || m.CreatedAt.Time.IsZero() {
			t.Fatalf("message created_at invalid at %d", i)
		}
	}
	var aAssistantCreated string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='assistant'`, tA).Scan(&aAssistantCreated); err != nil {
		t.Fatalf("read assistant A created_at: %v", err)
	}
	if aAssistantCreated == "" {
		t.Fatalf("aAssistantCreated empty")
	}
	var bUserCreatedAfter string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='user'`, tB).Scan(&bUserCreatedAfter); err != nil {
		t.Fatalf("read B user created_at after settle: %v", err)
	}
	if bUserCreatedAfter == "" {
		t.Fatalf("bUserCreatedAfter empty")
	}
	t.Logf("B input before settle: %s", bCreatedBefore)
	t.Logf("assistant A after settle: %s", aAssistantCreated)
	t.Logf("B input after settle: %s", bUserCreatedAfter)

	if len(transcript) != 3 {
		t.Fatalf("transcript len = %d, want 3 (user A, assistant A, user B)", len(transcript))
	}
	if transcript[0].Content != "user A" {
		t.Fatalf("transcript[0] = %q, want user A", transcript[0].Content)
	}
	if transcript[1].Content != "Stopped." && transcript[1].Content != "assistant A" {
		t.Fatalf("transcript[1] = %q, want assistant A / Stopped. (user A -> assistant A -> user B)", transcript[1].Content)
	}
	if transcript[2].Content != "user B" {
		t.Fatalf("transcript[2] = %q, want user B; actual order is %q (bug: user B before assistant A)", transcript[2].Content, msgContents(transcript))
	}
	if transcript[1].Content == "user B" {
		t.Fatalf("BUG REPRODUCED: transcript = user A -> user B -> assistant A (user B at %s before assistant A at %s)", bUserCreatedAfter, aAssistantCreated)
	}

	var want = []string{"user A", "Stopped.", "user B"}
	var paged []string
	seen := map[string]bool{}
	var before *ChatMessagesCursorResponse
	for {
		params := url.Values{"limit": {"1"}}
		if before != nil {
			params.Set("before_created_at", before.CreatedAt)
			params.Set("before_id", before.ID)
		}
		page := fetchChatMessagesPageForTest(t, sessionID, params)
		for _, m := range page.Messages {
			if seen[m.ID] {
				t.Fatalf("cursor pagination returned duplicate %s", m.ID)
			}
			if m.ID == "" {
				t.Fatalf("paged message id empty")
			}
			seen[m.ID] = true
			paged = append([]string{m.Content}, paged...)
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == nil {
			t.Fatal("page has_more without next_cursor")
		}
		if page.NextCursor.CreatedAt == "" || page.NextCursor.ID == "" {
			t.Fatalf("next cursor empty: %+v", page.NextCursor)
		}
		before = page.NextCursor
	}
	t.Logf("PAGINATED reconstructed (limit=1): %v", paged)
	if len(paged) != len(want) {
		t.Fatalf("paged len = %d, want %d", len(paged), len(want))
	}
	for i := range want {
		if paged[i] != want[i] {
			t.Fatalf("paged[%d]=%q want %q (full=%v)", i, paged[i], want[i], msgContents(transcript))
		}
	}
}

// TestMUL6886_ActiveStates_Table proves the widened predicate covers all
// reachable non-terminal active states: dispatched, waiting_local_directory,
// running. It reuses the core fixture and updates B in-place to each state
// before settlement, asserting the same reanchor: user A -> Stopped. -> user B.
func TestMUL6886_ActiveStates_Table(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name    string
		status  string
		prepare string
	}{
		{"dispatched", "dispatched", "dispatched_at = now()"},
		{"waiting_local_directory", "waiting_local_directory", "dispatched_at = now()"},
		{"running", "running", "dispatched_at = now(), started_at = now()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "MUL-6886 active "+tc.name)
			tA := sendDirectChat(t, ctx, agentID, sessionID, "user A")
			markTaskRunning(t, ctx, tA)
			if _, err := testHandler.TaskService.CancelTaskWithResult(ctx, parseUUID(tA), service.CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
				t.Fatalf("cancel A: %v", err)
			}
			tB := sendDirectChat(t, ctx, agentID, sessionID, "user B")
			claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
			var bStatus string
			if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, tB).Scan(&bStatus); err != nil {
				t.Fatalf("read B status after guard claim: %v", err)
			}
			if bStatus == "queued" {
				if _, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID)); err != nil {
					t.Fatalf("direct claim B: %v", err)
				}
			}
			if tc.status != "dispatched" {
				q := `UPDATE agent_task_queue SET status=$1, ` + tc.prepare + ` WHERE id=$2`
				if _, err := testPool.Exec(ctx, q, tc.status, tB); err != nil {
					t.Fatalf("set B to %s: %v", tc.status, err)
				}
			}
			var gotStatus string
			if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, tB).Scan(&gotStatus); err != nil {
				t.Fatalf("read B status for table: %v", err)
			}
			if gotStatus != tc.status {
				t.Fatalf("B status=%q want %q", gotStatus, tc.status)
			}
			if gotStatus == "" {
				t.Fatalf("gotStatus empty")
			}
			t.Logf("B status for %s case: %q claimed=%q", tc.name, gotStatus, claimed.ChatMessage)

			var bBefore string
			if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='user'`, tB).Scan(&bBefore); err != nil {
				t.Fatalf("read B before: %v", err)
			}
			if bBefore == "" {
				t.Fatalf("bBefore empty")
			}
			insertTaskTranscriptRow(t, ctx, tA)
			changed := testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(tA))
			if !changed {
				t.Fatalf("FinalizeDeferredCancelledChat should have claimed deferred marker for %s", tc.name)
			}
			var bAfter, aAfter string
			if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='user'`, tB).Scan(&bAfter); err != nil {
				t.Fatalf("read B after: %v", err)
			}
			if bAfter == "" {
				t.Fatalf("bAfter empty for %s", tc.name)
			}
			if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='assistant'`, tA).Scan(&aAfter); err != nil {
				t.Fatalf("read A assistant after: %v", err)
			}
			if aAfter == "" {
				t.Fatalf("aAfter empty for %s", tc.name)
			}
			t.Logf("[%s] B before=%s B after=%s A assistant=%s", tc.name, bBefore, bAfter, aAfter)
			if bAfter != aAfter && bBefore == bAfter {
				t.Fatalf("[%s] B was not reanchored: before %s after %s A %s", tc.name, bBefore, bAfter, aAfter)
			}
			transcript, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
			if err != nil {
				t.Fatalf("list messages for %s: %v", tc.name, err)
			}
			t.Logf("[%s] transcript: %v", tc.name, msgContents(transcript))
			assertChatTranscriptContents(t, transcript, []string{"user A", "Stopped.", "user B"})
			batch, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(tB))
			if err != nil {
				t.Fatalf("ListChatInputMessages for %s: %v", tc.name, err)
			}
			if len(batch) != 1 || batch[0].Content != "user B" {
				t.Fatalf("[%s] ListChatInputMessages: %+v", tc.name, batch)
			}
			if uuidToString(batch[0].ID) == "" || !batch[0].CreatedAt.Valid || batch[0].CreatedAt.Time.IsZero() {
				t.Fatalf("[%s] batch message id/timestamp empty", tc.name)
			}
		})
	}
}

// TestMUL6886_RetryExclusion_GREEN pins that retry-owned input is intentionally
// NOT reanchored. When B has a running retry B2 (chat_input_task_id = B), the
// next head is B2 (dispatched) but the physical user row is still task_id=B
// with chat_input_task_id != id for the head. The current ReanchorNextQueued
// filter chat_input_task_id=id excludes it, and the minimal fix will keep that
// exclusion to avoid moving terminal-history rows via the retry child.
func TestMUL6886_RetryExclusion_GREEN(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "MUL-6886 retry exclusion")

	tA := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	markTaskRunning(t, ctx, tA)
	if _, err := testHandler.TaskService.CancelTaskWithResult(ctx, parseUUID(tA), service.CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel A: %v", err)
	}
	tB := sendDirectChat(t, ctx, agentID, sessionID, "user B")

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status='failed', failure_reason='agent_error', completed_at=now() WHERE id=$1`, tB); err != nil {
		t.Fatalf("fail B: %v", err)
	}
	retry, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parseUUID(tB)})
	if err != nil {
		t.Fatalf("create retry B2: %v", err)
	}
	if !retry.ChatInputTaskID.Valid || uuidToString(retry.ChatInputTaskID) != tB {
		t.Fatalf("retry ChatInputTaskID=%s want %s", uuidToString(retry.ChatInputTaskID), tB)
	}
	if uuidToString(retry.ID) == "" {
		t.Fatalf("retry ID empty")
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched', dispatched_at=now() WHERE id=$1`, uuidToString(retry.ID)); err != nil {
		t.Fatalf("dispatch B2: %v", err)
	}
	var headID, headStatus, headOwner string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text, status, chat_input_task_id::text FROM agent_task_queue WHERE chat_session_id=$1 AND status IN ('queued','dispatched','running','waiting_local_directory','deferred') AND regenerate_quick_actions_for IS NULL ORDER BY CASE WHEN status IN ('dispatched','running','waiting_local_directory') THEN 0 WHEN status='deferred' THEN 1 ELSE 2 END, priority DESC, created_at ASC, id ASC LIMIT 1`,
		sessionID).Scan(&headID, &headStatus, &headOwner); err != nil {
		t.Fatalf("read head: %v", err)
	}
	if headID == "" || headStatus == "" || headOwner == "" {
		t.Fatalf("head fields empty: %s %s %s", headID, headStatus, headOwner)
	}
	t.Logf("head id=%s status=%s owner=%s want B2 %s", headID, headStatus, headOwner, uuidToString(retry.ID))
	if headID != uuidToString(retry.ID) {
		t.Fatalf("head should be retry B2 %s, got %s", uuidToString(retry.ID), headID)
	}

	insertTaskTranscriptRow(t, ctx, tA)
	var beforeCreated string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='user'`, tB).Scan(&beforeCreated); err != nil {
		t.Fatalf("read before: %v", err)
	}
	if beforeCreated == "" {
		t.Fatalf("beforeCreated empty")
	}
	if changed := testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(tA)); !changed {
		t.Fatalf("finalize should claim marker")
	}
	var afterCreated string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='user'`, tB).Scan(&afterCreated); err != nil {
		t.Fatalf("read after: %v", err)
	}
	if afterCreated == "" {
		t.Fatalf("afterCreated empty")
	}
	t.Logf("retry case: user B before=%s after=%s", beforeCreated, afterCreated)
	if beforeCreated != afterCreated {
		t.Fatalf("retry exclusion: user B created_at moved from %s to %s — would imply retry was reanchored via owner, but contract is to exclude retries", beforeCreated, afterCreated)
	}
	batch, err := testHandler.Queries.ListChatInputMessages(ctx, parseUUID(tB))
	if err != nil || len(batch) != 1 || batch[0].Content != "user B" {
		t.Fatalf("ListChatInputMessages via owner B: %+v err %v", batch, err)
	}
	if uuidToString(batch[0].ID) == "" || !batch[0].CreatedAt.Valid {
		t.Fatalf("batch id/timestamp empty")
	}
	batch2, err := testHandler.Queries.ListChatInputMessages(ctx, retry.ChatInputTaskID)
	if err != nil || len(batch2) != 1 || batch2[0].Content != "user B" {
		t.Fatalf("ListChatInputMessages via retry owner: %+v err %v", batch2, err)
	}
	if uuidToString(batch2[0].ID) == "" || !batch2[0].CreatedAt.Valid {
		t.Fatalf("batch2 id/timestamp empty")
	}
}

// TestMUL6886_CompletedTerminalNegative_GREEN pins that a completed follow-up
// is NOT reanchored. If B already has assistant B before A settles, A's late
// settlement must not rewrite B's input position. This is the intentional
// boundary for claimed but non-terminal scope: transcript is left as
// user A -> user B -> assistant B -> Stopped.(A).
func TestMUL6886_CompletedTerminalNegative_GREEN(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "MUL-6886 completed negative")

	tA := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	markTaskRunning(t, ctx, tA)
	if _, err := testHandler.TaskService.CancelTaskWithResult(ctx, parseUUID(tA), service.CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel A: %v", err)
	}
	tB := sendDirectChat(t, ctx, agentID, sessionID, "user B")
	claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	var bStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, tB).Scan(&bStatus); err != nil {
		t.Fatalf("read bStatus: %v", err)
	}
	if bStatus == "queued" {
		if _, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID)); err != nil {
			t.Fatalf("claim B fallback: %v", err)
		}
		if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, tB).Scan(&bStatus); err != nil {
			t.Fatalf("read bStatus after fallback: %v", err)
		}
	}
	if bStatus == "" {
		t.Fatalf("bStatus empty")
	}
	t.Logf("B claimed status before complete: %s chat=%q", bStatus, claimed.ChatMessage)
	if claimed.ChatMessage == "" {
		t.Fatalf("claimed chat empty")
	}
	markTaskRunning(t, ctx, tB)
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(tB), completeResult(t, "assistant B"), "", "", "", false, "", ""); err != nil {
		t.Fatalf("complete B: %v", err)
	}
	fullBefore, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	t.Logf("before A settle (B completed): %v", msgContents(fullBefore))
	var bCreatedBefore string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='user'`, tB).Scan(&bCreatedBefore); err != nil {
		t.Fatalf("read bCreatedBefore: %v", err)
	}
	if bCreatedBefore == "" {
		t.Fatalf("bCreatedBefore empty")
	}

	insertTaskTranscriptRow(t, ctx, tA)
	if changed := testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(tA)); !changed {
		t.Fatalf("finalize should claim marker")
	}

	var bCreatedAfter string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE task_id=$1 AND role='user'`, tB).Scan(&bCreatedAfter); err != nil {
		t.Fatalf("read bCreatedAfter: %v", err)
	}
	if bCreatedAfter == "" {
		t.Fatalf("bCreatedAfter empty")
	}
	t.Logf("B created before=%s after=%s", bCreatedBefore, bCreatedAfter)
	if bCreatedBefore != bCreatedAfter {
		t.Fatalf("completed B was reanchored: %s -> %s", bCreatedBefore, bCreatedAfter)
	}
	fullAfter, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	t.Logf("after A settle: %v", msgContents(fullAfter))
	for _, m := range fullAfter {
		if uuidToString(m.ID) == "" || !m.CreatedAt.Valid {
			t.Fatalf("message id/timestamp empty in fullAfter: %+v", m)
		}
	}
	assertChatTranscriptContents(t, fullAfter, []string{"user A", "user B", "assistant B", "Stopped."})
}

// TestMUL6886_ChannelIsolation_GREEN verifies channel-ingested batches are NOT moved.
func TestMUL6886_ChannelIsolation_GREEN(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, _, _ := setupDirectChatSession(t, ctx, "MUL-6886 channel isolation")

	tA := sendDirectChat(t, ctx, agentID, sessionID, "user A")
	markTaskRunning(t, ctx, tA)
	if _, err := testHandler.TaskService.CancelTaskWithResult(ctx, parseUUID(tA), service.CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel A: %v", err)
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id=$1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read runtimeID: %v", err)
	}
	if runtimeID == "" {
		t.Fatalf("runtimeID empty")
	}
	tB := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":      runtimeID,
		"chat_session_id": sessionID,
		"status":          "queued",
		"priority":        2,
	})
	if tB == "" {
		t.Fatalf("tB empty")
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, tB)
	msgID := dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id":  sessionID,
		"role":             "user",
		"content":          "user channel B",
		"task_id":          tB,
		"channel_ingested": true,
	})
	if msgID == "" {
		t.Fatalf("msgID empty")
	}
	var before string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE id=$1`, msgID).Scan(&before); err != nil {
		t.Fatalf("read before: %v", err)
	}
	if before == "" {
		t.Fatalf("before empty")
	}

	insertTaskTranscriptRow(t, ctx, tA)
	if changed := testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(tA)); !changed {
		t.Fatalf("finalize should claim marker")
	}

	var after string
	if err := testPool.QueryRow(ctx, `SELECT created_at::text FROM chat_message WHERE id=$1`, msgID).Scan(&after); err != nil {
		t.Fatalf("read after: %v", err)
	}
	if after == "" {
		t.Fatalf("after empty")
	}
	t.Logf("channel B before=%s after=%s", before, after)
	if before != after {
		t.Fatalf("channel batch was reanchored: %s -> %s", before, after)
	}
	transcript, err := testHandler.Queries.ListChatMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	t.Logf("channel transcript: %v", msgContents(transcript))
	for _, m := range transcript {
		if uuidToString(m.ID) == "" || !m.CreatedAt.Valid {
			t.Fatalf("message id/timestamp empty: %+v", m)
		}
	}
}
