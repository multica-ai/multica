package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type supplementFixture struct {
	runtimeID string
	agentID   string
	issueID   string
	taskID    string
	triggerID string
}

func TestStableTaskSupplementFailureReason(t *testing.T) {
	for _, reason := range []string{
		protocol.TaskSupplementFailureTurnNotStarted,
		protocol.TaskSupplementFailureProviderRejected,
		protocol.TaskSupplementFailureTimeout,
		protocol.TaskSupplementFailureTurnEnded,
	} {
		if got := stableTaskSupplementFailureReason(reason); got != reason {
			t.Fatalf("stableTaskSupplementFailureReason(%q) = %q", reason, got)
		}
	}
	for _, raw := range []string{"", "供应商错误：无法发送", strings.Repeat("界", 600)} {
		if got := stableTaskSupplementFailureReason(raw); got != protocol.TaskSupplementFailureProviderRejected {
			t.Fatalf("raw reason %q escaped as %q", raw, got)
		}
	}
}

func TestTaskSupplementSettlementHasNoDatabaseTrigger(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	var exists bool
	if err := testPool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1
			FROM pg_trigger
			WHERE tgname = 'trg_settle_terminal_task_supplements'
			  AND NOT tgisinternal
		)
	`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("task supplement settlement still depends on a database trigger")
	}
}

func TestTaskSupplementClaimStartReplay(t *testing.T) {
	f := newSupplementFixture(t, "codex", "dispatched", false)
	dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id=$1`, f.taskID)
	generation := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at=$2 WHERE id=$1`, f.taskID, generation)
	for range 2 {
		req := withURLParam(newDaemonTokenRequest(http.MethodPost, "/start", map[string]any{
			"runtime_id": f.runtimeID, "dispatched_at": generation.Format(time.RFC3339Nano),
			"capabilities": []string{protocol.DaemonCapabilityTaskSupplementV1},
		}, testWorkspaceID, "start-claim-test"), "taskId", f.taskID)
		var started AgentTaskResponse
		testutil.Call(t, testHandler.StartTask, req).Want(http.StatusOK).JSON(&started)
		if started.SupplementCapability != protocol.DaemonCapabilityTaskSupplementV1 {
			t.Fatalf("lost negotiated capability on start replay: %+v", started)
		}
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id=$1`, f.taskID); n != 1 {
		t.Fatalf("start replay wrote %d capabilities", n)
	}
}

