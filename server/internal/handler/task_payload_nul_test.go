package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedNULTask creates an agent with one running task, ready to be driven
// through a terminal daemon callback.
func seedNULTask(t *testing.T, label string) (agentID, taskID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, testWorkspaceID, label, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	// The task needs an issue: requireDaemonTaskAccess resolves the workspace
	// through it, and a task with no issue / chat / autopilot link 404s before
	// the handler ever reads the body.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, $2, 'in_progress', 'none', $3, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), 0)
		RETURNING id`, testWorkspaceID, label+" fixture", testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now())
		RETURNING id`, agentID, handlerTestRuntimeID(t), issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return agentID, taskID
}

// daemonTaskRequest builds a daemon-authenticated request with taskId bound as
// a chi URL param, the way the router does in production.
func daemonTaskRequest(t *testing.T, path, taskID string, body any) *http.Request {
	t.Helper()
	req := newDaemonTokenRequest("POST", path, body, testWorkspaceID, "nul-regression-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestCompleteTaskCallbackWithNULSucceeds is the /complete half of GH #7098,
// driven through the real handler.
//
// The NUL travels the way the report describes: encoding/json escapes it on the
// wire and the decoder rebuilds it, so the handler sees a genuine 0x00 the way
// a daemon would deliver it. Going through CompleteTask (rather than calling
// the sanitizer by hand) is what makes this a regression test for the WIRING —
// delete the sanitize call in the handler and this fails.
func TestCompleteTaskCallbackWithNULSucceeds(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "nul-complete-agent")

	w := httptest.NewRecorder()
	req := daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/complete", taskID, map[string]any{
		"output":           "done\x00 summary text",
		"work_dir":         "/tmp/work\x00dir",
		"durable_work_dir": "/Users/dev/pro\x00ject",
		"session_id":       "sess-nul-complete",
		"warnings":         []string{"cleanup\x00 pending"},
	})

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask returned %d, want 200: %s", w.Code, w.Body.String())
	}

	var (
		status      string
		completedAt *string
		storedJSON  []byte
	)
	if err := testPool.QueryRow(ctx, `
		SELECT status, completed_at::text, result
		FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &completedAt, &storedJSON); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status = %q, want completed", status)
	}
	if completedAt == nil {
		t.Fatal("completed_at is NULL, want a terminal timestamp")
	}

	var stored TaskCompleteRequest
	if err := json.Unmarshal(storedJSON, &stored); err != nil {
		t.Fatalf("decode stored result: %v", err)
	}
	// Removing rather than rejecting: the readable text survives.
	if stored.Output != "done summary text" {
		t.Fatalf("stored output = %q, want %q", stored.Output, "done summary text")
	}
	if stored.WorkDir != "/tmp/workdir" {
		t.Fatalf("stored work_dir = %q, want %q", stored.WorkDir, "/tmp/workdir")
	}
	if stored.DurableWorkDir != "/Users/dev/project" {
		t.Fatalf("stored durable_work_dir = %q, want %q", stored.DurableWorkDir, "/Users/dev/project")
	}
	if len(stored.Warnings) != 1 || stored.Warnings[0] != "cleanup pending" {
		t.Fatalf("stored warnings = %#v, want sanitized cleanup warning", stored.Warnings)
	}
}

// TestReportTaskMessagesCallbackWithNULSucceeds covers the /messages entry,
// again through the real handler.
//
// Three poisoned places at once, because they fail for three different reasons:
// type and output are TEXT columns, and input is JSONB where the offending byte
// sits two levels down inside a tool's arguments while every top-level string
// is clean.
func TestReportTaskMessagesCallbackWithNULSucceeds(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "nul-messages-agent")

	w := httptest.NewRecorder()
	req := daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/messages", taskID, map[string]any{
		"messages": []any{
			map[string]any{
				"seq":     1,
				"type":    "tool_use\x00",
				"tool":    "Bash\x00",
				"content": "reading\x00 the binary",
				"output":  "ELF\x00\x00binary",
				"input": map[string]any{
					"command": "cat",
					"args":    []any{"-n", "build/app.bin"},
					"result": map[string]any{
						"stdout": "ELF\x00\x00binary",
					},
				},
			},
		},
	})

	testHandler.ReportTaskMessages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ReportTaskMessages returned %d, want 200: %s", w.Code, w.Body.String())
	}

	var (
		msgType    string
		msgTool    string
		msgContent string
		msgOutput  string
		msgInput   []byte
	)
	if err := testPool.QueryRow(ctx, `
		SELECT type, COALESCE(tool, ''), COALESCE(content, ''), COALESCE(output, ''), input
		FROM task_message WHERE task_id = $1 AND seq = 1`, taskID).
		Scan(&msgType, &msgTool, &msgContent, &msgOutput, &msgInput); err != nil {
		t.Fatalf("read persisted task message: %v", err)
	}

	if msgType != "tool_use" {
		t.Fatalf("stored type = %q, want %q", msgType, "tool_use")
	}
	if msgTool != "Bash" {
		t.Fatalf("stored tool = %q, want %q", msgTool, "Bash")
	}
	if msgContent != "reading the binary" {
		t.Fatalf("stored content = %q, want %q", msgContent, "reading the binary")
	}
	if msgOutput != "ELFbinary" {
		t.Fatalf("stored output = %q, want %q", msgOutput, "ELFbinary")
	}

	var storedInput struct {
		Result struct {
			Stdout string `json:"stdout"`
		} `json:"result"`
	}
	if err := json.Unmarshal(msgInput, &storedInput); err != nil {
		t.Fatalf("decode stored input: %v", err)
	}
	if storedInput.Result.Stdout != "ELFbinary" {
		t.Fatalf("stored nested stdout = %q, want %q", storedInput.Result.Stdout, "ELFbinary")
	}
}

func TestReportTaskMessagesCreatesUUIDv7(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "uuidv7-messages-agent")

	w := httptest.NewRecorder()
	req := daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/messages", taskID, map[string]any{
		"messages": []any{
			map[string]any{
				"seq":     1,
				"type":    "text",
				"content": "UUIDv7 task message",
			},
		},
	})

	testHandler.ReportTaskMessages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ReportTaskMessages returned %d, want 200: %s", w.Code, w.Body.String())
	}

	var rawID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text
		FROM task_message WHERE task_id = $1 AND seq = 1`, taskID).Scan(&rawID); err != nil {
		t.Fatalf("read persisted task message id: %v", err)
	}
	messageID, err := uuid.Parse(rawID)
	if err != nil {
		t.Fatalf("parse task message id %q: %v", rawID, err)
	}
	if got := messageID.Version(); got != 7 {
		t.Fatalf("task message UUID version = %d, want 7", got)
	}
}

