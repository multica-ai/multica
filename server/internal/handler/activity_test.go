package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestListTimeline_MergedAndSorted(t *testing.T) {
	ctx := context.Background()

	// Create an issue
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Timeline test issue",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Create an activity record directly in DB
	_, err := testHandler.Queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		IssueID:     parseUUID(issueID),
		ActorType:   strToText("member"),
		ActorID:     parseUUID(testUserID),
		Action:      "created",
		Details:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateActivity: %v", err)
	}

	// Create a comment
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Timeline test comment",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Fetch timeline
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/timeline", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTimeline: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var timeline []TimelineEntry
	json.NewDecoder(w.Body).Decode(&timeline)
	if len(timeline) != 2 {
		t.Fatalf("expected 2 timeline entries, got %d", len(timeline))
	}

	// First entry should be the activity (created earlier)
	if timeline[0].Type != "activity" {
		t.Fatalf("expected first entry type 'activity', got %q", timeline[0].Type)
	}
	if *timeline[0].Action != "created" {
		t.Fatalf("expected action 'created', got %q", *timeline[0].Action)
	}

	// Second entry should be the comment
	if timeline[1].Type != "comment" {
		t.Fatalf("expected second entry type 'comment', got %q", timeline[1].Type)
	}
	if *timeline[1].Content != "Timeline test comment" {
		t.Fatalf("expected comment content 'Timeline test comment', got %q", *timeline[1].Content)
	}
}

func TestListTimeline_ChronologicalOrder(t *testing.T) {
	ctx := context.Background()

	// Create an issue
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Timeline order test issue",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Create comment first
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "First comment",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)

	// Then create an activity after the comment
	_, err := testHandler.Queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		IssueID:     parseUUID(issueID),
		ActorType:   strToText("member"),
		ActorID:     parseUUID(testUserID),
		Action:      "status_changed",
		Details:     []byte(`{"from":"todo","to":"in_progress"}`),
	})
	if err != nil {
		t.Fatalf("CreateActivity: %v", err)
	}

	// Fetch timeline
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/timeline", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)

	var timeline []TimelineEntry
	json.NewDecoder(w.Body).Decode(&timeline)
	if len(timeline) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(timeline))
	}

	// Entries should be in chronological order
	if timeline[0].CreatedAt > timeline[1].CreatedAt {
		t.Fatalf("timeline not in chronological order: %s > %s", timeline[0].CreatedAt, timeline[1].CreatedAt)
	}
}

func TestCreateComment_WithParentID(t *testing.T) {
	ctx := context.Background()

	// Create an issue
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Reply test issue",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Create parent comment
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Parent comment",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment (parent): expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parentComment CommentResponse
	json.NewDecoder(w.Body).Decode(&parentComment)

	// Create reply with parent_id
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "Reply to parent",
		"parent_id": parentComment.ID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment (reply): expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var replyComment CommentResponse
	json.NewDecoder(w.Body).Decode(&replyComment)

	if replyComment.ParentID == nil {
		t.Fatal("expected reply to have parent_id set")
	}
	if *replyComment.ParentID != parentComment.ID {
		t.Fatalf("expected parent_id %q, got %q", parentComment.ID, *replyComment.ParentID)
	}

	// Verify parent comment has no parent_id
	if parentComment.ParentID != nil {
		t.Fatalf("expected parent comment to have nil parent_id, got %q", *parentComment.ParentID)
	}
}

