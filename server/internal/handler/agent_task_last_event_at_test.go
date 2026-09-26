package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestAgentTaskLastEventAt pins the liveness contract of last_event_at on
// `GET /api/agents/{id}/tasks`, the endpoint behind `multica agent tasks
// --output json`.
//
// The phases run in order against one task because the contract is about a
// task's lifetime, not five independent facts; each is named so a failure says
// which promise broke. Message batches go through ReportTaskMessages rather
// than straight to the query, because the endpoint rejects any event time more
// than maxTaskMessageClockSkew from server now and substitutes database time
// for the whole batch — a reordering test that fed the query directly could
// assert on input production can never deliver.
func TestAgentTaskLastEventAt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "AgentTaskLastEvent", []byte("[]"))
	// The batch endpoint resolves the workspace through the task's issue, so an
	// unlinked task 404s before the body is read.
	issueID := dbfx.Issue(t, "last-event-at fixture", testutil.Cols{"status": "in_progress"})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})

	// Read the field back off the endpoint rather than the column, so what is
	// asserted is the wire contract an orchestrator actually consumes.
	lastEventAt := func(t *testing.T) *string {
		t.Helper()
		req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil)
		req = withURLParam(req, "id", agentID)
		var resp []AgentTaskResponse
		testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&resp)
		for _, task := range resp {
			if task.ID == taskID {
				return task.LastEventAt
			}
		}
		t.Fatalf("task %s missing from response", taskID)
		return nil
	}
	wantInstant := func(t *testing.T, got *string, want time.Time) {
		t.Helper()
		if got == nil {
			t.Fatalf("last_event_at is null, want %s", want.Format(time.RFC3339Nano))
		}
		parsed, err := time.Parse(time.RFC3339Nano, *got)
		if err != nil {
			t.Fatalf("last_event_at %q is not RFC3339: %v", *got, err)
		}
		if !parsed.Equal(want) {
			t.Fatalf("last_event_at = %s, want %s", parsed.UTC(), want)
		}
	}
	// PostgreSQL timestamptz keeps microseconds; offsets stay well inside
	// maxTaskMessageClockSkew so the endpoint accepts the daemon's event times
	// instead of falling back to database time.
	now := time.Now().UTC().Truncate(time.Microsecond)
	report := func(t *testing.T, msgs ...any) {
		t.Helper()
		testutil.Call(t, testHandler.ReportTaskMessages,
			batchMessagesRequest(t, taskID, msgs)).Want(http.StatusOK)
	}
	msg := func(seq int, at time.Time) any {
		return map[string]any{
			"seq": seq, "type": "text", "content": "x",
			"created_at": at.Format(time.RFC3339Nano),
		}
	}

	newest := now.Add(-10 * time.Second)

	t.Run("null before the first message", func(t *testing.T) {
		if got := lastEventAt(t); got != nil {
			t.Fatalf("last_event_at = %q before any message, want null", *got)
		}
		// The KEY has to be present while the value is null, or a caller cannot
		// tell "this run is silent" from "this server predates the field".
		// LastEventAt carries no omitempty while several of its neighbours in
		// AgentTaskResponse do, so a consistency tidy-up is the likeliest future
		// edit to break this — hence asserting on raw JSON, not the struct.
		req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil)
		req = withURLParam(req, "id", agentID)
		w := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK)
		var raw []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode raw task list: %v", err)
		}
		for _, task := range raw {
			if task["id"] != taskID {
				continue
			}
			if _, present := task["last_event_at"]; !present {
				t.Fatal("last_event_at key absent from the JSON of a task with no messages")
			}
			return
		}
		t.Fatalf("task %s missing from raw response", taskID)
	})

	t.Run("advances to the newest event in a batch, not the last row", func(t *testing.T) {
		// seq orders the batch; the daemon's event clock need not agree with it.
		report(t, msg(1, newest), msg(2, now.Add(-15*time.Second)))
		wantInstant(t, lastEventAt(t), newest)
	})

	t.Run("an older batch never walks it backwards", func(t *testing.T) {
		// Inside the skew window, so this batch keeps its own event times and
		// genuinely reaches the query out of order — the case GREATEST exists
		// for. The flusher's posts can overlap, so commit order is not event
		// order in production either.
		report(t, msg(3, now.Add(-40*time.Second)))
		wantInstant(t, lastEventAt(t), newest)
	})

	t.Run("the single-row writer advances it too", func(t *testing.T) {
		// Takes the column default for created_at, so it lands at transaction
		// time — after every event seeded above.
		if _, err := testHandler.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
			ID:     pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true},
			TaskID: parseUUID(taskID),
			Seq:    4,
			Type:   "text",
		}); err != nil {
			t.Fatalf("single-row write: %v", err)
		}
		got := lastEventAt(t)
		if got == nil {
			t.Fatal("last_event_at is null after the single-row write")
		}
		parsed, err := time.Parse(time.RFC3339Nano, *got)
		if err != nil {
			t.Fatalf("last_event_at %q is not RFC3339: %v", *got, err)
		}
		if !parsed.After(newest) {
			t.Fatalf("single-row write left last_event_at at %s, want later than %s", parsed.UTC(), newest)
		}
	})

	t.Run("frozen by the terminal transition", func(t *testing.T) {
		// The real completion mutation, not a fixture UPDATE: what is asserted
		// is that the completion path does not touch the column.
		before := lastEventAt(t)
		if _, err := testHandler.Queries.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
			ID:     parseUUID(taskID),
			Result: []byte(`{"summary":"done"}`),
		}); err != nil {
			t.Fatalf("complete task: %v", err)
		}
		after := lastEventAt(t)
		if after == nil || *after != *before {
			t.Fatalf("last_event_at = %v after completion, want it frozen at %q", after, *before)
		}
	})
}