func TestTaskSupplementLateDeliveryAndDeletion(t *testing.T) {
	f := newSupplementFixture(t, "claude", "running", true)
	var sent, pending CommentResponse
	supplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "already injected").Want(http.StatusCreated).JSON(&sent)
	if _, err := testHandler.Queries.ClaimNextTaskSupplement(t.Context(), parseUUID(f.taskID)); err != nil {
		t.Fatal(err)
	}
	supplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-222222222222", "never injected").Want(http.StatusCreated).JSON(&pending)
	deleteRequest := func() *http.Request {
		return withURLParam(newRequest(http.MethodDelete, "/comments/"+sent.ID, nil), "commentId", sent.ID)
	}
	testutil.Call(t, testHandler.DeleteComment, deleteRequest()).Want(http.StatusConflict)
	if w := completeTaskViaHandler(t, f.taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("complete task: status %d: %s", w.Code, w.Body.String())
	}
	for _, c := range []CommentResponse{sent, pending} {
		req := withURLParams(newDaemonTokenRequest(http.MethodPost, "/ack", map[string]any{"delivered": true}, testWorkspaceID, "legit-daemon"), "taskId", f.taskID, "commentId", c.ID)
		want := http.StatusOK
		if c.ID == pending.ID {
			want = http.StatusConflict
		}
		testutil.Call(t, testHandler.AckTaskSupplement, req).Want(want)
	}
	testutil.Call(t, testHandler.DeleteComment, deleteRequest()).Want(http.StatusNoContent)
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE comment_id=$1`, sent.ID); n != 0 {
		t.Fatal("deleted comment retained its receipt")
	}
}

func TestTaskSupplementSubmissionBoundsAndConflicts(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	const requestID = "0199a4e8-22ce-7b01-bba5-111111111111"
	supplementRequest(t, f, requestID, strings.Repeat("x", maxCommentContentBytes+1)).Want(http.StatusBadRequest)
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id=$1`, f.taskID); n != 0 {
		t.Fatal("oversized input was persisted")
	}
	supplementRequest(t, f, requestID, "original").Want(http.StatusCreated)
	// Another author's request key is unique within the task but invisible to
	// the current author's idempotency lookup. This is a conflict, not a 500.
	dbfx.Exec(t, `UPDATE task_supplement SET author_id=$2 WHERE task_id=$1`, f.taskID, "0199a4e8-22ce-7b01-bba5-333333333333")
	supplementRequest(t, f, requestID, "collision").Want(http.StatusConflict)
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND content='collision'`, f.issueID); n != 0 {
		t.Fatal("conflict left an orphan comment")
	}
}

func newSupplementFixture(t *testing.T, provider, status string, negotiated bool) supplementFixture {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "supplement-"+provider, testutil.Cols{"provider": provider})
	agentID := dbfx.Agent(t, "Supplement "+provider, runtimeID)
	issueID := dbfx.Issue(t, "supplement "+provider)
	triggerID := dbfx.Comment(t, issueID, "original objective")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":           issueID,
		"runtime_id":         runtimeID,
		"trigger_comment_id": triggerID,
		"status":             status,
		"started_at":         testutil.Raw("CASE WHEN '" + status + "' = 'running' THEN now() ELSE NULL END"),
	})
	if negotiated {
		dbfx.Exec(t, `
			INSERT INTO task_supplement_capability (task_id, workspace_id, issue_id, capability)
			VALUES ($1, $2, $3, $4)
		`, taskID, testWorkspaceID, issueID, protocol.DaemonCapabilityTaskSupplementV1)
		dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id = $1`, taskID)
	}
	dbfx.Cleanup(t, `DELETE FROM task_supplement WHERE task_id = $1`, taskID)
	return supplementFixture{runtimeID: runtimeID, agentID: agentID, issueID: issueID, taskID: taskID, triggerID: triggerID}
}

func supplementRequest(t *testing.T, fixture supplementFixture, requestID, content string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+fixture.issueID+"/tasks/"+fixture.taskID+"/supplements", map[string]any{
		"client_request_id": requestID,
		"content":           content,
	})
	return testutil.Call(t, testHandler.CreateTaskSupplement,
		withURLParams(req, "id", fixture.issueID, "taskId", fixture.taskID),
	)
}

func TestTaskSupplementNegotiationFailsClosed(t *testing.T) {

	for _, tc := range []struct {
		name              string
		provider          string
		daemonAdvertises  bool
		wantCapabilityRow bool
	}{
		{name: "codex negotiated", provider: "codex", daemonAdvertises: true, wantCapabilityRow: true},
		{name: "claude negotiated", provider: "claude", daemonAdvertises: true, wantCapabilityRow: true},
		{name: "grok negotiated", provider: "grok", daemonAdvertises: true, wantCapabilityRow: true},
		{name: "claude old daemon", provider: "claude", daemonAdvertises: false, wantCapabilityRow: false},
		{name: "old daemon", provider: "codex", daemonAdvertises: false, wantCapabilityRow: false},
		{name: "unsupported runtime", provider: "kimi", daemonAdvertises: true, wantCapabilityRow: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newSupplementFixture(t, tc.provider, "dispatched", false)
			var capabilities []string
			if tc.daemonAdvertises {
				capabilities = []string{protocol.DaemonCapabilityTaskSupplementV1}
			}
			req := withURLParam(newRequest(http.MethodPost, "/start", map[string]any{"capabilities": capabilities}), "taskId", fixture.taskID)
			var started AgentTaskResponse
			testutil.Call(t, testHandler.StartTask, req).Want(http.StatusOK).JSON(&started)
			if started.Status != "running" || started.StartedAt == nil {
				t.Fatalf("StartTask returned stale row: %#v", started)
			}
			if got := started.SupplementCapability == protocol.DaemonCapabilityTaskSupplementV1; got != tc.wantCapabilityRow {
				t.Fatalf("negotiated capability = %v, want %v", got, tc.wantCapabilityRow)
			}
			if !tc.wantCapabilityRow {
				supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-111111111111", "extra").Want(http.StatusPreconditionFailed)
			} else {
				supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-111111111111", "extra").Want(http.StatusCreated)
			}
		})
	}
}

