package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type historyWriteRecorder struct {
	*httptest.ResponseRecorder
	maxWrite   int
	firstWrite func()
}

func (w *historyWriteRecorder) Write(p []byte) (int, error) {
	if w.firstWrite != nil {
		fn := w.firstWrite
		w.firstWrite = nil
		fn()
	}
	w.maxWrite = max(w.maxWrite, len(p))
	return w.ResponseRecorder.Write(p)
}

func TestTaskHistoryLargeResultsStayCompleteAndStreamed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "History bounds", []byte("[]"))
	issueID := dbfx.Issue(t, "History bounds")
	count := int(historyBatchSize)*3 + 1
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		ids = append(ids, dbfx.Task(t, agentID, testutil.Cols{
			"issue_id": issueID, "runtime_id": handlerTestRuntimeID(t), "status": "completed",
			"created_at": testutil.Raw("'2026-01-01T00:00:00Z'::timestamptz"),
			"result":     testutil.Raw(`jsonb_build_object('value', repeat('x', 65536))`),
			"context":    testutil.Raw(`jsonb_build_object('type', 'quick_create', 'wakeup_id', '00000000-0000-0000-0000-000000000002', 'unused_payload', repeat('y', 262144))`),
		}))
	}
	// A hidden row at a batch boundary must not terminate traversal.
	dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": handlerTestRuntimeID(t), "status": "cancelled", "escalation_for_task_id": ids[0]})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": ids[0], "provider": "openai", "model": "history-model", "input_tokens": 123})
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	for _, tc := range []struct {
		name            string
		handler         http.HandlerFunc
		path, param, id string
	}{
		{"agent", testHandler.ListAgentTasks, "/api/agents/" + agentID + "/tasks?include_usage=true", "id", agentID},
		{"issue", testHandler.ListTasksByIssue, "/api/issues/" + issueID + "/task-runs", "id", issueID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := withURLParam(historyRequest("GET", tc.path, nil), tc.param, tc.id)
			w := &historyWriteRecorder{ResponseRecorder: httptest.NewRecorder()}
			tc.handler(w, req)
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if w.maxWrite > 128*1024 {
				t.Fatalf("buffered oversized history write: %d bytes", w.maxWrite)
			}
			var rows []AgentTaskResponse
			if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
				t.Fatal(err)
			}
			got := make([]string, len(rows))
			usage := 0
			for i, row := range rows {
				got[i] = row.ID
				if row.Kind != "quick_create" || row.WakeupID != "00000000-0000-0000-0000-000000000002" {
					t.Fatalf("lost context projection: %+v", row)
				}
				value := row.Result.(map[string]any)["value"].(string)
				if len(value) != 65536 {
					t.Fatalf("result truncated to %d bytes", len(value))
				}
				for _, u := range row.Usage {
					if u.InputTokens == 123 {
						usage++
					}
				}
			}
			if !reflect.DeepEqual(got, ids) {
				t.Fatalf("history lost/reordered tasks: got %d want %d", len(got), len(ids))
			}
			if usage != 1 {
				t.Fatalf("usage hydrated %d times", usage)
			}
			if w.maxWrite > 128*1024 {
				t.Fatalf("buffered oversized history write: %d bytes", w.maxWrite)
			}
			if w.Header().Get("Content-Length") != "" {
				t.Fatal("large history must stream")
			}
		})
	}
	// Verify SQL strips the large execution-only context before pgx materializes it.
	page, err := testHandler.Queries.ListAgentTaskHistoryPage(context.Background(), db.ListAgentTaskHistoryPageParams{OwnerID: parseUUID(agentID), PageSize: historyBatchSize})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != int(historyBatchSize) {
		t.Fatal(len(page))
	}
	for _, row := range page {
		if len(row.Context) > 200 || strings.Contains(string(row.Context), "unused_payload") {
			t.Fatal("full context fetched")
		}
	}
}

