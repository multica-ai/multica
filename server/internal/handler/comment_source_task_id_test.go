package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateComment_AgentAuthored_StampsSourceTaskIDFromXTaskID pins the
// invariant that MUL-4019 depends on: every agent-authored comment written
// through the HTTP CreateComment path carries source_task_id pointing at the
// task that produced it. Without this stamp,
// resolveOriginatorFromTriggerComment cannot walk agent → parent-task →
// originator, so the human originator is lost at the first agent hop and
// canInvokeAgent silently denies downstream private / member-scoped @agent
// triggers.
func TestCreateComment_AgentAuthored_StampsSourceTaskIDFromXTaskID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "source-task-id-stamp-agent", nil)

	// Create an issue the agent is running on.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'source-task-id stamp test')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Simulate the agent's active task; the CLI stamps this task ID on every
	// outbound HTTP request as X-Task-ID.
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "worker result posted from inside the agent's task",
	})
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", taskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SourceTaskID == nil {
		t.Fatalf("expected source_task_id on the response, got nil")
	}
	if *resp.SourceTaskID != taskID {
		t.Fatalf("source_task_id mismatch: got %q, want %q", *resp.SourceTaskID, taskID)
	}

	// Verify the underlying row too — response omission can hide a NULL
	// column if the JSON marshalling changes independently of the handler.
	var stored *string
	if err := testPool.QueryRow(ctx,
		`SELECT source_task_id::text FROM comment WHERE id = $1`, resp.ID,
	).Scan(&stored); err != nil {
		t.Fatalf("select stored comment: %v", err)
	}
	if stored == nil {
		t.Fatalf("comment.source_task_id is NULL in the database; expected %s", taskID)
	}
	if *stored != taskID {
		t.Fatalf("comment.source_task_id in db = %q, want %q", *stored, taskID)
	}
}

// TestCreateComment_MemberAuthored_LeavesSourceTaskIDNull pins the other half
// of the contract: a member-authored comment has no source task by
// definition, so source_task_id must stay NULL even if a stray X-Task-ID
// header were forwarded. Member comments are the top of the originator
// chain, not a fanout hop.
func TestCreateComment_MemberAuthored_LeavesSourceTaskIDNull(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'source-task-id member test')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "human comment",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SourceTaskID != nil {
		t.Fatalf("member comment should have no source_task_id, got %v", *resp.SourceTaskID)
	}

	var stored *string
	if err := testPool.QueryRow(ctx,
		`SELECT source_task_id::text FROM comment WHERE id = $1`, resp.ID,
	).Scan(&stored); err != nil {
		t.Fatalf("select stored comment: %v", err)
	}
	if stored != nil {
		t.Fatalf("member comment.source_task_id in db = %q, want NULL", *stored)
	}
}

