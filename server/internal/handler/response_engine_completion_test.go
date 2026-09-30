package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encoding/json"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakeTaskResponseFinalizer struct {
	calls  []service.TaskResponseFinalizationInput
	result service.TaskResponseFinalizationResult
	err    error
	onCall func(service.TaskResponseFinalizationInput)
}

func (f *fakeTaskResponseFinalizer) FinalizeTaskCompletion(_ context.Context, input service.TaskResponseFinalizationInput) (service.TaskResponseFinalizationResult, error) {
	f.calls = append(f.calls, input)
	if f.onCall != nil {
		f.onCall(input)
	}
	return f.result, f.err
}

func configureResponseEngineForTest(t *testing.T, mode service.ResponseEngineMode, finalizer service.TaskResponseFinalizer) {
	t.Helper()
	oldMode := testHandler.TaskService.ResponseEngineMode
	oldFinalizer := testHandler.TaskService.ResponseFinalizer
	testHandler.TaskService.ResponseEngineMode = mode
	testHandler.TaskService.ResponseFinalizer = finalizer
	t.Cleanup(func() {
		testHandler.TaskService.ResponseEngineMode = oldMode
		testHandler.TaskService.ResponseFinalizer = oldFinalizer
	})
}

func setupResponseEngineIssueTask(t *testing.T, number int32) (agentID, runtimeID, issueID, taskID string) {
	t.Helper()
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)
	setWorkspaceIssuePrefixForTest(t, "RSP")
	issueID = dbfx.Issue(t, fmt.Sprintf("response-engine-%d", number), testutil.Cols{
		"status": "in_progress",
		"number": number,
	})
	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
		"status":     "running",
		"attempt":    3,
		"started_at": testutil.Raw("now()"),
	})
	return
}

func completeResponseEngineTask(t *testing.T, taskID, output string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output}, testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	return w
}

func latestAgentIssueComment(t *testing.T, issueID, agentID string) string {
	t.Helper()
	var content string
	dbfx.QueryRow(t, `
		SELECT content FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, issueID, agentID).Scan(&content)
	return content
}

func TestCompleteTask_ResponseEngineLegacyDoesNotCallFinalizer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91001)
	fake := &fakeTaskResponseFinalizer{result: service.TaskResponseFinalizationResult{Rendered: "rendered"}}
	configureResponseEngineForTest(t, service.ResponseEngineLegacy, fake)

	w := completeResponseEngineTask(t, taskID, "raw legacy")
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status=%d body=%s", w.Code, w.Body.String())
	}
	if len(fake.calls) != 0 {
		t.Fatalf("legacy called finalizer %d times", len(fake.calls))
	}
	if got := latestAgentIssueComment(t, issueID, agentID); got != "raw legacy" {
		t.Fatalf("comment=%q, want raw legacy", got)
	}
}

func TestCompleteTask_ResponseEngineObserveCallsFinalizerButPersistsRaw(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91002)
	childID := dbfx.Issue(t, "child observe", testutil.Cols{
		"status":          "todo",
		"number":          91003,
		"parent_issue_id": issueID,
	})
	_ = childID
	fake := &fakeTaskResponseFinalizer{result: service.TaskResponseFinalizationResult{Rendered: "shadow-rendered"}}
	configureResponseEngineForTest(t, service.ResponseEngineObserve, fake)

	w := completeResponseEngineTask(t, taskID, "raw observe")
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status=%d body=%s", w.Code, w.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("observe finalizer calls=%d, want 1", len(fake.calls))
	}
	in := fake.calls[0]
	if in.TaskID != taskID || in.WorkspaceID != testWorkspaceID || in.RawOutput != "raw observe" {
		t.Fatalf("unexpected finalization input: %+v", in)
	}
	if in.Attempt != 3 {
		t.Fatalf("attempt=%d, want 3", in.Attempt)
	}
	if in.Issue == nil || in.Issue.Identifier != "RSP-91002" || in.Issue.Revision < 1 {
		t.Fatalf("issue snapshot=%+v", in.Issue)
	}
	if len(in.Children) != 1 || in.Children[0].Identifier != "RSP-91003" {
		t.Fatalf("children=%+v", in.Children)
	}
	if got := latestAgentIssueComment(t, issueID, agentID); got != "raw observe" {
		t.Fatalf("comment=%q, want raw observe", got)
	}
}

func TestCompleteTask_ResponseEngineObserveFailureDoesNotBlockLegacyOutput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91004)
	fake := &fakeTaskResponseFinalizer{err: errors.New("shadow unavailable")}
	configureResponseEngineForTest(t, service.ResponseEngineObserve, fake)

	w := completeResponseEngineTask(t, taskID, "raw survives")
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status=%d body=%s", w.Code, w.Body.String())
	}
	if got := latestAgentIssueComment(t, issueID, agentID); got != "raw survives" {
		t.Fatalf("comment=%q, want raw survives", got)
	}
}

func TestCompleteTask_ResponseEngineEnforcePersistsRenderedOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91005)
	fake := &fakeTaskResponseFinalizer{result: service.TaskResponseFinalizationResult{Rendered: "[Goal]\nSystem-rendered"}}
	configureResponseEngineForTest(t, service.ResponseEngineEnforce, fake)

	w := completeResponseEngineTask(t, taskID, "RAW-MUST-NOT-LEAK")
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status=%d body=%s", w.Code, w.Body.String())
	}
	got := latestAgentIssueComment(t, issueID, agentID)
	if got != "[Goal]\nSystem-rendered" {
		t.Fatalf("comment=%q, want rendered response", got)
	}
	if got == "RAW-MUST-NOT-LEAK" {
		t.Fatal("raw output leaked in enforce mode")
	}
}

func TestCompleteTask_ResponseEngineEnforceAlwaysPostsTerminalRenderedComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91010)
	dbfx.Exec(t, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'agent', $3, 'progress update before terminal', 'comment')
	`, issueID, testWorkspaceID, agentID)

	fake := &fakeTaskResponseFinalizer{
		result: service.TaskResponseFinalizationResult{Rendered: "[Goal]\nstandard terminal response"},
	}
	configureResponseEngineForTest(t, service.ResponseEngineEnforce, fake)

	w := completeResponseEngineTask(t, taskID, "RAW TERMINAL")
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status=%d body=%s", w.Code, w.Body.String())
	}

	rows, err := testPool.Query(context.Background(), `
		SELECT content FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2
		ORDER BY created_at ASC
	`, issueID, agentID)
	if err != nil {
		t.Fatalf("query comments: %v", err)
	}
	defer rows.Close()

	var contents []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			t.Fatalf("scan comment: %v", err)
		}
		contents = append(contents, content)
	}
	if len(contents) != 2 {
		t.Fatalf("agent comments=%v, want progress + one terminal rendered comment", contents)
	}
	if contents[1] != fake.result.Rendered {
		t.Fatalf("terminal comment=%q, want %q", contents[1], fake.result.Rendered)
	}
	if strings.Contains(strings.Join(contents, "\n"), "RAW TERMINAL") {
		t.Fatalf("raw terminal output leaked into issue comments: %v", contents)
	}
}