func TestTaskMessageHistoryDuplicateSequencesAndHighWatermark(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "paged history")
	count := int(historyBatchSize)*3 + 1
	for i := 0; i < count; i++ {
		dbfx.Insert(t, "task_message", testutil.Cols{"task_id": taskID, "seq": i/40 - 1, "type": "text", "content": fmt.Sprint(i), "output": strings.Repeat("z", 2048)})
	}
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{{"user", testHandler.ListTaskMessagesByUser}, {"daemon", testHandler.ListTaskMessages}} {
		for _, since := range []string{"", "?since=-1", "?since=0", "?since=99"} {
			t.Run(tc.name+since, func(t *testing.T) {
				req := testutil.WithURLParams(historyRequest("GET", "/api/tasks/"+taskID+"/messages"+since, nil), "taskId", taskID)
				req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "history"))
				w := &historyWriteRecorder{ResponseRecorder: httptest.NewRecorder()}
				tc.handler(w, req)
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				var got []protocol.TaskMessagePayload
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				expected := count
				if since == "?since=-1" {
					expected -= 40
				}
				if since == "?since=0" {
					expected -= 80
				}
				if since == "?since=99" {
					expected = 0
				}
				if len(got) != expected {
					t.Fatalf("got %d messages, want %d", len(got), expected)
				}
				seen := map[string]bool{}
				for i, m := range got {
					if seen[m.Content] {
						t.Fatal("duplicate message", m.Content)
					}
					seen[m.Content] = true
					if i > 0 && got[i-1].Seq > m.Seq {
						t.Fatal("out of order")
					}
				}
			})
		}
	}
	req := testutil.WithURLParams(historyRequest("GET", "/api/tasks/"+taskID+"/messages", nil), "taskId", taskID)
	w := &historyWriteRecorder{ResponseRecorder: httptest.NewRecorder(), firstWrite: func() {
		dbfx.Insert(t, "task_message", testutil.Cols{"task_id": taskID, "seq": 100, "type": "text", "content": "concurrent append"})
	}}
	testHandler.ListTaskMessagesByUser(w, req)
	var got []protocol.TaskMessagePayload
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != count {
		t.Fatalf("read chased a concurrent append: %d", len(got))
	}
	req = testutil.WithURLParams(historyRequest("GET", "/api/tasks/"+taskID+"/messages?since=1", nil), "taskId", taskID)
	testutil.Call(t, testHandler.ListTaskMessagesByUser, req).Want(200).JSON(&got)
	if len(got) != 1 || got[0].Content != "concurrent append" {
		t.Fatalf("next reconnect lost append: %+v", got)
	}
	for _, bad := range []string{"bad", "2147483648", "-2147483649"} {
		req = testutil.WithURLParams(historyRequest("GET", "/api/tasks/"+taskID+"/messages?since="+bad, nil), "taskId", taskID)
		testutil.Call(t, testHandler.ListTaskMessagesByUser, req).Want(400)
	}
}

func TestHistoryTaskResponsePreservesMetadata(t *testing.T) {
	row := db.ListAgentTaskHistoryPageRow{
		ID:     parseUUID("00000000-0000-0000-0000-000000000001"),
		Status: "cancelled", Context: []byte(`{"comment_change_cancelled_task_id":"00000000-0000-0000-0000-000000000001"}`),
		Result:          []byte(`{"large_integer":9007199254740993}`),
		CancelledByType: pgtype.Text{String: "user", Valid: true},
	}
	out := historyTaskResponse(row, "workspace")
	if !out.CancelledByCommentChange || out.CancelledBy == nil {
		t.Fatal("lost cancellation metadata")
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "9007199254740993") {
		t.Fatal("result lost integer precision")
	}
	row.Result = nil
	if historyTaskResponse(row, "workspace").Result != nil {
		t.Fatal("NULL result changed")
	}
}

func historyRequest(method, path string, body any) *http.Request {
	req := newRequest(method, path, body)
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
}

// Fault injection exercises the real handlers after their normal access gates.
type historyFailDB struct {
	db.DBTX
	queryName     string
	failAt, calls int
}

func (d *historyFailDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "-- name: "+d.queryName+" ") {
		d.calls++
		if d.calls == d.failAt {
			return nil, errors.New("injected history database failure")
		}
	}
	return d.DBTX.Query(ctx, sql, args...)
}
func (d *historyFailDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: "+d.queryName+" ") {
		return historyErrorRow{}
	}
	return d.DBTX.QueryRow(ctx, sql, args...)
}

type historyErrorRow struct{}

func (historyErrorRow) Scan(...any) error { return errors.New("injected watermark failure") }