// TestPostgresRejectsUnsanitizedTaskPayloads is the control for both tests
// above: it proves the database really does refuse these payloads, so the
// handler tests are demonstrating a fix rather than a no-op. If PostgreSQL ever
// starts accepting them, this fails and the guards can be reconsidered.
func TestPostgresRejectsUnsanitizedTaskPayloads(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "nul-control-agent")

	t.Run("complete result JSONB", func(t *testing.T) {
		raw, err := json.Marshal(TaskCompleteRequest{Output: "done\x00 summary"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), "\\u0000") {
			t.Fatalf("premise broken: payload carries no NUL escape: %s", raw)
		}
		if _, err := testHandler.Queries.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
			ID:     util.MustParseUUID(taskID),
			Result: raw,
		}); err == nil {
			t.Fatal("PostgreSQL accepted a JSONB payload containing a NUL escape")
		}

		// The rejected write rolled back — which is exactly the reported wedge.
		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
			t.Fatalf("read task: %v", err)
		}
		if status != "running" {
			t.Fatalf("status after rejected write = %q, want running", status)
		}
	})

	t.Run("task message input JSONB", func(t *testing.T) {
		raw, err := json.Marshal(map[string]any{
			"result": map[string]any{"stdout": "ELF\x00binary"},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := testHandler.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
			TaskID: util.MustParseUUID(taskID),
			Seq:    99,
			Type:   "tool_use",
			Input:  raw,
		}); err == nil {
			t.Fatal("PostgreSQL accepted nested JSONB containing a NUL escape")
		}
	})

	t.Run("task message type TEXT", func(t *testing.T) {
		if _, err := testHandler.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
			TaskID: util.MustParseUUID(taskID),
			Seq:    98,
			Type:   "tool_use\x00",
		}); err == nil {
			t.Fatal("PostgreSQL accepted a TEXT value containing a NUL")
		}
	})
}