func TestCompleteTask_ResponseEngineEnforcePreservesLongStandardizedResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91012)
	rendered := "[Goal]\nLong standardized response\n\nทำอะไรไป?\n" +
		strings.Repeat("x", 8250) +
		"\n\nและต่อไปคืออะไร?\nตรวจสอบผล\n\nReason:\nเพื่อรักษา contract สี่ส่วน"
	fake := &fakeTaskResponseFinalizer{
		result: service.TaskResponseFinalizationResult{Rendered: rendered},
	}
	configureResponseEngineForTest(t, service.ResponseEngineEnforce, fake)

	w := completeResponseEngineTask(t, taskID, "RAW LONG OUTPUT")
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status=%d body=%s", w.Code, w.Body.String())
	}

	var content string
	dbfx.QueryRow(t, `
		SELECT content FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2 AND source_task_id=$3
	`, issueID, agentID, taskID).Scan(&content)
	if content != rendered {
		t.Fatalf("terminal comment lost standardized response contract; len=%d want=%d", len([]rune(content)), len([]rune(rendered)))
	}
	if strings.Contains(content, "RAW LONG OUTPUT") {
		t.Fatalf("raw output leaked into terminal comment")
	}
}

func TestCompleteTask_ResponseEngineEnforceFailureKeepsTaskRunningAndRawUnpersisted(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91006)
	fake := &fakeTaskResponseFinalizer{err: errors.New("finalizer unavailable")}
	configureResponseEngineForTest(t, service.ResponseEngineEnforce, fake)

	w := completeResponseEngineTask(t, taskID, "RAW-MUST-NOT-PERSIST")
	if w.Code < 500 {
		t.Fatalf("CompleteTask status=%d body=%s, want 5xx", w.Code, w.Body.String())
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
	if status != "running" {
		t.Fatalf("task status=%q, want running", status)
	}
	var count int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2
	`, issueID, agentID).Scan(&count)
	if count != 0 {
		t.Fatalf("agent comments=%d, want 0", count)
	}
}

func TestCompleteTask_ResponseEngineEnforceRejectsIssueRevisionChangeDuringFinalization(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91007)
	fake := &fakeTaskResponseFinalizer{
		result: service.TaskResponseFinalizationResult{Rendered: "rendered stale"},
		onCall: func(service.TaskResponseFinalizationInput) {
			dbfx.Exec(t, `UPDATE issue SET revision = revision + 1, updated_at=now() WHERE id=$1`, issueID)
		},
	}
	configureResponseEngineForTest(t, service.ResponseEngineEnforce, fake)

	w := completeResponseEngineTask(t, taskID, "raw stale")
	if w.Code < 500 {
		t.Fatalf("CompleteTask status=%d body=%s, want 5xx", w.Code, w.Body.String())
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
	if status != "running" {
		t.Fatalf("task status=%q, want running", status)
	}
	var count int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2
	`, issueID, agentID).Scan(&count)
	if count != 0 {
		t.Fatalf("agent comments=%d, want 0; stale rendered output must not persist", count)
	}
}