func TestCreateComment_AgentWithWrongParentRejected(t *testing.T) {
	ctx := context.Background()

	// Find the fixture agent + its runtime.
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	// Two issues: A hosts the comment-triggered task; B exists to prove the
	// guard is scoped to the task's own issue and does not block cross-issue
	// agent activity. (The CLI stamps X-Task-ID on every request, so an agent
	// legitimately commenting on another issue must still succeed.)
	createIssue := func(title string) string {
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": title})
		testHandler.CreateIssue(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue(%s): %d: %s", title, w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		return issue.ID
	}
	issueA := createIssue("agent parent guard test — issue A")
	issueB := createIssue("agent parent guard test — issue B")

	var freshTaskID string
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, freshTaskID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id IN ($1, $2)`, issueA, issueB)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id IN ($1, $2)`, issueA, issueB)
	})

	postComment := func(t *testing.T, issueID string, body map[string]any, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		testHandler.CreateComment(w, r)
		return w
	}

	w := postComment(t, issueA, map[string]any{"content": "stale comment"}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create stale parent: %d: %s", w.Code, w.Body.String())
	}
	var staleParent CommentResponse
	json.NewDecoder(w.Body).Decode(&staleParent)

	w = postComment(t, issueA, map[string]any{"content": "fresh comment"}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create fresh parent: %d: %s", w.Code, w.Body.String())
	}
	var freshParent CommentResponse
	json.NewDecoder(w.Body).Decode(&freshParent)

	// Comment-triggered task bound to issueA.
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, trigger_comment_id)
		 VALUES ($1, $2, $3, 'queued', 0, $4) RETURNING id`,
		agentID, runtimeID, issueA, freshParent.ID,
	).Scan(&freshTaskID); err != nil {
		t.Fatalf("insert fresh task: %v", err)
	}

	agentHeaders := map[string]string{"X-Agent-ID": agentID, "X-Task-ID": freshTaskID}

	// Same issue + wrong parent → 409.
	w = postComment(t, issueA,
		map[string]any{"content": "drifted reply", "parent_id": staleParent.ID},
		agentHeaders,
	)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 when agent replies with wrong parent, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), freshParent.ID) {
		t.Fatalf("expected error body to reference the correct trigger comment id, got %s", w.Body.String())
	}

	// Same issue + no parent → 409 (must reply to trigger).
	w = postComment(t, issueA,
		map[string]any{"content": "no parent"},
		agentHeaders,
	)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 when agent replies with no parent, got %d", w.Code)
	}

	// Same issue + correct parent → 201.
	w = postComment(t, issueA,
		map[string]any{"content": "correct reply", "parent_id": freshParent.ID},
		agentHeaders,
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 when agent replies with matching parent, got %d: %s", w.Code, w.Body.String())
	}

	// Cross-issue: agent carries X-Task-ID (bound to issueA) but comments on
	// issueB. The guard must NOT fire — this is the cross-issue regression
	// covering the fix for gpt-boy's review.
	w = postComment(t, issueB,
		map[string]any{"content": "cross-issue note"},
		agentHeaders,
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent posting on a different issue should not be blocked by its current task's trigger, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCommentWithParentID_AppearsInTimeline(t *testing.T) {
	ctx := context.Background()

	// Create an issue
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Timeline reply test",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Create parent comment
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Parent in timeline",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	var parent CommentResponse
	json.NewDecoder(w.Body).Decode(&parent)

	// Create reply
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "Reply in timeline",
		"parent_id": parent.ID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)

	// Fetch timeline
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/timeline", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTimeline: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var timeline []TimelineEntry
	json.NewDecoder(w.Body).Decode(&timeline)
	if len(timeline) != 2 {
		t.Fatalf("expected 2 timeline entries, got %d", len(timeline))
	}

	// Find the reply entry
	var found bool
	for _, entry := range timeline {
		if entry.Type == "comment" && entry.ParentID != nil && *entry.ParentID == parent.ID {
			found = true
			if *entry.Content != "Reply in timeline" {
				t.Fatalf("expected reply content 'Reply in timeline', got %q", *entry.Content)
			}
		}
	}
	if !found {
		t.Fatal("expected to find reply with parent_id in timeline")
	}
}

// TestCreateComment_FixerReplyBypassesTriggerGuard verifies that fixer_reply
// rows are NOT subject to the resumed-session-drift parent_id guard. They have
// their own (stricter) invariant validated separately in CreateComment (parent
// must be type=cr_review_comment with a non-null review_thread_id). Rosa runs
// from inside a comment-triggered task and MUST be able to thread fixer_reply
// under the CR review comment, not under the task's trigger.
//
// Repro is the live failure on Story 1.6 (issue 55250d67): member posted
// "Proceed", which spawned a comment-triggered retry for Rosa. Every
// fixer_reply Rosa tried to post returned 409 from the resume-drift guard,
// because the trigger comment is not a cr_review_comment.
func TestCreateComment_FixerReplyBypassesTriggerGuard(t *testing.T) {
	ctx := context.Background()

	// Reuse the fixture agent + runtime.
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	// Create the issue Rosa will post on.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "fixer_reply guard exemption test",
	})
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	var triggerCommentID, crCommentID, reviewThreadID, taskID string
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue_review_thread WHERE id = $1`, reviewThreadID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	postComment := func(t *testing.T, body map[string]any, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		testHandler.CreateComment(w, r)
		return w
	}

	// Seed: a "Proceed" comment from a member — this will be the task trigger.
	w = postComment(t, map[string]any{"content": "Proceed"}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create trigger comment: %d: %s", w.Code, w.Body.String())
	}
	var triggerComment CommentResponse
	json.NewDecoder(w.Body).Decode(&triggerComment)
	triggerCommentID = triggerComment.ID

	// Seed: an issue_review_thread row (FK target for comment.review_thread_id).
	// review_thread_id is uuid REFERENCES issue_review_thread(id), so we have
	// to land a real thread row before we can mint the cr_review_comment.
	// gh_comment_id is UNIQUE — use the unix-nano clock so concurrent test
	// runs don't collide.
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue_review_thread
		   (workspace_id, issue_id, pr_repo, pr_number, gh_comment_id,
		    file_path, severity, title, body, url, author_login, state)
		 VALUES ($1, $2, 'test/repo', 1, $3,
		     'README.md', 'issue', 'fixer-reply test', 'body',
		     'https://example.invalid', 'coderabbit', 'unresolved')
		 RETURNING id`,
		testWorkspaceID, issueID, time.Now().UnixNano(),
	).Scan(&reviewThreadID); err != nil {
		t.Fatalf("insert issue_review_thread: %v", err)
	}

	// Seed: a CR review comment Rosa will reply to. The CreateComment handler
	// refuses cr_review_comment writes (only the GitHub webhook may insert
	// those rows), so we insert directly via SQL — mirroring how the live
	// webhook lands them.
	if err := testPool.QueryRow(ctx,
		`INSERT INTO comment
		   (issue_id, workspace_id, author_type, content, type, review_thread_id)
		 VALUES ($1, $2, 'system', '**Major** finding', 'cr_review_comment', $3)
		 RETURNING id`,
		issueID, testWorkspaceID, reviewThreadID,
	).Scan(&crCommentID); err != nil {
		t.Fatalf("insert cr_review_comment: %v", err)
	}

	// Seed: a comment-triggered task pointing at the "Proceed" comment.
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue
		   (agent_id, runtime_id, issue_id, status, priority, trigger_comment_id)
		 VALUES ($1, $2, $3, 'running', 0, $4)
		 RETURNING id`,
		agentID, runtimeID, issueID, triggerCommentID,
	).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	agentHeaders := map[string]string{"X-Agent-ID": agentID, "X-Task-ID": taskID}

	// Case 1: fixer_reply under the cr_review_comment (NOT the trigger) must
	// succeed — this is the case the patch enables.
	w = postComment(t,
		map[string]any{
			"content":   "applied patch — verbatim text for GitHub",
			"type":      "fixer_reply",
			"parent_id": crCommentID,
		},
		agentHeaders,
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for fixer_reply under cr_review_comment, got %d: %s",
			w.Code, w.Body.String())
	}

	// Case 2 (regression guard): a PLAIN type=comment from the same task with
	// the wrong parent must STILL return 409. The patch only exempts
	// fixer_reply; the resume-drift guard for plain comments is unchanged.
	w = postComment(t,
		map[string]any{"content": "drifted plain comment", "parent_id": crCommentID},
		agentHeaders,
	)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for plain comment with wrong parent, got %d: %s",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), triggerCommentID) {
		t.Fatalf("expected 409 body to reference trigger comment id %s, got %s",
			triggerCommentID, w.Body.String())
	}
}
