package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// authRequestWithAgent makes an authenticated request with X-Agent-ID header,
// causing the server to resolve the actor as an agent instead of a member.
func authRequestWithAgent(t *testing.T, method, path string, body any, agentID string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)

	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return r
}

// countPendingTasks returns the number of queued/dispatched tasks for an issue.
func countPendingTasks(t *testing.T, issueID string) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued', 'dispatched')`,
		issueID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count pending tasks: %v", err)
	}
	return count
}

// clearTasks deletes all tasks for an issue (cleanup between subtests).
func clearTasks(t *testing.T, issueID string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	if err != nil {
		t.Fatalf("failed to clear tasks: %v", err)
	}
}

// latestTriggerCommentID returns the trigger_comment_id of the most recently
// created queued/dispatched task for the given issue, or empty string if none.
func latestTriggerCommentID(t *testing.T, issueID string) string {
	t.Helper()
	var triggerID *string
	err := testPool.QueryRow(context.Background(),
		`SELECT trigger_comment_id::text
		   FROM agent_task_queue
		  WHERE issue_id = $1 AND status IN ('queued', 'dispatched')
		  ORDER BY created_at DESC
		  LIMIT 1`,
		issueID).Scan(&triggerID)
	if err != nil {
		t.Fatalf("failed to fetch trigger_comment_id: %v", err)
	}
	if triggerID == nil {
		return ""
	}
	return *triggerID
}

