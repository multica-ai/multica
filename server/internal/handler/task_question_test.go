package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// taskQuestionRequest builds the daemon-authenticated POST the daemon sends
// when the agent called AskUserQuestion (GitHub #8048).
func taskQuestionRequest(t *testing.T, taskID string, body map[string]any) *http.Request {
	t.Helper()
	req := testutil.JSONRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/question", body)
	req = testutil.WithURLParams(req, "taskId", taskID)
	return req.WithContext(middleware.WithDaemonContext(
		req.Context(), testWorkspaceID, "task-question-daemon"))
}

// TestReportTaskQuestionPostsMarkedAgentComment pins the whole contract of the
// endpoint: the question lands as an agent-authored `comment` row on the
// task's issue, carrying the normalized snake_case payload the timeline card
// reads, a markdown body that stands on its own for readers without the card,
// and source_task_id so the completion fallback sees the agent already spoke.
func TestReportTaskQuestionPostsMarkedAgentComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := seedBatchTask(t, "task-question")

	var resp TaskQuestionResponse
	testutil.Call(t, testHandler.ReportTaskQuestion, taskQuestionRequest(t, taskID, map[string]any{
		"tool_use_id": "toolu_q1",
		"questions": []any{map[string]any{
			"question":    "Which flag name?",
			"header":      "Flag",
			"multiSelect": false,
			"options": []any{
				map[string]any{"label": "--dry-run", "description": "Conventional"},
				map[string]any{"label": "--preview", "description": "Friendlier"},
			},
		}},
	})).Want(http.StatusOK).JSON(&resp)
	if resp.CommentID == "" {
		t.Fatal("expected the created comment id in the response")
	}

	comment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(resp.CommentID))
	if err != nil {
		t.Fatalf("load created comment: %v", err)
	}
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM comment WHERE id = $1`, resp.CommentID) })

	task, err := testHandler.Queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if comment.AuthorType != "agent" || uuidToString(comment.AuthorID) != uuidToString(task.AgentID) {
		t.Fatalf("comment author = %s/%s, want agent %s", comment.AuthorType, uuidToString(comment.AuthorID), uuidToString(task.AgentID))
	}
	if uuidToString(comment.IssueID) != uuidToString(task.IssueID) {
		t.Fatalf("comment issue = %s, want %s", uuidToString(comment.IssueID), uuidToString(task.IssueID))
	}
	if comment.Type != "comment" {
		t.Fatalf("comment type = %q, want the ordinary comment type", comment.Type)
	}
	if uuidToString(comment.SourceTaskID) != taskID {
		t.Fatalf("source_task_id = %s, want %s", uuidToString(comment.SourceTaskID), taskID)
	}
	if !strings.Contains(comment.Content, "Which flag name?") || !strings.Contains(comment.Content, "--preview") {
		t.Fatalf("markdown body must carry the question and options:\n%s", comment.Content)
	}

	var payload struct {
		Questions []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multi_select"`
			Options     []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(comment.QuestionPayload, &payload); err != nil {
		t.Fatalf("question_payload is not the stored shape: %v\n%s", err, comment.QuestionPayload)
	}
	if len(payload.Questions) != 1 || payload.Questions[0].Header != "Flag" || len(payload.Questions[0].Options) != 2 {
		t.Fatalf("stored payload = %+v", payload)
	}
	if strings.Contains(string(comment.QuestionPayload), "multiSelect") {
		t.Fatalf("stored payload must be snake_case, got %s", comment.QuestionPayload)
	}

	// The API shape the timeline consumes carries the payload verbatim.
	if got := commentToResponse(comment, nil, nil); len(got.QuestionPayload) == 0 {
		t.Fatal("CommentResponse must expose question_payload")
	}
}

func TestReportTaskQuestionRejectsMalformedPayload(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "task-question-bad")

	testutil.Call(t, testHandler.ReportTaskQuestion, taskQuestionRequest(t, taskID, map[string]any{
		"questions": []any{},
	})).Want(http.StatusBadRequest)

	testutil.Call(t, testHandler.ReportTaskQuestion, taskQuestionRequest(t, taskID, map[string]any{
		"questions": []any{map[string]any{"question": "", "options": []any{}}},
	})).Want(http.StatusBadRequest)

	var n int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE source_task_id = $1`, taskID).Scan(&n)
	if n != 0 {
		t.Fatalf("a rejected question must not leave a comment behind, found %d", n)
	}
}