func TestTaskSupplementCapabilityDoesNotBreakNonIssueStarts(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, provider := range []string{"codex", "claude", "grok"} {
		t.Run(provider, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "supplement-no-issue", testutil.Cols{"provider": provider})
			agentID := dbfx.Agent(t, "Supplement no issue", runtimeID)
			taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": nil, "status": "dispatched"})
			started, err := testHandler.TaskService.StartTask(t.Context(), parseUUID(taskID), true)
			if err != nil || started.Status != "running" {
				t.Fatalf("non-issue start = %#v: %v", started, err)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id = $1`, taskID); n != 0 {
				t.Fatalf("capability rows = %d, want 0", n)
			}
		})
	}
}

func TestTaskSupplementCompletionDoesNotReplayBoundComments(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "grok"} {
		for _, status := range []string{"pending", "delivering", "delivered", "failed"} {
			for _, ordinary := range []string{"none", "unhandled", "queued"} {
				t.Run(provider+"/"+status+"/"+ordinary, func(t *testing.T) {
					fixture := newSupplementFixture(t, provider, "running", true)
					// A plain reply routes back to the agent owning the original thread.
					dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, fixture.triggerID,
						fmt.Sprintf("[@Supplement](mention://agent/%s) original objective", fixture.agentID))
					dbfx.Exec(t, `UPDATE agent_task_queue SET delivered_comment_ids = ARRAY[trigger_comment_id] WHERE id = $1`, fixture.taskID)
					dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fixture.issueID)

					var ordinaryID, queuedID string
					if ordinary != "none" {
						ordinaryID = dbfx.Comment(t, fixture.issueID, "Handle this ordinary reply next.", testutil.Cols{
							"parent_id": fixture.triggerID,
						})
					}
					if ordinary == "queued" {
						queuedID = dbfx.Task(t, fixture.agentID, testutil.Cols{
							"issue_id": fixture.issueID, "runtime_id": fixture.runtimeID,
							"trigger_comment_id": ordinaryID,
						})
					}

					var supplement CommentResponse
					supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-999999999999", "Add this only to the current run.").
						Want(http.StatusCreated).JSON(&supplement)
					// Receipt transitions are covered by the retry test; vary only the
					// persisted state consumed by completion reconciliation here.
					dbfx.Exec(t, `UPDATE task_supplement SET status = $2 WHERE comment_id = $1`, supplement.ID, status)

					// Drive the real completion handler: a direct SQL status update skips
					// the reconciliation that previously replayed the supplement.
					req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+fixture.taskID+"/complete",
						map[string]any{"output": "done"}, testWorkspaceID, "legit-daemon")
					testutil.Call(t, testHandler.CompleteTask, withURLParam(req, "taskId", fixture.taskID)).Want(http.StatusOK)
					if status == "pending" || status == "delivering" {
						receipt, err := testHandler.Queries.GetTaskSupplementForRun(t.Context(), db.GetTaskSupplementForRunParams{
							CommentID: parseUUID(supplement.ID), TaskID: parseUUID(fixture.taskID), WorkspaceID: parseUUID(testWorkspaceID),
						})
						if err != nil || receipt.Status != "failed" || receipt.FailureReason.String != protocol.TaskSupplementFailureTurnEnded {
							t.Fatalf("completion did not settle pending delivery: %#v: %v", receipt, err)
						}
					}

					wantTasks := 1
					if ordinary != "none" {
						wantTasks++
					}
					if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, fixture.issueID); n != wantTasks {
						t.Fatalf("completion left %d runs, want %d; supplement must never create a follow-up", n, wantTasks)
					}
					if ordinary != "none" {
						var followupID, triggerID string
						var coalesced []string
						dbfx.QueryRow(t, `SELECT id, trigger_comment_id, coalesced_comment_ids::text[]
						FROM agent_task_queue WHERE issue_id = $1 AND id <> $2 AND status = 'queued'`,
							fixture.issueID, fixture.taskID).Scan(&followupID, &triggerID, &coalesced)
						if queuedID != "" && followupID != queuedID {
							t.Fatalf("replaced existing queued run %s with %s", queuedID, followupID)
						}
						if triggerID != ordinaryID || slices.Contains(coalesced, supplement.ID) {
							t.Fatalf("follow-up input = trigger %s, coalesced %v; want ordinary comment %s without supplement %s",
								triggerID, coalesced, ordinaryID, supplement.ID)
						}
					}
				})
			}
		}
	}
}

func TestTaskSupplementConcurrentClaimsKeepUniqueReceipts(t *testing.T) {
	fixture := newSupplementFixture(t, "codex", "running", true)

	const additions = 20
	for i := 0; i < additions; i++ {
		supplementRequest(t, fixture, fmt.Sprintf("0199a4e8-22ce-7b01-bba5-%012x", i+1),
			fmt.Sprintf("addition %02d", i)).Want(http.StatusCreated)
	}
	var wg sync.WaitGroup

	type claimResult struct {
		row db.ClaimNextTaskSupplementRow
		err error
	}
	claims := make(chan claimResult, additions)
	for i := 0; i < additions; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := testHandler.Queries.ClaimNextTaskSupplement(context.Background(), parseUUID(fixture.taskID))
			claims <- claimResult{row, err}
		}()
	}
	wg.Wait()
	close(claims)
	seen := make(map[string]bool, additions)
	for result := range claims {
		claimed := result.row
		id := uuidToString(claimed.CommentID)
		if result.err != nil || seen[id] || claimed.AttemptCount != 1 {
			t.Fatalf("claim %q duplicated=%v attempts=%d: %v", id, seen[id], claimed.AttemptCount, result.err)
		}
		seen[id] = true
		if _, err := testHandler.Queries.AckTaskSupplementDelivered(context.Background(), db.AckTaskSupplementDeliveredParams{
			TaskID: parseUUID(fixture.taskID), CommentID: claimed.CommentID,
		}); err != nil {
			t.Fatalf("ack %s: %v", id, err)
		}
	}
	if len(seen) != additions {
		t.Fatalf("claimed %d unique comments, want %d", len(seen), additions)
	}
	if _, err := testHandler.Queries.ClaimNextTaskSupplement(context.Background(), parseUUID(fixture.taskID)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("extra claim = %v, want no rows", err)
	}
	if delivered := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1 AND status = 'delivered'`, fixture.taskID); delivered != additions {
		t.Fatalf("delivered receipts = %d, want %d", delivered, additions)
	}

	var taskCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, fixture.issueID).Scan(&taskCount)
	if taskCount != 1 {
		t.Fatalf("concurrent additions created %d runs, want one", taskCount)
	}
}