// getAgentID returns the ID of the first agent in the test workspace.
func getAgentID(t *testing.T) string {
	t.Helper()
	resp := authRequest(t, "GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	var agents []map[string]any
	readJSON(t, resp, &agents)
	if len(agents) == 0 {
		t.Fatal("no agents in test workspace")
	}
	return agents[0]["id"].(string)
}

// createSecondAgent creates a second agent in the test workspace and returns its ID.
// It reuses the same runtime as the first agent.
func createSecondAgent(t *testing.T) string {
	t.Helper()
	// Fetch the first agent to get its runtime_id.
	resp := authRequest(t, "GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	var agents []map[string]any
	readJSON(t, resp, &agents)
	if len(agents) == 0 {
		t.Fatal("no agents in test workspace")
	}
	runtimeID := agents[0]["runtime_id"].(string)

	resp = authRequest(t, "POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Second Test Agent",
		"runtime_id": runtimeID,
		"visibility": "workspace",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateAgent: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var agent map[string]any
	readJSON(t, resp, &agent)
	id := agent["id"].(string)
	t.Cleanup(func() {
		authRequest(t, "POST", "/api/agents/"+id+"/archive?workspace_id="+testWorkspaceID, nil)
	})
	return id
}

// createIssueAssignedToAgent creates a todo issue assigned to the given agent.
func createIssueAssignedToAgent(t *testing.T, title, agentID string) string {
	t.Helper()
	resp := authRequest(t, "PUT", fmt.Sprintf("/api/issues/%s", createIssue(t, title)), map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	var issue map[string]any
	readJSON(t, resp, &issue)
	return issue["id"].(string)
}

// createIssue creates a basic todo issue and returns its ID.
func createIssue(t *testing.T, title string) string {
	t.Helper()
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateIssue: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var issue map[string]any
	readJSON(t, resp, &issue)
	return issue["id"].(string)
}

// postComment posts a comment as the test member.
func postComment(t *testing.T, issueID, content string, parentID *string) string {
	t.Helper()
	body := map[string]any{
		"content": content,
		"type":    "comment",
	}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	resp := authRequest(t, "POST", "/api/issues/"+issueID+"/comments", body)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("postComment: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var comment map[string]any
	readJSON(t, resp, &comment)
	return comment["id"].(string)
}

// postCommentAsAgent posts a comment with the X-Agent-ID header.
func postCommentAsAgent(t *testing.T, issueID, content, agentID string, parentID *string) string {
	t.Helper()
	body := map[string]any{
		"content": content,
		"type":    "comment",
	}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	resp := authRequestWithAgent(t, "POST", "/api/issues/"+issueID+"/comments", body, agentID)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("postCommentAsAgent: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var comment map[string]any
	readJSON(t, resp, &comment)
	return comment["id"].(string)
}

// strPtr returns a pointer to a string.
func strPtr(s string) *string { return &s }

// TestCommentTriggerOnComment tests on_comment trigger scenarios end-to-end.
// Verifies that the agent task queue is populated correctly based on:
// - top-level vs threaded comments
// - member vs agent thread starters
// - presence/absence of @mentions
func TestCommentTriggerOnComment(t *testing.T) {
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Comment trigger integration test", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	t.Run("top-level comment without mentions triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)
		postComment(t, issueID, "Please fix this bug", nil)
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	t.Run("top-level comment mentioning only others suppresses trigger", func(t *testing.T) {
		clearTasks(t, issueID)
		// Mention a fake agent UUID that is not the assignee.
		content := "[@SomeoneElse](mention://agent/00000000-0000-0000-0000-000000000001) what do you think?"
		postComment(t, issueID, content, nil)
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks, got %d", n)
		}
	})

	t.Run("top-level comment mentioning assignee triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)
		content := fmt.Sprintf("[@Agent](mention://agent/%s) fix this", agentID)
		postComment(t, issueID, content, nil)
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	t.Run("reply to agent thread without mentions triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)
		// Agent starts a thread.
		threadID := postCommentAsAgent(t, issueID, "I analyzed the issue.", agentID, nil)
		// Member replies in the agent's thread.
		postComment(t, issueID, "Looks good, please proceed", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	// Regression guard for #1301: the assignee on_comment path must record
	// the NEW reply as trigger_comment_id, not the thread root. Otherwise
	// the daemon feeds stale content to the agent prompt, which with
	// `--resume` sessions surfaces as "already replied, no further action".
	// Reply placement (flat-thread grouping) is handled downstream in
	// TaskService.createAgentComment, not here.
	t.Run("reply records new comment id (not thread root) as trigger_comment_id", func(t *testing.T) {
		clearTasks(t, issueID)
		threadID := postCommentAsAgent(t, issueID, "First pass analysis.", agentID, nil)
		replyID := postComment(t, issueID, "Please also check the edge case", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Fatalf("expected 1 pending task, got %d", n)
		}
		if got := latestTriggerCommentID(t, issueID); got != replyID {
			t.Errorf("trigger_comment_id = %q, want reply id %q (thread root was %q)",
				got, replyID, threadID)
		}
	})

	t.Run("reply to member thread without mentions suppresses trigger", func(t *testing.T) {
		clearTasks(t, issueID)
		// Member starts a thread.
		threadID := postComment(t, issueID, "Hey team, what do you think?", nil)
		// Clear the task that was created by the top-level comment.
		clearTasks(t, issueID)
		// Another member reply (same user in this test, but the key is parent is by member).
		postComment(t, issueID, "I agree with you", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks (member-to-member reply), got %d", n)
		}
	})

	t.Run("reply to member thread after agent replied triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)
		// Member starts a thread (top-level comment).
		threadID := postComment(t, issueID, "Please fix this bug", nil)
		clearTasks(t, issueID)
		// Agent replies in the thread.
		postCommentAsAgent(t, issueID, "Working on it, found the root cause.", agentID, strPtr(threadID))
		// Member follows up in the same thread without @mentioning the agent.
		postComment(t, issueID, "Great, please also check the edge case", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (agent participated in thread), got %d", n)
		}
	})

	t.Run("reply to member thread mentioning assignee triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)
		// Member starts a thread.
		threadID := postComment(t, issueID, "Question about this", nil)
		clearTasks(t, issueID)
		// Reply mentioning the assignee agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you help with this?", agentID)
		postComment(t, issueID, content, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 0 {
			// The mention of the assignee agent unblocks on_comment but
			// the assignee-mention path in on_mention skips the assignee.
			// Either 0 or 1 is acceptable depending on the on_comment logic.
			// With our implementation: isReplyToMemberThread returns false
			// (assignee mentioned), and commentMentionsOthersButNotAssignee
			// returns false (assignee is mentioned). So on_comment triggers.
			// Let's re-check.
		}
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (assignee mentioned in member thread), got %d", n)
		}
	})

	t.Run("reply to member thread that @mentioned assignee triggers without re-mention", func(t *testing.T) {
		clearTasks(t, issueID)
		// Member starts a thread that @mentions the assignee agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you review this?", agentID)
		threadID := postComment(t, issueID, content, nil)
		// Clear the task created by the top-level mention.
		clearTasks(t, issueID)
		// Reply in the thread WITHOUT re-mentioning the assignee.
		postComment(t, issueID, "Here is more context for you", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (assignee mentioned in thread root), got %d", n)
		}
	})
}

// TestCommentTriggerAtAllSuppression verifies that @all mentions do not
// trigger agent execution — @all is a broadcast, not a direct request.
func TestCommentTriggerAtAllSuppression(t *testing.T) {
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "@all suppression test", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	t.Run("top-level @all comment suppresses on_comment", func(t *testing.T) {
		clearTasks(t, issueID)
		postComment(t, issueID, "[@All](mention://all/all) heads up everyone", nil)
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks (@all should not trigger agent), got %d", n)
		}
	})

	t.Run("@all in agent thread suppresses on_comment", func(t *testing.T) {
		clearTasks(t, issueID)
		threadID := postCommentAsAgent(t, issueID, "Here is my analysis.", agentID, nil)
		postComment(t, issueID, "[@All](mention://all/all) FYI for the team", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks (@all in agent thread), got %d", n)
		}
	})
}

// TestCommentTriggerOnAssignNoStatusGate verifies that assigning an agent to
// a non-todo issue still triggers the agent (status gate was removed).
func TestCommentTriggerOnAssignNoStatusGate(t *testing.T) {
	agentID := getAgentID(t)

	// Create an in_progress issue.
	issueID := createIssue(t, "On-assign status gate test")
	resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "in_progress",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	// Assign the agent — should trigger despite non-todo status.
	resp = authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("assign agent: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task after assigning to in_progress issue, got %d", n)
	}
}

// TestCommentTriggerOnMentionNoStatusGate verifies that @mentioning an agent
// on a done issue still triggers the agent (no status gate on on_mention).
func TestCommentTriggerOnMentionNoStatusGate(t *testing.T) {
	agentID := getAgentID(t)

	// Create a done issue (not assigned to agent).
	issueID := createIssue(t, "On-mention done issue test")
	resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	// @mention the agent on a done issue — should still trigger.
	content := fmt.Sprintf("[@Agent](mention://agent/%s) found a problem here", agentID)
	postComment(t, issueID, content, nil)

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task after @mention on done issue, got %d", n)
	}
}

// TestCommentTriggerThreadInheritedMention verifies that when a top-level
// comment @mentions an agent (not the assignee), replies in that thread
// also trigger the mentioned agent — even without explicitly re-mentioning it.
func TestCommentTriggerThreadInheritedMention(t *testing.T) {
	agentID := getAgentID(t)

	// Create an issue NOT assigned to the agent, so on_comment won't fire.
	issueID := createIssue(t, "Thread-inherited mention test")
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	t.Run("reply in thread inherits parent mention", func(t *testing.T) {
		clearTasks(t, issueID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you review this?", agentID)
		threadID := postComment(t, issueID, content, nil)
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Fatalf("expected 1 pending task after initial mention, got %d", n)
		}
		// Clear the task so we can test the reply independently.
		clearTasks(t, issueID)
		// Reply in the thread WITHOUT mentioning the agent.
		postComment(t, issueID, "Here is more context for you", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task from thread-inherited mention, got %d", n)
		}
	})

	t.Run("reply does not double-trigger when re-mentioning same agent", func(t *testing.T) {
		clearTasks(t, issueID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) help", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)
		// Reply also @mentions the same agent — should still be just 1 task.
		reply := fmt.Sprintf("[@Agent](mention://agent/%s) any update?", agentID)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (no duplicate), got %d", n)
		}
	})

	t.Run("reply mentioning only a member does not inherit agent mention", func(t *testing.T) {
		clearTasks(t, issueID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you help?", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)
		// Reply mentions only a member — should NOT inherit parent's agent mention.
		reply := fmt.Sprintf("cc [@Someone](mention://member/%s)", testUserID)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks (member-only reply should not inherit agent mention), got %d", n)
		}
	})

	t.Run("reply mentioning a different agent does not inherit parent agent", func(t *testing.T) {
		clearTasks(t, issueID)
		agentB := createSecondAgent(t)
		// Top-level comment @mentions agent A.
		content := fmt.Sprintf("[@AgentA](mention://agent/%s) please review", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)
		// Reply @mentions agent B — should trigger ONLY agent B, not agent A.
		reply := fmt.Sprintf("[@AgentB](mention://agent/%s) can you also look?", agentB)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (only agent B), got %d", n)
		}
	})

	t.Run("reply mentioning same agent and member triggers via explicit mention", func(t *testing.T) {
		clearTasks(t, issueID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) review this", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)
		// Reply re-mentions the same agent along with a member — triggers via the reply's own mention.
		reply := fmt.Sprintf("[@Agent](mention://agent/%s) and cc [@Someone](mention://member/%s)", agentID, testUserID)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (reply mentions agent explicitly), got %d", n)
		}
	})
}