func TestHistoryDatabaseFailuresAndEmptyMessages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "History faults", []byte("[]"))
	issueID := dbfx.Issue(t, "History faults")
	for i := 0; i < int(historyBatchSize)+1; i++ {
		dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": handlerTestRuntimeID(t), "status": "completed"})
	}
	taskID := seedBatchTask(t, "history faults")
	req := testutil.WithURLParams(historyRequest("GET", "/api/tasks/"+taskID+"/messages", nil), "taskId", taskID)
	var empty []protocol.TaskMessagePayload
	testutil.Call(t, testHandler.ListTaskMessagesByUser, req).Want(200).JSON(&empty)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty response = %#v", empty)
	}
	for i := 0; i < int(historyBatchSize)+1; i++ {
		dbfx.Insert(t, "task_message", testutil.Cols{"task_id": taskID, "seq": i, "type": "text"})
	}
	for _, tc := range []struct {
		name    string
		query   string
		path    string
		id      string
		param   string
		handler func(*Handler) http.HandlerFunc
		late    bool
	}{
		{"agent first", "ListAgentTaskHistoryPage", "/api/agents/" + agentID + "/tasks", agentID, "id", func(h *Handler) http.HandlerFunc { return h.ListAgentTasks }, false},
		{"agent later", "ListAgentTaskHistoryPage", "/api/agents/" + agentID + "/tasks", agentID, "id", func(h *Handler) http.HandlerFunc { return h.ListAgentTasks }, true},
		{"agent usage later", "ListAgentTaskUsage", "/api/agents/" + agentID + "/tasks?include_usage=true", agentID, "id", func(h *Handler) http.HandlerFunc { return h.ListAgentTasks }, true},
		{"issue first", "ListIssueTaskHistoryPage", "/api/issues/" + issueID + "/task-runs", issueID, "id", func(h *Handler) http.HandlerFunc { return h.ListTasksByIssue }, false},
		{"issue later", "ListIssueTaskHistoryPage", "/api/issues/" + issueID + "/task-runs", issueID, "id", func(h *Handler) http.HandlerFunc { return h.ListTasksByIssue }, true},
		{"message watermark", "GetTaskMessageHighWatermark", "/api/tasks/" + taskID + "/messages", taskID, "taskId", func(h *Handler) http.HandlerFunc { return h.ListTaskMessagesByUser }, false},
		{"message first", "ListTaskMessagesPage", "/api/tasks/" + taskID + "/messages", taskID, "taskId", func(h *Handler) http.HandlerFunc { return h.ListTaskMessagesByUser }, false},
		{"message later", "ListTaskMessagesPage", "/api/tasks/" + taskID + "/messages", taskID, "taskId", func(h *Handler) http.HandlerFunc { return h.ListTaskMessagesByUser }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := *testHandler
			failAt := 1
			if tc.late {
				failAt = 2
			}
			h.Queries = db.New(&historyFailDB{DBTX: testPool, queryName: tc.query, failAt: failAt})
			req := testutil.WithURLParams(historyRequest("GET", tc.path, nil), tc.param, tc.id)
			w := httptest.NewRecorder()
			if tc.late {
				defer func() {
					if got := recover(); got != http.ErrAbortHandler {
						t.Fatalf("panic=%v", got)
					}
					if json.Valid(w.Body.Bytes()) {
						t.Fatal("partial history accepted")
					}
				}()
			}
			tc.handler(&h)(w, req)
			if tc.late {
				t.Fatal("late failure did not abort")
			}
			if w.Code != 500 {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
	// Issue accounting is deliberately best-effort, unlike explicit agent include_usage.
	h := *testHandler
	h.Queries = db.New(&historyFailDB{DBTX: testPool, queryName: "ListIssueTaskUsageForTasks", failAt: 1})
	req = testutil.WithURLParams(historyRequest("GET", "/api/issues/"+issueID+"/task-runs", nil), "id", issueID)
	var tasks []AgentTaskResponse
	testutil.Call(t, h.ListTasksByIssue, req).Want(200).JSON(&tasks)
	if len(tasks) != int(historyBatchSize)+1 {
		t.Fatal("usage failure lost history")
	}
}

func TestTaskHistoryEmptyResponses(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Empty history", []byte("[]"))
	issueID := dbfx.Issue(t, "Empty history")
	for _, tc := range []struct {
		handler  http.HandlerFunc
		path, id string
	}{
		{testHandler.ListAgentTasks, "/api/agents/" + agentID + "/tasks", agentID},
		{testHandler.ListTasksByIssue, "/api/issues/" + issueID + "/task-runs", issueID},
	} {
		var rows []AgentTaskResponse
		req := testutil.WithURLParams(historyRequest("GET", tc.path, nil), "id", tc.id)
		res := testutil.Call(t, tc.handler, req).Want(200).JSON(&rows)
		if rows == nil || len(rows) != 0 {
			t.Fatalf("empty history = %#v", rows)
		}
		if res.Header().Get("Content-Length") != "3" {
			t.Fatal("empty response lost its length")
		}
	}
}
