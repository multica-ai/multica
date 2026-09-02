package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// seedBatchTask builds the running, issue-backed task the /messages endpoint
// needs: requireDaemonTaskAccess resolves the workspace through the issue, and a
// task with no issue / chat / autopilot link 404s before the body is read.
func seedBatchTask(t *testing.T, label string) string {
	t.Helper()
	agentID := dbfx.Agent(t, label+" agent", handlerTestRuntimeID(t), testutil.Cols{
		"instructions": "",
		"custom_env":   testutil.Raw("'{}'::jsonb"),
		"custom_args":  testutil.Raw("'[]'::jsonb"),
	})
	issueID := dbfx.Issue(t, label+" fixture", testutil.Cols{"status": "in_progress"})
	return dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
}

// batchMessagesRequest builds the daemon-authenticated POST the daemon sends,
// with taskId bound as a chi URL param the way the router does in production.
func batchMessagesRequest(t *testing.T, taskID string, messages []any) *http.Request {
	t.Helper()
	req := testutil.JSONRequest(http.MethodPost,
		"/api/daemon/tasks/"+taskID+"/messages", map[string]any{"messages": messages})
	req = testutil.WithURLParams(req, "taskId", taskID)
	return req.WithContext(middleware.WithDaemonContext(
		req.Context(), testWorkspaceID, "batch-messages-daemon"))
}

// TestReportTaskMessagesPersistsWholeBatch covers the multi-message shape of the
// /messages endpoint after it stopped issuing one INSERT per message. The row
// count, the seq values, and the NULL/non-NULL split all have to survive the
// jsonb round trip CreateTaskMessages does: a tool event carries tool + input +
// output, a text event carries only content, and everything the daemon omitted
// must land as SQL NULL rather than an empty string.
func TestReportTaskMessagesPersistsWholeBatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := seedBatchTask(t, "batch-messages")

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "thinking", "content": "planning"},
		map[string]any{
			"seq":    2,
			"type":   "tool_use",
			"tool":   "fs_read",
			"input":  map[string]any{"path": "/etc/hosts", "nested": map[string]any{"depth": 2}},
			"output": "127.0.0.1 localhost",
		},
		map[string]any{"seq": 3, "type": "text", "content": "done"},
	})).Want(http.StatusOK)

	stored, err := testHandler.Queries.ListTaskMessages(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("list persisted task messages: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("persisted %d task messages, want 3", len(stored))
	}
	for i, want := range []int32{1, 2, 3} {
		if stored[i].Seq != want {
			t.Fatalf("row %d seq = %d, want %d", i, stored[i].Seq, want)
		}
	}

	if stored[0].Type != "thinking" || stored[0].Content.String != "planning" {
		t.Fatalf("thinking row = %+v, want type=thinking content=planning", stored[0])
	}
	// Fields the daemon did not send must be NULL, not "".
	if stored[0].Tool.Valid || stored[0].Output.Valid || stored[0].Input != nil {
		t.Fatalf("thinking row should have NULL tool/output/input, got %+v", stored[0])
	}
	if stored[1].Tool.String != "fs_read" {
		t.Fatalf("tool_use row tool = %q, want fs_read", stored[1].Tool.String)
	}
	if stored[1].Output.String != "127.0.0.1 localhost" {
		t.Fatalf("tool_use row output = %q", stored[1].Output.String)
	}
	if stored[1].Content.Valid {
		t.Fatalf("tool_use row content should be NULL, got %q", stored[1].Content.String)
	}
	// The jsonb argument must arrive as an object, not as a string holding JSON,
	// or `input->>'path'` stops resolving for every consumer of this column.
	var inputPath string
	dbfx.QueryRow(t,
		`SELECT input->>'path' FROM task_message WHERE task_id = $1 AND seq = 2`,
		taskID).Scan(&inputPath)
	if inputPath != "/etc/hosts" {
		t.Fatalf("input->>path = %q, want /etc/hosts", inputPath)
	}
	if stored[2].Type != "text" || stored[2].Content.String != "done" {
		t.Fatalf("text row = %+v, want type=text content=done", stored[2])
	}
}