func TestCompleteTaskWithResponseFence_RejectsStaleIssueRevisionInsideTransaction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91009)

	var revision int64
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, issueID).Scan(&revision)
	dbfx.Exec(t, `UPDATE issue SET revision=revision+1, updated_at=now() WHERE id=$1`, issueID)

	result, err := json.Marshal(protocol.TaskCompletedPayload{Output: "[Goal]\nrendered"})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	_, err = testHandler.TaskService.CompleteTaskWithResponseFence(
		context.Background(),
		parseUUID(taskID),
		result,
		"", "", "", false, "", "",
		&service.TaskResponseCompletionFence{
			IssueID:     parseUUID(issueID),
			WorkspaceID: parseUUID(testWorkspaceID),
			Revision:    revision,
		},
	)
	if !errors.Is(err, service.ErrResponseSnapshotStale) {
		t.Fatalf("CompleteTaskWithResponseFence error=%v, want ErrResponseSnapshotStale", err)
	}

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
	if status != "running" {
		t.Fatalf("task status=%q, want running", status)
	}
	var count int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2
	`, issueID, agentID).Scan(&count)
	if count != 0 {
		t.Fatalf("agent comments=%d, want 0 after stale fenced completion", count)
	}
}

func TestCompleteTask_ResponseEngineEnforceRetryBackfillsMissingTerminalComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91011)
	fake := &fakeTaskResponseFinalizer{
		result: service.TaskResponseFinalizationResult{Rendered: "[Goal]\nrendered crash-safe"},
	}
	configureResponseEngineForTest(t, service.ResponseEngineEnforce, fake)

	first := completeResponseEngineTask(t, taskID, "raw first")
	if first.Code != http.StatusOK {
		t.Fatalf("first CompleteTask status=%d body=%s", first.Code, first.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("first finalizer calls=%d, want 1", len(fake.calls))
	}

	dbfx.Exec(t, `
		DELETE FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2 AND source_task_id=$3
	`, issueID, agentID, taskID)

	var beforeRetry int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2 AND source_task_id=$3
	`, issueID, agentID, taskID).Scan(&beforeRetry)
	if beforeRetry != 0 {
		t.Fatalf("terminal comments before retry=%d, want 0", beforeRetry)
	}

	fake.err = errors.New("finalizer must not be called on terminal retry")
	testHandler.TaskService.ResponseEngineMode = service.ResponseEngineLegacy
	second := completeResponseEngineTask(t, taskID, "RAW RETRY MUST NOT LEAK")
	if second.Code != http.StatusOK {
		t.Fatalf("retry CompleteTask status=%d body=%s", second.Code, second.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("retry called finalizer; calls=%d, want 1", len(fake.calls))
	}

	var contents []string
	rows, err := testPool.Query(context.Background(), `
		SELECT content FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2 AND source_task_id=$3
		ORDER BY created_at ASC
	`, issueID, agentID, taskID)
	if err != nil {
		t.Fatalf("query terminal comments: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			t.Fatalf("scan terminal comment: %v", err)
		}
		contents = append(contents, content)
	}
	if len(contents) != 1 {
		t.Fatalf("terminal comments=%v, want exactly one backfilled comment", contents)
	}
	if contents[0] != fake.result.Rendered {
		t.Fatalf("backfilled comment=%q, want persisted rendered %q", contents[0], fake.result.Rendered)
	}
	if strings.Contains(strings.Join(contents, "\n"), "RAW RETRY MUST NOT LEAK") {
		t.Fatalf("raw retry leaked into terminal comments: %v", contents)
	}
}

func TestCompleteTask_ResponseEngineEnforceTerminalRetrySkipsFinalizer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, issueID, taskID := setupResponseEngineIssueTask(t, 91008)
	fake := &fakeTaskResponseFinalizer{
		result: service.TaskResponseFinalizationResult{Rendered: "[Goal]\nrendered once"},
	}
	configureResponseEngineForTest(t, service.ResponseEngineEnforce, fake)

	first := completeResponseEngineTask(t, taskID, "raw first")
	if first.Code != http.StatusOK {
		t.Fatalf("first CompleteTask status=%d body=%s", first.Code, first.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("first finalizer calls=%d, want 1", len(fake.calls))
	}

	fake.err = errors.New("finalizer must not be called on terminal retry")
	second := completeResponseEngineTask(t, taskID, "raw retry")
	if second.Code != http.StatusOK {
		t.Fatalf("retry CompleteTask status=%d body=%s", second.Code, second.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("retry called finalizer; calls=%d, want 1", len(fake.calls))
	}
	if got := latestAgentIssueComment(t, issueID, agentID); got != "[Goal]\nrendered once" {
		t.Fatalf("comment=%q, want original rendered response", got)
	}
}