// TestCommentTriggerCoalescing verifies that rapid-fire comments don't create
// duplicate tasks (coalescing dedup).
func TestCommentTriggerCoalescing(t *testing.T) {
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Coalescing test", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	// Post two comments rapidly — only 1 task should be created (coalescing).
	postComment(t, issueID, "First comment", nil)
	postComment(t, issueID, "Second comment", nil)

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task (coalescing), got %d", n)
	}
}

// TestCommentTriggerMentionAssigneeDoneIssue verifies that @mentioning the
// assigned agent on a done issue still triggers execution. Previously the
// assignee was unconditionally skipped in the mention path (assuming
// on_comment handled it), but on_comment is suppressed for terminal statuses.
func TestCommentTriggerMentionAssigneeDoneIssue(t *testing.T) {
	agentID := getAgentID(t)

	// Create an issue assigned to the agent, then mark it done.
	issueID := createIssueAssignedToAgent(t, "Mention-assignee-done test", agentID)
	clearTasks(t, issueID) // clear any tasks from assignment
	resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	// @mention the assigned agent on the done issue — should trigger.
	content := fmt.Sprintf("[@Agent](mention://agent/%s) reopen this please", agentID)
	postComment(t, issueID, content, nil)

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task after @mention of assignee on done issue, got %d", n)
	}
}
