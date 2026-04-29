package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func createExecutionTestIssue(t *testing.T, title string, number int) string {
	t.Helper()

	ctx := context.Background()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, $2, 'todo', 'medium', 'member', $3, $4, 0)
		RETURNING id
	`, testWorkspaceID, title, testUserID, number).Scan(&issueID); err != nil {
		t.Fatalf("create test issue: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	return issueID
}

func createExecutionTestComment(t *testing.T, issueID, content string) string {
	t.Helper()

	ctx := context.Background()
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, content).Scan(&commentID); err != nil {
		t.Fatalf("create test comment: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})

	return commentID
}

func createExecutionTestTask(
	t *testing.T,
	agentID string,
	issueID string,
	status string,
	priority int,
	triggerCommentID string,
	errorText string,
	timestampColumn string,
) string {
	t.Helper()

	ctx := context.Background()
	query := `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, trigger_comment_id, error, created_at, %s
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, ''), now() - interval '1 minute', now())
		RETURNING id
	`
	if timestampColumn == "" {
		query = `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority, trigger_comment_id, error, created_at
			)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, ''), now())
			RETURNING id
		`
	} else {
		query = fmt.Sprintf(query, timestampColumn)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, query, agentID, handlerTestRuntimeID(t), issueID, status, priority, triggerCommentID, errorText).Scan(&taskID); err != nil {
		t.Fatalf("create test task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	return taskID
}

func createForeignWorkspaceIssueWithTask(t *testing.T) string {
	t.Helper()

	ctx := context.Background()

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES (
			'Foreign Execution Workspace',
			'foreign-execution-' || substring(gen_random_uuid()::text, 1, 8),
			'',
			'FOR'
		)
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Foreign Runtime', 'cloud', 'foreign_runtime', 'online', 'foreign runtime', '{}'::jsonb, now())
		RETURNING id
	`, workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create foreign runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Foreign Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, workspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'Foreign issue', 'todo', 'medium', 'member', $2, 9911, 0)
		RETURNING id
	`, workspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create foreign issue: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 1)
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("create foreign task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	return issueID
}

func subscribeTaskEventForIssue(eventType, issueID string) <-chan string {
	ch := make(chan string, 16)
	testHandler.Bus.Subscribe(eventType, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || payload["issue_id"] != issueID {
			return
		}
		taskID, _ := payload["task_id"].(string)
		select {
		case ch <- taskID:
		default:
		}
	})
	return ch
}

func waitForTaskEvent(t *testing.T, ch <-chan string, eventType string) string {
	t.Helper()
	select {
	case taskID := <-ch:
		return taskID
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", eventType)
		return ""
	}
}

func TestGetIssueExecutionSummaries_AggregatesPerIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "execution-summary-agent", nil)
	queuedIssueID := createExecutionTestIssue(t, "Queued execution summary issue", 9301)
	runningIssueID := createExecutionTestIssue(t, "Running execution summary issue", 9302)
	failedIssueID := createExecutionTestIssue(t, "Failed execution summary issue", 9303)
	idleIssueID := createExecutionTestIssue(t, "Idle execution summary issue", 9304)

	queuedCommentID := createExecutionTestComment(t, queuedIssueID, "Queued trigger")
	runningCommentID := createExecutionTestComment(t, runningIssueID, "Running trigger")

	createExecutionTestTask(t, agentID, queuedIssueID, "queued", 2, queuedCommentID, "", "")
	createExecutionTestTask(t, agentID, runningIssueID, "running", 1, runningCommentID, "", "started_at")
	createExecutionTestTask(t, agentID, failedIssueID, "failed", 1, "", "boom", "completed_at")
	foreignIssueID := createForeignWorkspaceIssueWithTask(t)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/issues/execution-summary", nil)

	testHandler.GetIssueExecutionSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Summaries []IssueExecutionSummaryResponse `json:"summaries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	byIssueID := make(map[string]IssueExecutionSummaryResponse, len(resp.Summaries))
	for _, summary := range resp.Summaries {
		byIssueID[summary.IssueID] = summary
	}

	queuedSummary, ok := byIssueID[queuedIssueID]
	if !ok {
		t.Fatalf("missing queued issue summary")
	}
	if queuedSummary.State != "queued" || queuedSummary.QueuedCount != 1 {
		t.Fatalf("queued summary mismatch: %+v", queuedSummary)
	}
	if queuedSummary.LatestTriggerExcerpt == nil || *queuedSummary.LatestTriggerExcerpt != "Queued trigger" {
		t.Fatalf("expected queued trigger excerpt, got %+v", queuedSummary.LatestTriggerExcerpt)
	}

	runningSummary, ok := byIssueID[runningIssueID]
	if !ok {
		t.Fatalf("missing running issue summary")
	}
	if runningSummary.State != "running" || runningSummary.RunningCount != 1 {
		t.Fatalf("running summary mismatch: %+v", runningSummary)
	}

	failedSummary, ok := byIssueID[failedIssueID]
	if !ok {
		t.Fatalf("missing failed issue summary")
	}
	if failedSummary.State != "failed" {
		t.Fatalf("failed summary mismatch: %+v", failedSummary)
	}
	if failedSummary.LatestError == nil || *failedSummary.LatestError != "boom" {
		t.Fatalf("expected failed error, got %+v", failedSummary.LatestError)
	}

	idleSummary, ok := byIssueID[idleIssueID]
	if !ok {
		t.Fatalf("missing idle issue summary")
	}
	if idleSummary.State != "idle" {
		t.Fatalf("idle summary mismatch: %+v", idleSummary)
	}

	if _, ok := byIssueID[foreignIssueID]; ok {
		t.Fatalf("foreign workspace issue should not be included")
	}
}

