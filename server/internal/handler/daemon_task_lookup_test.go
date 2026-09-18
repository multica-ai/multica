package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The daemon interrupts a running agent the moment a task-status poll answers
// `404 task not found` (shouldInterruptAgent → isTaskNotFoundError). So that
// body is a kill signal, and only a lookup that completed and found no task row
// may produce it. Transient failures and authorization misses must stay distinct.

// lookupFaultPool fails one named query and passes everything else through, so
// a single link in ResolveTaskWorkspaceIDChecked can be made to time out while
// the task row itself stays readable.
type lookupFaultPool struct {
	db.DBTX
	query  string
	called bool
}

func (f *lookupFaultPool) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "-- name: "+f.query+" :one") {
		f.called = true
		return &mockRow{err: context.DeadlineExceeded}
	}
	return f.DBTX.QueryRow(ctx, query, args...)
}

// TestGetTaskStatus_DoesNotResolveSourceWorkspace pins the hot-path contract:
// status polling authorizes through the owning agent's workspace and never
// follows optional issue / chat / autopilot links.
func TestGetTaskStatus_DoesNotResolveSourceWorkspace(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "MUL-7259 lookup runtime")
	agentID := dbfx.Agent(t, "MUL-7259 lookup agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 lookup issue")
	chatID := dbfx.ChatSession(t, agentID)
	apID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": testWorkspaceID, "title": "MUL-7259 lookup autopilot",
		"assignee_id": agentID, "status": "paused", "created_by_type": "member", "created_by_id": testUserID,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{"autopilot_id": apID, "source": "manual", "status": "running"})

	for _, tc := range []struct{ query, column, id string }{
		{"GetIssue", "issue_id", issueID},
		{"GetChatSession", "chat_session_id", chatID},
		{"GetAutopilotRun", "autopilot_run_id", runID},
		{"GetAutopilot", "autopilot_run_id", runID},
	} {
		t.Run(tc.query, func(t *testing.T) {
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "status": "running", "started_at": testutil.Raw("now()"), tc.column: tc.id,
			})
			fault := &lookupFaultPool{DBTX: testPool, query: tc.query}
			h := &Handler{Queries: db.New(fault), TaskService: &service.TaskService{Queries: db.New(fault)}}
			req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/status", nil, testWorkspaceID, "test-daemon")
			req = withURLParam(req, "taskId", taskID)

			var response map[string]string
			testutil.Call(t, h.GetTaskStatus, req).Want(http.StatusOK).JSON(&response)
			if fault.called {
				t.Fatalf("status poll unexpectedly executed %s", tc.query)
			}
			if response["status"] != "running" {
				t.Fatalf("status = %q, want running", response["status"])
			}
		})
	}
}

// TestGetTaskStatus_TaskRowPresenceContract distinguishes a genuinely missing
// task from a surviving task whose optional source row has gone away.
func TestGetTaskStatus_TaskRowPresenceContract(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "MUL-7259 absent runtime")
	agentID := dbfx.Agent(t, "MUL-7259 absent agent", runtimeID)

	t.Run("task row missing", func(t *testing.T) {
		missing := uuid.NewString()
		req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+missing+"/status", nil, testWorkspaceID, "test-daemon")
		req = withURLParam(req, "taskId", missing)
		w := testutil.Call(t, testHandler.GetTaskStatus, req).Want(http.StatusNotFound)
		if !strings.Contains(w.Body.String(), "task not found") {
			t.Fatalf("a genuinely missing task must still interrupt the daemon: %s", w.Body.String())
		}
	})

	t.Run("optional source missing", func(t *testing.T) {
		// agent_task_queue.issue_id is ON DELETE CASCADE, so an issue task can
		// never outlive its issue. chat_session_id is ON DELETE SET NULL, so a
		// chat task can survive its source. The status endpoint now authorizes
		// that row through its owning agent instead of treating the optional
		// source as task identity.
		chatID := dbfx.ChatSession(t, agentID)
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "status": "running",
			"started_at": testutil.Raw("now()"), "chat_session_id": chatID,
		})
		dbfx.Exec(t, "DELETE FROM chat_session WHERE id = $1", chatID)

		req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/status", nil, testWorkspaceID, "test-daemon")
		req = withURLParam(req, "taskId", taskID)
		var response map[string]string
		testutil.Call(t, testHandler.GetTaskStatus, req).Want(http.StatusOK).JSON(&response)
		if response["status"] != "running" {
			t.Fatalf("status = %q, want running", response["status"])
		}
	})
}

// TestGetTaskStatus_ForeignWorkspace_Returns404 pins the permission boundary:
// splitting lookup failures out of the 404 must not turn a cross-workspace task
// into a distinguishable response. A foreign task and a missing one look alike.
func TestGetTaskStatus_ForeignWorkspace_Returns404(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "MUL-7259 foreign runtime")
	agentID := dbfx.Agent(t, "MUL-7259 foreign agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 foreign issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID,
		"status": "running", "started_at": testutil.Raw("now()"),
	})

	otherWorkspace := uuid.NewString()
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/status", nil, otherWorkspace, "other-daemon")
	req = withURLParam(req, "taskId", taskID)
	w := testutil.Call(t, testHandler.GetTaskStatus, req).Want(http.StatusNotFound)
	if strings.Contains(w.Body.String(), "task not found") {
		t.Fatalf("authorization miss must not carry the daemon's deletion signal: %s", w.Body.String())
	}
}

// TestResolveTaskWorkspaceIDChecked_SeparatesAbsenceFromFailure asserts the
// distinction at the service boundary, where the two callers now rely on it.
func TestResolveTaskWorkspaceIDChecked_SeparatesAbsenceFromFailure(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 resolver runtime")
	agentID := dbfx.Agent(t, "MUL-7259 resolver agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 resolver issue")

	t.Run("resolves", func(t *testing.T) {
		taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
		if err != nil {
			t.Fatal(err)
		}
		got, err := testHandler.TaskService.ResolveTaskWorkspaceIDChecked(ctx, task)
		if err != nil || got != testWorkspaceID {
			t.Fatalf("workspace=%q err=%v, want %q / nil", got, err, testWorkspaceID)
		}
	})

	t.Run("absent link is not an error", func(t *testing.T) {
		chatID := dbfx.ChatSession(t, agentID)
		taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": chatID})
		dbfx.Exec(t, "DELETE FROM chat_session WHERE id = $1", chatID)
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
		if err != nil {
			t.Fatal(err)
		}
		got, err := testHandler.TaskService.ResolveTaskWorkspaceIDChecked(ctx, task)
		if got != "" || err != nil {
			t.Fatalf("workspace=%q err=%v, want \"\" / nil", got, err)
		}
	})

	t.Run("failed lookup is an error", func(t *testing.T) {
		taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
		if err != nil {
			t.Fatal(err)
		}
		svc := &service.TaskService{Queries: db.New(&lookupFaultPool{DBTX: testPool, query: "GetIssue"})}
		got, err := svc.ResolveTaskWorkspaceIDChecked(ctx, task)
		if got != "" || err == nil {
			t.Fatalf("workspace=%q err=%v, want \"\" / error", got, err)
		}
		// The best-effort wrapper keeps flattening both cases to "" — broadcast
		// callers depend on that and must not start panicking on a blip.
		if ws := svc.ResolveTaskWorkspaceID(ctx, task); ws != "" {
			t.Fatalf("best-effort resolver should still return \"\", got %q", ws)
		}
	})
}
