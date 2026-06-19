package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createCommentTriggerPreviewIssue(t *testing.T, title string, assigneeType, assigneeID string) string {
	t.Helper()
	ctx := context.Background()

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	var assigneeTypeArg any
	var assigneeIDArg any
	if assigneeType != "" {
		assigneeTypeArg = assigneeType
	}
	if assigneeID != "" {
		assigneeIDArg = assigneeID
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
		VALUES ($1, 'member', $2, $3, $4, $5, $6)
		RETURNING id
	`, testWorkspaceID, testUserID, title, assigneeTypeArg, assigneeIDArg, number).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	return issueID
}

func previewCommentTriggersForTest(t *testing.T, issueID string, body map[string]any) CommentTriggerPreviewResponse {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments/trigger-preview", body)
	r = withURLParam(r, "id", issueID)
	testHandler.PreviewCommentTriggers(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewCommentTriggers: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentTriggerPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	return resp
}

func postCommentForTriggerPreviewTest(t *testing.T, issueID string, body map[string]any) string {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}
	return resp.ID
}

func updateCommentForTriggerPreviewTest(t *testing.T, commentID string, body map[string]any) {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPut, "/api/comments/"+commentID, body)
	r = withURLParam(r, "commentId", commentID)
	testHandler.UpdateComment(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func countQueuedCommentTriggerTasks(t *testing.T, issueID, agentID string) int {
	t.Helper()

	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	return n
}

func createCommentTriggerPreviewSquad(t *testing.T, name, leaderID string) string {
	t.Helper()

	var squadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})
	return squadID
}

func requirePreviewAgents(t *testing.T, preview CommentTriggerPreviewResponse, wantIDs ...string) {
	t.Helper()
	if len(preview.Agents) != len(wantIDs) {
		t.Fatalf("preview agents = %+v, want ids %v", preview.Agents, wantIDs)
	}
	got := make(map[string]struct{}, len(preview.Agents))
	for _, agent := range preview.Agents {
		got[agent.ID] = struct{}{}
	}
	for _, want := range wantIDs {
		if _, ok := got[want]; !ok {
			t.Fatalf("preview agents = %+v, missing id %s", preview.Agents, want)
		}
	}
}

func TestPreviewCommentTriggers_ReturnsMentionedAgentsAndSuppressFiltersCreate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentA := createHandlerTestAgent(t, "Preview Mention A", nil)
	agentB := createHandlerTestAgent(t, "Preview Mention B", nil)
	issueID := createCommentTriggerPreviewIssue(t, "comment trigger preview mentions", "", "")
	content := fmt.Sprintf("[@A](mention://agent/%s) [@B](mention://agent/%s) please inspect", agentA, agentB)

	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if got := len(preview.Agents); got != 2 {
		t.Fatalf("expected 2 preview agents, got %d: %+v", got, preview.Agents)
	}
	for _, agent := range preview.Agents {
		if agent.Source != string(commentTriggerSourceMentionAgent) {
			t.Fatalf("preview source = %q, want %q", agent.Source, commentTriggerSourceMentionAgent)
		}
	}

	postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content":            content,
		"suppress_agent_ids": []string{agentB},
	})

	if got := countQueuedCommentTriggerTasks(t, issueID, agentA); got != 1 {
		t.Fatalf("unsuppressed mentioned agent queued tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, agentB); got != 0 {
		t.Fatalf("suppressed mentioned agent queued tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_EditExcludesSameCommentPendingTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Edit Preview Exclude Agent", nil)

	t.Run("agent assignee on-comment", func(t *testing.T) {
		issueID := createCommentTriggerPreviewIssue(t, "edit preview assignee", "agent", agentID)
		commentID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{
			"content": "please start here",
		})
		if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, issueID, map[string]any{
			"content":            "actually start over here",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceIssueAssignee) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceIssueAssignee)
		}
	})

	t.Run("squad assignee on-comment", func(t *testing.T) {
		squadID := createCommentTriggerPreviewSquad(t, "Edit Preview Assignee Squad", agentID)
		issueID := createCommentTriggerPreviewIssue(t, "edit preview squad assignee", "squad", squadID)
		commentID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{
			"content": "please coordinate this",
		})
		if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, issueID, map[string]any{
			"content":            "actually coordinate this instead",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceIssueAssignee) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceIssueAssignee)
		}
	})

	t.Run("direct agent mention", func(t *testing.T) {
		issueID := createCommentTriggerPreviewIssue(t, "edit preview agent mention", "", "")
		content := fmt.Sprintf("[@Agent](mention://agent/%s) inspect this", agentID)
		commentID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{
			"content": content,
		})
		if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, issueID, map[string]any{
			"content":            content + " again",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceMentionAgent) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionAgent)
		}
	})

	t.Run("squad mention leader", func(t *testing.T) {
		squadID := createCommentTriggerPreviewSquad(t, "Edit Preview Mention Squad", agentID)
		issueID := createCommentTriggerPreviewIssue(t, "edit preview squad mention", "", "")
		content := fmt.Sprintf("[@Squad](mention://squad/%s) inspect this", squadID)
		commentID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{
			"content": content,
		})
		if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, issueID, map[string]any{
			"content":            content + " again",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceMentionSquadLeader) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionSquadLeader)
		}
	})
}

func TestPreviewCommentTriggers_EditExclusionDoesNotIgnoreOtherCommentPendingTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Edit Preview Other Pending Agent", nil)
	issueID := createCommentTriggerPreviewIssue(t, "edit preview other pending", "", "")
	content := fmt.Sprintf("[@Agent](mention://agent/%s) inspect this", agentID)
	_ = postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content": content,
	})
	if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
		t.Fatalf("queued tasks before edit preview = %d, want 1", got)
	}
	editingCommentID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content": "plain follow-up with no mention",
	})

	preview := previewCommentTriggersForTest(t, issueID, map[string]any{
		"content":            content,
		"editing_comment_id": editingCommentID,
	})
	requirePreviewAgents(t, preview)
}

func TestUpdateComment_SuppressAgentIDsFiltersEditRetrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentA := createHandlerTestAgent(t, "Edit Suppress A", nil)
	agentB := createHandlerTestAgent(t, "Edit Suppress B", nil)
	issueID := createCommentTriggerPreviewIssue(t, "edit suppress agent ids", "", "")
	commentID := postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content": "plain comment",
	})
	content := fmt.Sprintf("[@A](mention://agent/%s) [@B](mention://agent/%s) inspect this", agentA, agentB)

	updateCommentForTriggerPreviewTest(t, commentID, map[string]any{
		"content":            content,
		"suppress_agent_ids": []string{agentB},
	})

	if got := countQueuedCommentTriggerTasks(t, issueID, agentA); got != 1 {
		t.Fatalf("unsuppressed agent queued tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, agentB); got != 0 {
		t.Fatalf("suppressed agent queued tasks = %d, want 0", got)
	}
}

func TestCreateComment_SuppressUnknownAgentIDIsNoop(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Suppress Noop Agent", nil)
	issueID := createCommentTriggerPreviewIssue(t, "comment trigger suppress noop", "", "")
	content := fmt.Sprintf("[@Agent](mention://agent/%s) please inspect", agentID)

	postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content": content,
		"suppress_agent_ids": []string{
			"00000000-0000-0000-0000-000000000001",
		},
	})

	if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
		t.Fatalf("mentioned agent queued tasks = %d, want 1", got)
	}
}

func TestPreviewCommentTriggers_NoteReturnsNoAgents(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Preview Note Agent", nil)
	issueID := createCommentTriggerPreviewIssue(t, "comment trigger note", "agent", agentID)
	content := fmt.Sprintf("/note [@Agent](mention://agent/%s) human-only context", agentID)

	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if got := len(preview.Agents); got != 0 {
		t.Fatalf("note preview agents = %d, want 0: %+v", got, preview.Agents)
	}
}

func TestCreateComment_NoteMentionDoesNotQueueAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Create Note Agent", nil)
	issueID := createCommentTriggerPreviewIssue(t, "comment trigger create note", "agent", agentID)
	content := fmt.Sprintf("/note [@Agent](mention://agent/%s) human-only context", agentID)

	postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": content})

	if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 0 {
		t.Fatalf("note create queued tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_AssigneeAndSuppress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Preview Assignee", nil)
	issueID := createCommentTriggerPreviewIssue(t, "comment trigger assignee", "agent", agentID)

	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": "can you continue here?"})
	if got := len(preview.Agents); got != 1 {
		t.Fatalf("expected 1 assignee preview agent, got %d: %+v", got, preview.Agents)
	}
	if preview.Agents[0].ID != agentID {
		t.Fatalf("preview agent id = %s, want %s", preview.Agents[0].ID, agentID)
	}
	if preview.Agents[0].Source != string(commentTriggerSourceIssueAssignee) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceIssueAssignee)
	}

	postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content":            "can you continue here?",
		"suppress_agent_ids": []string{agentID},
	})
	if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 0 {
		t.Fatalf("suppressed assignee queued tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_AllSuppressesAssigneeAndPendingDedupes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Preview Dedup Assignee", nil)
	issueID := createCommentTriggerPreviewIssue(t, "comment trigger all pending", "agent", agentID)

	allPreview := previewCommentTriggersForTest(t, issueID, map[string]any{
		"content": "FYI [@all](mention://all/all)",
	})
	if got := len(allPreview.Agents); got != 0 {
		t.Fatalf("@all preview agents = %d, want 0: %+v", got, allPreview.Agents)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
		VALUES ($1, $2, $3, 'queued')
	`, agentID, handlerTestRuntimeID(t), issueID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}

	pendingPreview := previewCommentTriggersForTest(t, issueID, map[string]any{
		"content": "can you continue here?",
	})
	if got := len(pendingPreview.Agents); got != 0 {
		t.Fatalf("pending preview agents = %d, want 0: %+v", got, pendingPreview.Agents)
	}
}