// TestCreateComment_MUL4019_SquadLeaderMentionOfPrivateAgentChain reproduces
// the full MUL-4019 failure chain end-to-end at the HTTP handler level and
// asserts the fixed behavior:
//
//	human U (private agent B's owner) → issue assigned to squad S →
//	worker agent A runs a task on that issue and posts a result comment →
//	squad leader L is auto-triggered by A's comment (originator inherited
//	from A's task = U via comment.source_task_id) →
//	L runs and posts a comment @-mentioning private agent B →
//	B is enqueued because canInvokeAgent judges A2A by the ORIGINATOR (U),
//	who IS B's owner.
//
// Before the fix the HTTP CreateComment path did not stamp source_task_id on
// agent-authored comments, which nulled the originator at the A→L hop and
// silently blocked L's private-B trigger. This test would fail on that
// pre-fix code path with "expected private B to be enqueued, got 0".
func TestCreateComment_MUL4019_SquadLeaderMentionOfPrivateAgentChain(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Private agent B, owned by ownerID.
	privateAgentB, ownerID, _ := privateAgentTestFixture(t)

	// Workspace-invocable worker agent A and leader L (both public_to workspace).
	workerA := createHandlerTestAgent(t, "mul4019-worker-A", nil)
	leaderL := createHandlerTestAgent(t, "mul4019-leader-L", nil)

	// Squad S with L as its leader.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'MUL-4019 squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderL, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Issue assigned to squad S. Creator = ownerID so any assignment gate
	// passes; ownerID is a workspace member seeded by privateAgentTestFixture.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, 'MUL-4019 chain', 'squad', $3)
		RETURNING id
	`, testWorkspaceID, ownerID, squadID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// A's task is running on this issue with the human originator stamped.
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, originator_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $3)
		RETURNING id
	`, workerA, issueID, ownerID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}
	// t.Cleanup delete-by-issue above sweeps this too.

	countQueuedFor := func(agentID string) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count queued tasks for %s: %v", agentID, err)
		}
		return n
	}

	// Step 1: A finishes and posts a result comment. No @-mention — the squad
	// leader routes via routeAssignedSquadLeaderFallback.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "worker A done, please coordinate the next step",
	})
	r.Header.Set("X-Agent-ID", workerA)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("A CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var workerComment CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&workerComment); err != nil {
		t.Fatalf("decode worker comment: %v", err)
	}
	if workerComment.SourceTaskID == nil || *workerComment.SourceTaskID != workerTaskID {
		got := "<nil>"
		if workerComment.SourceTaskID != nil {
			got = *workerComment.SourceTaskID
		}
		t.Fatalf("A's comment should carry source_task_id=%s, got %s (this is the MUL-4019 bug)",
			workerTaskID, got)
	}

	// The squad leader L must be queued with the human U's originator
	// inherited from A's task via the comment.source_task_id trail.
	if got := countQueuedFor(leaderL); got != 1 {
		t.Fatalf("expected exactly 1 queued task for squad leader L after A's comment, got %d", got)
	}
	var leaderTaskID, leaderOriginator string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text, coalesce(originator_user_id::text, '') FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		 ORDER BY created_at DESC LIMIT 1`,
		issueID, leaderL,
	).Scan(&leaderTaskID, &leaderOriginator); err != nil {
		t.Fatalf("load leader task: %v", err)
	}
	if leaderOriginator != ownerID {
		t.Fatalf("squad leader task originator_user_id = %q, want %q (originator must inherit from A's task via source_task_id)",
			leaderOriginator, ownerID)
	}

	// Step 2: promote L's task to running so it can act as the task actor
	// on its subsequent HTTP write.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`,
		leaderTaskID,
	); err != nil {
		t.Fatalf("promote leader task: %v", err)
	}

	// Step 3: L posts a comment @-mentioning private agent B.
	// Note: keep parent_id matching L's trigger comment (A's comment ID) to
	// pass the resumed-session parent-drift guard.
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "delegating deep work to [@B](mention://agent/" + privateAgentB + ")",
		"parent_id": workerComment.ID,
	})
	r.Header.Set("X-Agent-ID", leaderL)
	r.Header.Set("X-Task-ID", leaderTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("L CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var leaderComment CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&leaderComment); err != nil {
		t.Fatalf("decode leader comment: %v", err)
	}
	if leaderComment.SourceTaskID == nil || *leaderComment.SourceTaskID != leaderTaskID {
		got := "<nil>"
		if leaderComment.SourceTaskID != nil {
			got = *leaderComment.SourceTaskID
		}
		t.Fatalf("L's comment should carry source_task_id=%s, got %s",
			leaderTaskID, got)
	}

	// Final assertion: private agent B must be queued. Before the fix
	// canInvokeAgent saw an empty originator (L's task had NULL
	// originator_user_id because A's comment lacked source_task_id) and
	// silently dropped the trigger.
	if got := countQueuedFor(privateAgentB); got != 1 {
		t.Fatalf("expected private agent B to be enqueued by squad leader @mention, got %d queued tasks (this is the MUL-4019 symptom)",
			got)
	}
	var privateBOriginator string
	if err := testPool.QueryRow(ctx,
		`SELECT coalesce(originator_user_id::text, '') FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		 ORDER BY created_at DESC LIMIT 1`,
		issueID, privateAgentB,
	).Scan(&privateBOriginator); err != nil {
		t.Fatalf("load private B task: %v", err)
	}
	if privateBOriginator != ownerID {
		t.Fatalf("private B task originator_user_id = %q, want %q (must inherit from L's task via source_task_id)",
			privateBOriginator, ownerID)
	}
}
