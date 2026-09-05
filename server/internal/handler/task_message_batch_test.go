package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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

// TestCreateTaskMessageClampsSingleRowDuration covers the single-row insert,
// which no handler currently drives with a duration but any direct caller can:
// outside the handler path a pgtype.Int8 can carry anything, so the query
// itself has to clamp. Same contract as the batch insert — 0 stays NULL (not
// reported) and a negative value is malformed rather than a measurement.
func TestCreateTaskMessageClampsSingleRowDuration(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := util.MustParseUUID(seedBatchTask(t, "single-duration"))

	for i, tc := range []struct {
		name    string
		param   pgtype.Int8
		wantSet bool
		want    int64
	}{
		{"measured duration is stored", pgtype.Int8{Valid: true, Int64: 4321}, true, 4321},
		{"explicit zero stays NULL", pgtype.Int8{Valid: true}, false, 0},
		{"negative stays NULL", pgtype.Int8{Valid: true, Int64: -7}, false, 0},
		{"unset stays NULL", pgtype.Int8{}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := uuid.NewV7()
			if err != nil {
				t.Fatalf("new v7: %v", err)
			}
			row, err := testHandler.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
				ID:         pgtype.UUID{Bytes: id, Valid: true},
				TaskID:     taskID,
				Seq:        int32(i + 1),
				Type:       "tool_result",
				Tool:       pgtype.Text{Valid: true, String: "bash"},
				Output:     pgtype.Text{Valid: true, String: "ok"},
				DurationMs: tc.param,
			})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if row.DurationMs.Valid != tc.wantSet || row.DurationMs.Int64 != tc.want {
				t.Fatalf("duration_ms = %+v, want Valid=%v %d", row.DurationMs, tc.wantSet, tc.want)
			}
		})
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

// TestCreateTaskMessagesBatchIsAtomic exercises the production query, not a copy
// of it: a batch that violates the primary key must persist nothing. This is the
// property the single statement buys over the per-message loop, which could
// leave a prefix of the batch behind and no way to complete it (the daemon does
// not retry this endpoint).
//
// Note what this does and does not guarantee: the batch is consistent, not
// complete. A failing batch is now lost whole. Closing that gap needs a retry
// plus a (task_id, seq) uniqueness rule, which is not part of this change.
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
		TaskID:      util.MustParseUUID(taskID),
		Ids:         []pgtype.UUID{util.MustParseUUID("018f0000-0000-7000-8000-000000000002"), util.MustParseUUID(dupID)},
		Seqs:        []int32{2, 3},
		Types:       []string{"text", "text"},
		Tools:       []string{"", ""},
		Contents:    []string{"ok", "collides"},
		Inputs:      []string{"", ""},
		Outputs:     []string{"", ""},
		DurationMss: []int64{0, 0},
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

// TestReportTaskMessagesRoundTripsDurationMs pins the provider-duration path
// end to end (multica-ai/multica#8025): OpenCode measures each tool call
// itself (part.state.time.start/end), and the run view must show that number —
// not the 0s the arrival-stamp difference collapses to. The duration has to
// survive every hop: HTTP request → task_message column → catch-up response
// AND the realtime publish. Rows without a duration persist NULL and come back
// without the field, so transcripts from runtimes that never measure timing
// are byte-identical to before; a negative value is malformed, not a
// measurement, and lands as unknown rather than poisoning the row.
func TestReportTaskMessagesRoundTripsDurationMs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "batch-duration")

	sharedBus := testHandler.Bus
	testHandler.Bus = events.New()
	t.Cleanup(func() { testHandler.Bus = sharedBus })

	var mu sync.Mutex
	published := map[int]int64{}
	testHandler.Bus.Subscribe(protocol.EventTaskMessage, func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.TaskID != taskID {
			return
		}
		if payload, ok := e.Payload.(protocol.TaskMessagePayload); ok {
			published[payload.Seq] = payload.DurationMs
		}
	})

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "tool_result", "tool": "bash", "output": "ok", "duration_ms": 12345},
		map[string]any{"seq": 2, "type": "tool_result", "tool": "read", "output": "ok"},
		map[string]any{"seq": 3, "type": "tool_result", "tool": "edit", "output": "ok", "duration_ms": -5},
	})).Want(http.StatusOK)

	// Persisted column: measured duration survives; absent and negative land
	// as NULL (unknown) so every other consumer sees exactly the old shape.
	stored, err := testHandler.Queries.ListTaskMessages(context.Background(), util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("list persisted task messages: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("persisted %d rows, want 3", len(stored))
	}
	if !stored[0].DurationMs.Valid || stored[0].DurationMs.Int64 != 12345 {
		t.Fatalf("seq 1 duration_ms = %+v, want 12345", stored[0].DurationMs)
	}
	if stored[1].DurationMs.Valid {
		t.Fatalf("seq 2 duration_ms = %+v, want NULL", stored[1].DurationMs)
	}
	if stored[2].DurationMs.Valid {
		t.Fatalf("seq 3 duration_ms = %+v, want NULL (a negative value is malformed, not a measurement)", stored[2].DurationMs)
	}

	// Catch-up response: the payload carries the duration only where one was
	// reported — 0 stays off the wire via omitempty.
	listReq := testutil.JSONRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/messages", nil)
	listReq = testutil.WithURLParams(listReq, "taskId", taskID)
	listReq = listReq.WithContext(middleware.WithDaemonContext(
		listReq.Context(), testWorkspaceID, "batch-messages-daemon"))
	resp := testutil.Call(t, testHandler.ListTaskMessages, listReq).Want(http.StatusOK)
	var got []protocol.TaskMessagePayload
	resp.JSON(&got)
	if len(got) != 3 {
		t.Fatalf("catch-up returned %d payloads, want 3", len(got))
	}
	if got[0].DurationMs != 12345 {
		t.Fatalf("catch-up seq 1 duration_ms = %d, want 12345", got[0].DurationMs)
	}
	if got[1].DurationMs != 0 {
		t.Fatalf("catch-up seq 2 duration_ms = %d, want 0 (omitted)", got[1].DurationMs)
	}
	// Raw body, not just the decoded struct: absence is the compatibility
	// contract — an old client parsing this response must not see the field.
	if !strings.Contains(resp.Body.String(), `"duration_ms":12345`) {
		t.Fatalf("catch-up body missing duration_ms for seq 1: %s", resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), `"duration_ms":-`) {
		t.Fatalf("catch-up body carries a negative duration_ms: %s", resp.Body.String())
	}

	// Realtime publish: subscribers render these events as they arrive, so
	// the live transcript shows the same number the row stored.
	mu.Lock()
	defer mu.Unlock()
	for seq, want := range map[int]int64{1: 12345, 2: 0, 3: 0} {
		if got := published[seq]; got != want {
			t.Fatalf("published seq %d duration_ms = %d, want %d", seq, got, want)
		}
	}
}