func TestPreviewCommentTriggers_AssignedSquadLeaderAndSuppress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	leaderID := createHandlerTestAgent(t, "Preview Squad Leader", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Preview Trigger Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	issueID := createCommentTriggerPreviewIssue(t, "comment trigger squad assignee", "squad", squadID)

	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": "please coordinate this"})
	if got := len(preview.Agents); got != 1 {
		t.Fatalf("expected 1 squad leader preview agent, got %d: %+v", got, preview.Agents)
	}
	if preview.Agents[0].ID != leaderID {
		t.Fatalf("preview leader id = %s, want %s", preview.Agents[0].ID, leaderID)
	}
	if preview.Agents[0].Source != string(commentTriggerSourceIssueAssignee) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceIssueAssignee)
	}

	postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content":            "please coordinate this",
		"suppress_agent_ids": []string{leaderID},
	})
	if got := countQueuedCommentTriggerTasks(t, issueID, leaderID); got != 0 {
		t.Fatalf("suppressed squad leader queued tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_MentionedSquadLeaderAndSuppress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	leaderID := createHandlerTestAgent(t, "Preview Mentioned Squad Leader", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Preview Mentioned Trigger Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	issueID := createCommentTriggerPreviewIssue(t, "comment trigger mentioned squad", "", "")
	content := fmt.Sprintf("[@Squad](mention://squad/%s) please take this", squadID)

	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if got := len(preview.Agents); got != 1 {
		t.Fatalf("expected 1 mentioned squad leader preview agent, got %d: %+v", got, preview.Agents)
	}
	if preview.Agents[0].ID != leaderID {
		t.Fatalf("preview leader id = %s, want %s", preview.Agents[0].ID, leaderID)
	}
	if preview.Agents[0].Source != string(commentTriggerSourceMentionSquadLeader) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionSquadLeader)
	}

	postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content":            content,
		"suppress_agent_ids": []string{leaderID},
	})
	if got := countQueuedCommentTriggerTasks(t, issueID, leaderID); got != 0 {
		t.Fatalf("suppressed mentioned squad leader queued tasks = %d, want 0", got)
	}
}