// TestReportTaskMessagesPublishesInSeqOrder pins the realtime ordering the batch
// insert has to preserve. The per-message loop it replaced published in request
// order; `INSERT ... RETURNING` has no row-order guarantee, so the order now
// comes from CreateTaskMessages' ORDER BY seq. Subscribers render these events
// as they arrive, so an out-of-order batch is a visible transcript bug.
//
// The request deliberately lists the messages out of seq order: if the ORDER BY
// were dropped, the events would come back in array order and this fails. That
// also makes seq — the daemon's own counter — the single ordering authority,
// rather than however the rows happen to land.
func TestReportTaskMessagesPublishesInSeqOrder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "batch-order")

	// The shared bus has no unsubscribe, so a listener registered on it would
	// outlive this test and keep firing for the rest of the package. Swap in a
	// bus of this test's own and put the shared one back afterwards.
	sharedBus := testHandler.Bus
	testHandler.Bus = events.New()
	t.Cleanup(func() { testHandler.Bus = sharedBus })

	var mu sync.Mutex
	var gotSeqs []int
	testHandler.Bus.Subscribe(protocol.EventTaskMessage, func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.TaskID != taskID {
			return
		}
		if payload, ok := e.Payload.(protocol.TaskMessagePayload); ok {
			gotSeqs = append(gotSeqs, payload.Seq)
		}
	})

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 3, "type": "text", "content": "third"},
		map[string]any{"seq": 1, "type": "text", "content": "first"},
		map[string]any{"seq": 2, "type": "text", "content": "second"},
	})).Want(http.StatusOK)

	mu.Lock()
	defer mu.Unlock()
	want := []int{1, 2, 3}
	if len(gotSeqs) != len(want) {
		t.Fatalf("published %v task:message events, want seqs %v", gotSeqs, want)
	}
	for i := range want {
		if gotSeqs[i] != want[i] {
			t.Fatalf("published seq order = %v, want %v — subscribers render these "+
				"events in arrival order, so the transcript would show the batch scrambled",
				gotSeqs, want)
		}
	}
}

func TestReportTaskMessagesReplayIsIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "batch-replay")

	sharedBus := testHandler.Bus
	testHandler.Bus = events.New()
	t.Cleanup(func() { testHandler.Bus = sharedBus })

	var mu sync.Mutex
	published := 0
	testHandler.Bus.Subscribe(protocol.EventTaskMessage, func(e events.Event) {
		if e.TaskID == taskID {
			mu.Lock()
			published++
			mu.Unlock()
		}
	})

	messages := []any{
		map[string]any{"seq": 1, "type": "tool_use", "tool": "exec", "input": map[string]any{"cmd": "true", "nested": map[string]any{"ok": true}}},
		map[string]any{"seq": 2, "type": "text", "content": "second"},
	}
	for range 2 {
		testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, messages)).Want(http.StatusOK)
	}

	if count := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1`, taskID); count != 2 {
		t.Fatalf("persisted rows = %d, want 2", count)
	}
	mu.Lock()
	defer mu.Unlock()
	if published != 2 {
		t.Fatalf("published events = %d, want 2 (replay must publish none)", published)
	}
}

func TestReportTaskMessagesRejectsConflictingReplayAtomically(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "batch-conflicting-replay")
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "original"},
	})).Want(http.StatusOK)

	// The new seq must roll back with the altered replay; accepting only the
	// new row would turn one rejected batch into a partial transcript.
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "tool_use", "tool": "exec", "content": "altered"},
		map[string]any{"seq": 2, "type": "text", "content": "must roll back"},
	})).Want(http.StatusConflict)

	stored, err := testHandler.Queries.ListTaskMessages(context.Background(), util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(stored) != 1 || stored[0].Type != "text" || stored[0].Content.String != "original" {
		t.Fatalf("conflicting replay changed transcript: %+v", stored)
	}
}

func TestReportTaskMessagesRejectsInvalidBatchSeq(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "batch-invalid-seq")

	tests := []struct {
		name     string
		messages []any
	}{
		{"zero", []any{map[string]any{"seq": 0, "type": "text", "content": "bad"}}},
		{"overflow", []any{map[string]any{"seq": 2147483648, "type": "text", "content": "bad"}}},
		{"duplicate", []any{
			map[string]any{"seq": 1, "type": "text", "content": "first"},
			map[string]any{"seq": 1, "type": "text", "content": "different"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, tt.messages)).Want(http.StatusBadRequest)
		})
	}
	if count := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1`, taskID); count != 0 {
		t.Fatalf("persisted rows = %d, want 0", count)
	}
}

func TestCompleteTaskFinalizesIncompleteTranscriptAsFailure(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := seedBatchTask(t, "terminal-transcript-receipt")

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "first"},
		map[string]any{"seq": 3, "type": "text", "content": "third"},
	})).Want(http.StatusOK)

	complete := func() *http.Request {
		return daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/complete", taskID, map[string]any{
			"output":               "done",
			"expected_message_seq": 3,
		})
	}
	testutil.Call(t, testHandler.CompleteTask, complete()).Want(http.StatusOK)
	// A lost response replays the original completion callback. Once the
	// transcript gap has been persisted as a terminal failure, that replay is
	// idempotent rather than returning 5xx forever.
	testutil.Call(t, testHandler.CompleteTask, complete()).Want(http.StatusOK)

	var status, taskErr, failureReason string
	if err := testPool.QueryRow(ctx, `
		SELECT status, error, failure_reason FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&status, &taskErr, &failureReason); err != nil {
		t.Fatalf("read task after incomplete completion: %v", err)
	}
	if status != "failed" || failureReason != taskfailure.ReasonTranscriptIncomplete.String() || !strings.Contains(taskErr, "transcript incomplete") {
		t.Fatalf("terminal task = status %q error %q reason %q", status, taskErr, failureReason)
	}
}

func TestCompleteTaskReceiptRejectsLateTranscriptWrite(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "terminal-transcript-seal")
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "only"},
	})).Want(http.StatusOK)
	testutil.Call(t, testHandler.CompleteTask, daemonTaskRequest(t,
		"/api/daemon/tasks/"+taskID+"/complete", taskID, map[string]any{
			"output": "done", "expected_message_seq": 1,
		})).Want(http.StatusOK)

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 2, "type": "text", "content": "late"},
	})).Want(http.StatusConflict)
	if count := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1`, taskID); count != 1 {
		t.Fatalf("late write changed sealed transcript row count to %d", count)
	}
}

