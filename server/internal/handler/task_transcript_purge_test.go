package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func purgeTranscript(t *testing.T, taskID, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodDelete, "/api/tasks/"+taskID+"/messages", nil), userID), "taskId", taskID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	rec := httptest.NewRecorder()
	testHandler.PurgeTaskTranscript(rec, req)
	return rec
}

func taskMessageCount(t *testing.T, taskID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM task_message WHERE task_id = $1`, taskID)
}

func finishTask(t *testing.T, taskID string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now(),
		result = '{"output":"final answer with a secret","comment":"reply-comment","process_cleanup_capability":"v1"}'::jsonb
		WHERE id = $1`, taskID)
}

func TestPurgeTaskTranscript_AdminClearsFinishedRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID, _ := seedTranscript(t, "purge-admin")
	finishTask(t, taskID)

	rec := purgeTranscript(t, taskID, testUserID)
	if rec.Code != http.StatusOK {
		t.Fatalf("purge = %d %s", rec.Code, rec.Body.String())
	}
	var resp TaskTranscriptPurgeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.MessagesDeleted != 3 || resp.TranscriptPurgedBy != testUserID || resp.TranscriptPurgedAt == "" {
		t.Fatalf("purge response = %+v", resp)
	}
	if n := taskMessageCount(t, taskID); n != 0 {
		t.Fatalf("%d transcript rows left", n)
	}

	var result map[string]any
	var raw []byte
	dbfx.QueryRow(t, `SELECT result FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw)
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["output"]; ok {
		t.Error("final output survived the purge")
	}
	if result["comment"] != "reply-comment" || result["process_cleanup_capability"] != "v1" {
		t.Errorf("purge dropped result fields other paths read: %v", result)
	}

	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if got := taskToResponse(task, testWorkspaceID); got.TranscriptPurgedAt == nil || got.TranscriptPurgedBy != testUserID {
		t.Errorf("run response does not carry the purge: at=%v by=%q", got.TranscriptPurgedAt, got.TranscriptPurgedBy)
	}

	// A late daemon flush is acknowledged and dropped.
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 4, "type": "text", "content": "late tail"},
	})).Want(http.StatusOK)
	if n := taskMessageCount(t, taskID); n != 0 {
		t.Fatalf("late flush wrote %d rows into a cleared log", n)
	}

	// Clearing again keeps the first record.
	again := purgeTranscript(t, taskID, testUserID)
	if again.Code != http.StatusOK {
		t.Fatalf("second purge = %d %s", again.Code, again.Body.String())
	}
	var second TaskTranscriptPurgeResponse
	if err := json.Unmarshal(again.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.TranscriptPurgedAt != resp.TranscriptPurgedAt || second.MessagesDeleted != 0 {
		t.Errorf("second purge = %+v, first cleared at %s", second, resp.TranscriptPurgedAt)
	}
}

func TestPurgeTaskTranscript_Refusals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Run("plain member", func(t *testing.T) {
		taskID, _ := seedTranscript(t, "purge-member")
		finishTask(t, taskID)
		member := createTestUserAndMember(t, "member")
		if rec := purgeTranscript(t, taskID, member); rec.Code != http.StatusForbidden {
			t.Fatalf("member purge = %d, want 403", rec.Code)
		}
		if n := taskMessageCount(t, taskID); n != 3 {
			t.Fatalf("refused purge changed the log: %d rows", n)
		}
	})

	t.Run("outsider", func(t *testing.T) {
		taskID, _ := seedTranscript(t, "purge-outsider")
		finishTask(t, taskID)
		outsider := createTestUserAndMember(t, "")
		if rec := purgeTranscript(t, taskID, outsider); rec.Code != http.StatusNotFound {
			t.Fatalf("outsider purge = %d, want 404", rec.Code)
		}
	})

	t.Run("running run", func(t *testing.T) {
		taskID, _ := seedTranscript(t, "purge-running")
		if rec := purgeTranscript(t, taskID, testUserID); rec.Code != http.StatusConflict {
			t.Fatalf("running purge = %d, want 409", rec.Code)
		}
		if n := taskMessageCount(t, taskID); n != 3 {
			t.Fatalf("refused purge changed the log: %d rows", n)
		}
	})
}

func TestPurgeTaskTranscript_DropsAutopilotRunOutput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`, testWorkspaceID).Scan(&agentID)
	taskID := createAutopilotRunOnlyTask(t, agentID)
	finishTask(t, taskID)
	dbfx.Exec(t, `UPDATE autopilot_run SET status = 'completed',
		result = '{"output":"final answer with a secret","pr_url":"https://example.test/pr/1"}'::jsonb
		WHERE id = (SELECT autopilot_run_id FROM agent_task_queue WHERE id = $1)`, taskID)

	if rec := purgeTranscript(t, taskID, testUserID); rec.Code != http.StatusOK {
		t.Fatalf("purge = %d %s", rec.Code, rec.Body.String())
	}
	var hasOutput bool
	var prURL string
	dbfx.QueryRow(t, `SELECT result ? 'output', result->>'pr_url' FROM autopilot_run
		WHERE id = (SELECT autopilot_run_id FROM agent_task_queue WHERE id = $1)`, taskID).Scan(&hasOutput, &prURL)
	if hasOutput || prURL == "" {
		t.Errorf("autopilot run result after purge: has_output=%v pr_url=%q", hasOutput, prURL)
	}
}
