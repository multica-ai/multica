package handler

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestLongReplyAttachmentCompletionDoesNotEnqueueTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	originalStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = originalStorage })

	agentID := createHandlerTestAgent(t, "Long reply agent", []byte("[]"))
	issueID := dbfx.Issue(t, "Long reply", testutil.Cols{
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "reply.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(strings.Repeat("界", 8001))); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("issue_id", issueID); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Actor-Source", "task_token")
	var attachment AttachmentResponse
	testutil.Call(t, testHandler.UploadFile, req).Want(http.StatusOK).JSON(&attachment)
	if attachment.ID == "" || attachment.IssueID == nil || *attachment.IssueID != issueID {
		t.Fatalf("attachment not persisted on issue: %+v", attachment)
	}
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM attachment WHERE id = $1`, attachment.ID) })

	output := fmt.Sprintf("> Reply excerpt\n\nFull reply attached as Markdown (8001 characters).\n\n!file[reply.md](%s)", attachment.MarkdownURL)
	complete := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": output})
	testutil.Call(t, testHandler.CompleteTask, testutil.WithURLParams(complete, "taskId", taskID)).Want(http.StatusOK)

	var comment string
	dbfx.QueryRow(t, `SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2`, issueID, agentID).Scan(&comment)
	if comment != output {
		t.Fatalf("completion comment = %q, want attachment card", comment)
	}
	var tasks int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&tasks)
	if tasks != 1 {
		t.Fatalf("issue tasks = %d, want only the original task", tasks)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "completed" {
		t.Fatalf("original task status = %q", status)
	}
}

func TestLongReplyUploadFailureRetainsServerNotice(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Long reply fallback agent", []byte("[]"))
	issueID := dbfx.Issue(t, "Long reply fallback", testutil.Cols{"status": "in_progress"})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
	// The daemon preserves this exact output after upload failure.
	complete := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": strings.Repeat("界", 8001)})
	testutil.Call(t, testHandler.CompleteTask, testutil.WithURLParams(complete, "taskId", taskID)).Want(http.StatusOK)
	var comment string
	dbfx.QueryRow(t, `SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2`, issueID, agentID).Scan(&comment)
	if !strings.Contains(comment, "not posted") {
		t.Fatalf("existing oversized-output notice lost: %q", comment)
	}
}