func TestTaskSupplementConcurrentDuplicateSubmissionsAndRetries(t *testing.T) {
	fixture := newSupplementFixture(t, "codex", "running", true)
	const attempts = 10
	responses := make(chan *testutil.Response, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-888888888888", "duplicate addition")
		}()
	}
	wg.Wait()
	close(responses)
	var commentID string
	for response := range responses {
		var comment CommentResponse
		response.WantOneOf(http.StatusOK, http.StatusCreated).JSON(&comment)
		if comment.SupplementTaskID != fixture.taskID || comment.SupplementStatus != "pending" {
			t.Fatalf("submission receipt = %#v", comment)
		}
		if commentID != "" && commentID != comment.ID {
			t.Fatalf("duplicate submission created %s, want %s", comment.ID, commentID)
		}
		commentID = comment.ID
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1`, fixture.taskID); n != 1 {
		t.Fatalf("duplicate submissions created %d receipts", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content = 'duplicate addition'`, fixture.issueID); n != 1 {
		t.Fatalf("duplicate submissions left %d comments", n)
	}
	claim, err := testHandler.Queries.ClaimNextTaskSupplement(context.Background(), parseUUID(fixture.taskID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.Queries.AckTaskSupplementFailed(context.Background(), db.AckTaskSupplementFailedParams{
		TaskID: parseUUID(fixture.taskID), CommentID: claim.CommentID,
		FailureReason: pgtype.Text{String: protocol.TaskSupplementFailureProviderRejected, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	responses = make(chan *testutil.Response, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := withURLParams(newRequest(http.MethodPost, "/retry", nil),
				"id", fixture.issueID, "taskId", fixture.taskID, "commentId", commentID)
			responses <- testutil.Call(t, testHandler.RetryTaskSupplement, req)
		}()
	}
	wg.Wait()
	close(responses)
	for response := range responses {
		response.Want(http.StatusOK)
	}
	retried, err := testHandler.Queries.ClaimNextTaskSupplement(context.Background(), parseUUID(fixture.taskID))
	if err != nil || retried.CommentID != claim.CommentID || retried.AttemptCount != 2 {
		t.Fatalf("retry claim = %#v: %v", retried, err)
	}
	if _, err := testHandler.Queries.ClaimNextTaskSupplement(context.Background(), parseUUID(fixture.taskID)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate retry left another claim: %v", err)
	}
	ack := db.AckTaskSupplementDeliveredParams{TaskID: parseUUID(fixture.taskID), CommentID: claim.CommentID}
	firstAck, err := testHandler.Queries.AckTaskSupplementDelivered(context.Background(), ack)
	if err != nil || firstAck.Status != "delivered" || !firstAck.DeliveredAt.Valid {
		t.Fatalf("delivery receipt = %#v: %v", firstAck, err)
	}
	secondAck, err := testHandler.Queries.AckTaskSupplementDelivered(context.Background(), ack)
	if err != nil || !secondAck.DeliveredAt.Valid || secondAck.DeliveredAt != firstAck.DeliveredAt {
		t.Fatalf("duplicate acknowledgement changed receipt: %#v: %v", secondAck, err)
	}
}

func TestTaskSupplementTimestampOrderAndCommentIDTieBreak(t *testing.T) {
	for _, sameTime := range []bool{false, true} {
		t.Run(fmt.Sprintf("same timestamp=%v", sameTime), func(t *testing.T) {
			fixture := newSupplementFixture(t, "codex", "running", true)
			var first, second CommentResponse
			supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-666666666666", "first").Want(http.StatusCreated).JSON(&first)
			supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-777777777777", "second").Want(http.StatusCreated).JSON(&second)
			firstTime := time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC)
			secondTime := firstTime.Add(time.Second)
			want := []string{first.ID, second.ID}
			if sameTime {
				secondTime = firstTime
				slices.Sort(want)
			} else {
				// Deliberately invert creation order: timestamps, not a hidden
				// allocation sequence, must drive both metadata and claims.
				firstTime, secondTime = secondTime, firstTime
				want = []string{second.ID, first.ID}
			}
			dbfx.Exec(t, `UPDATE task_supplement SET created_at = $2 WHERE comment_id = $1`, first.ID, firstTime)
			dbfx.Exec(t, `UPDATE task_supplement SET created_at = $2 WHERE comment_id = $1`, second.ID, secondTime)
			rows, err := testHandler.Queries.ListTaskSupplementMetadata(context.Background(), db.ListTaskSupplementMetadataParams{
				WorkspaceID: parseUUID(testWorkspaceID), TaskIds: []pgtype.UUID{parseUUID(fixture.taskID)},
			})
			if err != nil || len(rows) != 1 {
				t.Fatalf("metadata rows=%v: %v", rows, err)
			}
			if got := uuidsToStrings(rows[0].CommentIds); !slices.Equal(got, want) {
				t.Fatalf("metadata order=%v, want %v", got, want)
			}
			for _, id := range want {
				claim, err := testHandler.Queries.ClaimNextTaskSupplement(context.Background(), parseUUID(fixture.taskID))
				if err != nil || uuidToString(claim.CommentID) != id {
					t.Fatalf("claim=%s, want %s: %v", uuidToString(claim.CommentID), id, err)
				}
			}
		})
	}
}

func TestTaskSupplementTerminalRaceCreatesNothing(t *testing.T) {
	for _, status := range []string{"completed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			assertTaskSupplementTerminalRaceCreatesNothing(t, status)
		})
	}
}

