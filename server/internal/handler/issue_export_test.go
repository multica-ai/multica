package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExportIssue_ReturnsMarkdownDownload(t *testing.T) {
	ctx := context.Background()
	issueID := createIssueForTimeline(t, "Export issue test")

	if _, err := testPool.Exec(ctx, `
		UPDATE issue
		SET description = 'Review this exported issue.', metadata = '{"pipeline_status":"waiting_review"}'::jsonb
		WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("seed issue metadata: %v", err)
	}
	seedTimelineEntries(t, issueID, 1, 1)

	agentID := createHandlerTestAgent(t, "Export Agent", nil)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, started_at, completed_at,
			result, trigger_summary, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, $4, $5, '{"ok":true}'::jsonb, 'Export trigger', '/home/tester/repos/multica')
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), issueID, time.Now().UTC().Add(-2*time.Minute), time.Now().UTC().Add(-time.Minute)).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_message (task_id, seq, type, tool, content, input, output)
		VALUES ($1, 1, 'text', NULL, 'Agent exported this issue.', NULL, NULL)
	`, taskID); err != nil {
		t.Fatalf("seed task message: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/export", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ExportIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ExportIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != issueExportContentType {
		t.Fatalf("Content-Type = %q, want %q", got, issueExportContentType)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, `attachment; filename="issue-HAN-`) || !strings.HasSuffix(got, `-export.md"`) {
		t.Fatalf("Content-Disposition = %q, want issue export attachment", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	body := w.Body.String()
	for _, want := range []string{
		"# HAN-",
		"Export issue test",
		"Review this exported issue.",
		"pipeline_status",
		"## Timeline",
		"comment 0",
		"status_changed",
		"## Agent Runs",
		taskID,
		"Agent exported this issue.",
		"repos/multica",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("export body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/home/tester") {
		t.Fatalf("export body leaked raw home directory: %s", body)
	}
}