func TestGetIssueExecutionSummaries_PaginatesIssues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	firstIssueID := createExecutionTestIssue(t, "Paged execution summary issue 1", 9311)
	secondIssueID := createExecutionTestIssue(t, "Paged execution summary issue 2", 9312)
	thirdIssueID := createExecutionTestIssue(t, "Paged execution summary issue 3", 9313)

	ctx := context.Background()
	updates := []struct {
		id       string
		interval string
	}{
		{firstIssueID, "30 minutes"},
		{secondIssueID, "20 minutes"},
		{thirdIssueID, "10 minutes"},
	}
	for _, update := range updates {
		if _, err := testPool.Exec(ctx, `UPDATE issue SET created_at = now() + $2::interval WHERE id = $1`, update.id, update.interval); err != nil {
			t.Fatalf("update issue created_at: %v", err)
		}
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/issues/execution-summary?limit=1&offset=1", nil)

	testHandler.GetIssueExecutionSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Summaries []IssueExecutionSummaryResponse `json:"summaries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Summaries) != 1 {
		t.Fatalf("expected one paged summary, got %d", len(resp.Summaries))
	}
	if resp.Summaries[0].IssueID != secondIssueID {
		t.Fatalf("expected second newest issue %s, got %s", secondIssueID, resp.Summaries[0].IssueID)
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/issues/execution-summary?issue_id="+firstIssueID+"&limit=1&offset=99", nil)

	testHandler.GetIssueExecutionSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for issue-scoped summary, got %d: %s", w.Code, w.Body.String())
	}
	resp.Summaries = nil
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode issue-scoped response: %v", err)
	}
	if len(resp.Summaries) != 1 {
		t.Fatalf("expected one issue-scoped summary, got %d", len(resp.Summaries))
	}
	if resp.Summaries[0].IssueID != firstIssueID {
		t.Fatalf("expected issue-scoped summary %s, got %s", firstIssueID, resp.Summaries[0].IssueID)
	}
}

func TestGetIssueExecutionSummaries_PrefersRunningTaskAsLatest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "execution-summary-latest-agent", nil)
	issueID := createExecutionTestIssue(t, "Running plus queued issue", 9314)
	runningCommentID := createExecutionTestComment(t, issueID, "Active running trigger")
	queuedCommentID := createExecutionTestComment(t, issueID, "New queued follow-up")

	createExecutionTestTask(t, agentID, issueID, "running", 1, runningCommentID, "", "started_at")
	createExecutionTestTask(t, agentID, issueID, "queued", 2, queuedCommentID, "", "")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/issues/execution-summary?limit=1000", nil)

	testHandler.GetIssueExecutionSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Summaries []IssueExecutionSummaryResponse `json:"summaries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var summary *IssueExecutionSummaryResponse
	for i := range resp.Summaries {
		if resp.Summaries[i].IssueID == issueID {
			summary = &resp.Summaries[i]
			break
		}
	}
	if summary == nil {
		t.Fatalf("missing issue summary")
	}
	if summary.State != "running" || summary.RunningCount != 1 || summary.QueuedCount != 1 {
		t.Fatalf("running+queued summary mismatch: %+v", summary)
	}
	if summary.LatestTriggerExcerpt == nil || *summary.LatestTriggerExcerpt != "Active running trigger" {
		t.Fatalf("expected running trigger as latest/current trigger, got %+v", summary.LatestTriggerExcerpt)
	}
}