func assertTaskSupplementTerminalRaceCreatesNothing(t *testing.T, terminalStatus string) {
	t.Helper()
	fixture := newSupplementFixture(t, "codex", "running", true)
	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `SELECT id FROM agent_task_queue WHERE id = $1 FOR UPDATE`, fixture.taskID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, createErr := testHandler.Queries.CreateTaskSupplement(context.Background(), db.CreateTaskSupplementParams{
			TaskID: parseUUID(fixture.taskID), IssueID: parseUUID(fixture.issueID), WorkspaceID: parseUUID(testWorkspaceID),
			AuthorID: parseUUID(testUserID), Content: "must not orphan",
			ClientRequestID: parseUUID("0199a4e8-22ce-7b01-bba5-444444444444"),
		})
		result <- createErr
	}()
	if _, err := tx.Exec(context.Background(), `UPDATE agent_task_queue SET status = $2, completed_at = now() WHERE id = $1`, fixture.taskID, terminalStatus); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("race create error = %v, want no rows", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("race create remained blocked")
	}
	var comments, supplements int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content = 'must not orphan'`, fixture.issueID).Scan(&comments)
	dbfx.QueryRow(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1`, fixture.taskID).Scan(&supplements)
	if comments != 0 || supplements != 0 {
		t.Fatalf("terminal race left comments=%d supplements=%d", comments, supplements)
	}
}