func TestReportTaskMessagesAcceptsCancelledTaskFinalDrain(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "cancelled-transcript-final-drain")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'cancelled' WHERE id = $1`, taskID)

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "final drain"},
	})).Want(http.StatusOK)
	if count := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1`, taskID); count != 1 {
		t.Fatalf("cancelled task final drain persisted %d rows, want 1", count)
	}
}

func TestCompleteTaskAcceptsEmptyTranscriptReceipt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	emptyTaskID := seedBatchTask(t, "empty-transcript-receipt")
	testutil.Call(t, testHandler.CompleteTask, daemonTaskRequest(t,
		"/api/daemon/tasks/"+emptyTaskID+"/complete", emptyTaskID, map[string]any{
			"output": "done", "expected_message_seq": 0,
		})).Want(http.StatusOK)

	nonemptyTaskID := seedBatchTask(t, "invalid-empty-transcript-receipt")
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, nonemptyTaskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "present"},
	})).Want(http.StatusOK)
	testutil.Call(t, testHandler.CompleteTask, daemonTaskRequest(t,
		"/api/daemon/tasks/"+nonemptyTaskID+"/complete", nonemptyTaskID, map[string]any{
			"output": "done", "expected_message_seq": 0,
		})).Want(http.StatusOK)
}

func TestFailTaskFinalizesIncompleteTranscriptAsFailure(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := seedBatchTask(t, "failed-transcript-receipt")
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 2, "type": "text", "content": "second"},
	})).Want(http.StatusOK)

	fail := func() *http.Request {
		return daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/fail", taskID, map[string]any{
			"error":                "agent failed",
			"failure_reason":       "agent_error.unknown",
			"expected_message_seq": 2,
		})
	}
	testutil.Call(t, testHandler.FailTask, fail()).Want(http.StatusOK)
	// Retry the exact callback to cover a lost successful HTTP response.
	testutil.Call(t, testHandler.FailTask, fail()).Want(http.StatusOK)

	var status, taskErr, failureReason string
	if err := testPool.QueryRow(ctx, `
		SELECT status, error, failure_reason FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&status, &taskErr, &failureReason); err != nil {
		t.Fatalf("read failed task: %v", err)
	}
	if status != "failed" || failureReason != taskfailure.ReasonTranscriptIncomplete.String() || !strings.Contains(taskErr, "transcript incomplete") {
		t.Fatalf("terminal task = status %q error %q reason %q", status, taskErr, failureReason)
	}
}

// TestCreateTaskMessagesBatchIsAtomic exercises the production query, not a copy
// of it: a batch that violates the primary key must persist nothing. This is the
// property the single statement buys over the per-message loop. Delivery
// completeness is covered separately by retry, (task_id, seq) uniqueness and
// the terminal contiguous-sequence receipt.
func TestCreateTaskMessagesBatchIsAtomic(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := seedBatchTask(t, "batch-atomic")

	// The colliding row is built as a fixture, not through the query under
	// test: test setup that shares code with the code being exercised can pass
	// for the wrong reason.
	const dupID = "018f0000-0000-7000-8000-000000000001"
	dbfx.Insert(t, "task_message", testutil.Cols{
		"id":      dupID,
		"task_id": taskID,
		"seq":     1,
		"type":    "text",
		"content": "first",
	})

	_, err := testHandler.Queries.CreateTaskMessages(ctx, db.CreateTaskMessagesParams{
		TaskID:   util.MustParseUUID(taskID),
		Ids:      []pgtype.UUID{util.MustParseUUID("018f0000-0000-7000-8000-000000000002"), util.MustParseUUID(dupID)},
		Seqs:     []int32{2, 3},
		Types:    []string{"text", "text"},
		Tools:    []string{"", ""},
		Contents: []string{"ok", "collides"},
		Inputs:   []string{"", ""},
		Outputs:  []string{"", ""},
	})
	if err == nil {
		t.Fatal("CreateTaskMessages accepted a batch with a duplicate primary key")
	}

	count := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1`, taskID)
	if count != 1 {
		t.Fatalf("task_message rows after the failed batch = %d, want 1 (only the seeded row) — "+
			"the batch persisted a prefix instead of rolling back", count)
	}
}