func TestTaskServicePublishesQueuedAndCancelledEvents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "task-event-agent", nil)
	issueID := createExecutionTestIssue(t, "Task event issue", 9315)
	if _, err := testPool.Exec(ctx, `
		UPDATE issue SET assignee_type = 'agent', assignee_id = $1
		WHERE id = $2
	`, agentID, issueID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	queuedCh := subscribeTaskEventForIssue(protocol.EventTaskQueued, issueID)
	cancelledCh := subscribeTaskEventForIssue(protocol.EventTaskCancelled, issueID)

	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	if got := waitForTaskEvent(t, queuedCh, protocol.EventTaskQueued); got != uuidToString(task.ID) {
		t.Fatalf("queued event task id mismatch: want %s, got %s", uuidToString(task.ID), got)
	}

	if err := testHandler.TaskService.CancelTasksForIssue(ctx, issue.ID); err != nil {
		t.Fatalf("cancel issue tasks: %v", err)
	}
	if got := waitForTaskEvent(t, cancelledCh, protocol.EventTaskCancelled); got != uuidToString(task.ID) {
		t.Fatalf("cancelled event task id mismatch: want %s, got %s", uuidToString(task.ID), got)
	}
}

func TestCreateCommentTriggersQueuedExecutionSummary(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "comment-queued-agent", nil)
	issueID := createExecutionTestIssue(t, "Comment queued issue", 9316)
	if _, err := testPool.Exec(ctx, `
		UPDATE issue SET assignee_type = 'agent', assignee_id = $1
		WHERE id = $2
	`, agentID, issueID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}

	queuedCh := subscribeTaskEventForIssue(protocol.EventTaskQueued, issueID)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Please continue from this comment",
	})
	req = withURLParam(req, "id", issueID)

	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	waitForTaskEvent(t, queuedCh, protocol.EventTaskQueued)

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/issues/execution-summary?limit=1000", nil)

	testHandler.GetIssueExecutionSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Summaries []IssueExecutionSummaryResponse `json:"summaries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, summary := range resp.Summaries {
		if summary.IssueID != issueID {
			continue
		}
		if summary.State != "queued" || summary.QueuedCount != 1 {
			t.Fatalf("expected queued summary after comment, got %+v", summary)
		}
		if summary.LatestTriggerExcerpt == nil || *summary.LatestTriggerExcerpt != "Please continue from this comment" {
			t.Fatalf("expected comment trigger excerpt, got %+v", summary.LatestTriggerExcerpt)
		}
		return
	}
	t.Fatalf("missing summary for comment-triggered issue")
}

func TestListAgentTasks_EnrichesIssueAndQueueMetadata(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "agent-task-enrichment", nil)
	runtimeID := handlerTestRuntimeID(t)

	blockedIssueID := createExecutionTestIssue(t, "Blocked queue issue", 9401)
	claimableIssueID := createExecutionTestIssue(t, "Claimable queue issue", 9402)
	issueTriggeredID := createExecutionTestIssue(t, "Issue triggered issue", 9403)

	blockedCommentID := createExecutionTestComment(t, blockedIssueID, "Please investigate blocker")
	claimableCommentID := createExecutionTestComment(t, claimableIssueID, "Queued from comment")

	var runningTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, started_at, created_at
		)
		VALUES ($1, $2, $3, $4, 'running', 5, now(), now() - interval '3 minutes')
		RETURNING id
	`, agentID, runtimeID, blockedIssueID, blockedCommentID).Scan(&runningTaskID); err != nil {
		t.Fatalf("create running task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, runningTaskID)
	})

	var blockedQueuedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at
		)
		VALUES ($1, $2, $3, $4, 'queued', 4, now() - interval '2 minutes')
		RETURNING id
	`, agentID, runtimeID, blockedIssueID, blockedCommentID).Scan(&blockedQueuedTaskID); err != nil {
		t.Fatalf("create blocked queued task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, blockedQueuedTaskID)
	})

	var claimableQueuedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at
		)
		VALUES ($1, $2, $3, $4, 'queued', 3, now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, claimableIssueID, claimableCommentID).Scan(&claimableQueuedTaskID); err != nil {
		t.Fatalf("create claimable queued task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, claimableQueuedTaskID)
	})

	var issueTriggeredTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, completed_at, created_at
		)
		VALUES ($1, $2, $3, 'completed', 1, now(), now() - interval '4 minutes')
		RETURNING id
	`, agentID, runtimeID, issueTriggeredID).Scan(&issueTriggeredTaskID); err != nil {
		t.Fatalf("create issue-triggered task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, issueTriggeredTaskID)
	})

	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES (
			'Foreign Agent Task Workspace',
			'foreign-agent-task-' || substring(gen_random_uuid()::text, 1, 8),
			'',
			'FAT'
		)
		RETURNING id
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}

	var foreignIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'Foreign task issue', 'todo', 'medium', 'member', $2, 9501, 0)
		RETURNING id
	`, foreignWorkspaceID, testUserID).Scan(&foreignIssueID); err != nil {
		t.Fatalf("create foreign issue: %v", err)
	}

	var foreignIssueTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, completed_at, created_at
		)
		VALUES ($1, $2, $3, 'failed', 1, now(), now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, foreignIssueID).Scan(&foreignIssueTaskID); err != nil {
		t.Fatalf("create foreign issue task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, foreignIssueTaskID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, foreignIssueID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil), "id", agentID)

	testHandler.ListAgentTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	byTaskID := make(map[string]AgentTaskResponse, len(resp))
	for _, task := range resp {
		byTaskID[task.ID] = task
	}

	blockedTask, ok := byTaskID[blockedQueuedTaskID]
	if !ok {
		t.Fatalf("missing blocked queued task")
	}
	if blockedTask.TriggerSource != "message" {
		t.Fatalf("expected message trigger source, got %q", blockedTask.TriggerSource)
	}
	if blockedTask.TriggerExcerpt == nil || *blockedTask.TriggerExcerpt != "Please investigate blocker" {
		t.Fatalf("expected blocked trigger excerpt, got %+v", blockedTask.TriggerExcerpt)
	}
	if blockedTask.QueueBlockedReason == nil || *blockedTask.QueueBlockedReason == "" {
		t.Fatalf("expected blocked reason, got %+v", blockedTask.QueueBlockedReason)
	}
	if blockedTask.IssueIdentifier == nil || *blockedTask.IssueIdentifier != "HAN-9401" {
		t.Fatalf("expected issue identifier HAN-9401, got %+v", blockedTask.IssueIdentifier)
	}

	claimableTask, ok := byTaskID[claimableQueuedTaskID]
	if !ok {
		t.Fatalf("missing claimable queued task")
	}
	if claimableTask.QueuePosition == nil || *claimableTask.QueuePosition != 1 {
		t.Fatalf("expected queue position 1, got %+v", claimableTask.QueuePosition)
	}
	if claimableTask.QueueAheadCount == nil || *claimableTask.QueueAheadCount != 0 {
		t.Fatalf("expected queue ahead count 0, got %+v", claimableTask.QueueAheadCount)
	}
	if claimableTask.IssueTitle == nil || *claimableTask.IssueTitle != "Claimable queue issue" {
		t.Fatalf("expected issue title, got %+v", claimableTask.IssueTitle)
	}

	issueTriggeredTask, ok := byTaskID[issueTriggeredTaskID]
	if !ok {
		t.Fatalf("missing issue-triggered task")
	}
	if issueTriggeredTask.TriggerSource != "issue" {
		t.Fatalf("expected issue trigger source, got %q", issueTriggeredTask.TriggerSource)
	}

	foreignIssueTask, ok := byTaskID[foreignIssueTaskID]
	if !ok {
		t.Fatalf("missing foreign issue task")
	}
	if foreignIssueTask.IssueIdentifier != nil || foreignIssueTask.IssueTitle != nil {
		t.Fatalf("foreign workspace issue should not be enriched: %+v", foreignIssueTask)
	}
}