func TestTaskSupplementStopRemainsIndependent(t *testing.T) {
	fixture := newSupplementFixture(t, "codex", "running", true)
	var comment CommentResponse
	supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-777777777777", "pending when stopped").
		Want(http.StatusCreated).JSON(&comment)
	cancelReq := withURLParams(newRequest(http.MethodPost, "/cancel", nil),
		"id", fixture.issueID, "taskId", fixture.taskID)
	testutil.Call(t, testHandler.CancelTask, cancelReq).Want(http.StatusOK)
	receipt, err := testHandler.Queries.GetTaskSupplementForRun(context.Background(), db.GetTaskSupplementForRunParams{
		CommentID: parseUUID(comment.ID), TaskID: parseUUID(fixture.taskID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil || receipt.Status != "failed" || receipt.FailureReason.String != "turn_ended" {
		t.Fatalf("stop receipt = %#v, err %v", receipt, err)
	}
	var taskCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, fixture.issueID).Scan(&taskCount)
	if taskCount != 1 {
		t.Fatalf("stop or supplement created %d runs, want one", taskCount)
	}
}

func TestTaskSupplementFailureSettlesPendingReceipt(t *testing.T) {
	fixture := newSupplementFixture(t, "codex", "running", true)
	var comment CommentResponse
	supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-555555555555", "pending when failed").
		Want(http.StatusCreated).JSON(&comment)
	if w := failTaskViaHandler(t, fixture.taskID); w.Code != http.StatusOK {
		t.Fatalf("fail task: status %d: %s", w.Code, w.Body.String())
	}
	receipt, err := testHandler.Queries.GetTaskSupplementForRun(t.Context(), db.GetTaskSupplementForRunParams{
		CommentID: parseUUID(comment.ID), TaskID: parseUUID(fixture.taskID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil || receipt.Status != "failed" || receipt.FailureReason.String != protocol.TaskSupplementFailureTurnEnded {
		t.Fatalf("failed task receipt = %#v, err %v", receipt, err)
	}
}

func TestTaskSupplementTerminalWriteRollsBackWhenSettlementFails(t *testing.T) {
	fixture := newSupplementFixture(t, "codex", "running", true)
	var comment CommentResponse
	supplementRequest(t, fixture, "0199a4e8-22ce-7b01-bba5-aaaaaaaaaaaa", "must settle atomically").
		Want(http.StatusCreated).JSON(&comment)

	dbfx.Exec(t, `
		CREATE OR REPLACE FUNCTION reject_task_supplement_settlement() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'settlement rejected'; END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_task_supplement_settlement
		BEFORE UPDATE OF status ON task_supplement
		FOR EACH ROW EXECUTE FUNCTION reject_task_supplement_settlement()
	`)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_task_supplement_settlement ON task_supplement`)
		_, _ = testPool.Exec(context.Background(), `DROP FUNCTION IF EXISTS reject_task_supplement_settlement()`)
	})

	if w := completeTaskViaHandler(t, fixture.taskID, "done"); w.Code != http.StatusInternalServerError {
		t.Fatalf("complete task with rejected settlement: status %d: %s", w.Code, w.Body.String())
	}
	var taskStatus, receiptStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&taskStatus)
	dbfx.QueryRow(t, `SELECT status FROM task_supplement WHERE comment_id = $1`, comment.ID).Scan(&receiptStatus)
	if taskStatus != "running" || receiptStatus != "pending" {
		t.Fatalf("partial terminal commit: task=%s receipt=%s", taskStatus, receiptStatus)
	}
}

func TestTaskSupplementPermissionAndTenantIsolation(t *testing.T) {
	fixture := newSupplementFixture(t, "codex", "running", true)
	otherUser := dbfx.Insert(t, "user", testutil.Cols{"name": "No Invoke", "email": "no-invoke-supplement@example.test"})
	dbfx.Member(t, testWorkspaceID, otherUser, "member")
	req := newRequestAs(otherUser, http.MethodPost, "/supplements", map[string]any{
		"client_request_id": "0199a4e8-22ce-7b01-bba5-555555555555", "content": "not allowed",
	})
	testutil.Call(t, testHandler.CreateTaskSupplement,
		withURLParams(req, "id", fixture.issueID, "taskId", fixture.taskID)).Want(http.StatusForbidden)

	foreignIssueID, foreignTaskID := setupForeignWorkspaceFixture(t)
	crossTenant := newRequest(http.MethodPost, "/api/issues/"+foreignIssueID+"/tasks/"+foreignTaskID+"/supplements", map[string]any{
		"client_request_id": "0199a4e8-22ce-7b01-bba5-666666666666", "content": "cross tenant",
	})
	testutil.Call(t, testHandler.CreateTaskSupplement,
		withURLParams(crossTenant, "id", foreignIssueID, "taskId", foreignTaskID)).Want(http.StatusNotFound)

	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1`, fixture.taskID).Scan(&count)
	if count != 0 {
		t.Fatalf("denied requests created %d supplements", count)
	}
}
