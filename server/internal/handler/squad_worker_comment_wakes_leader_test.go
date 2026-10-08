package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestGuestSquadWorkerReplayRequiresPlannedInput_GH8301(t *testing.T) {
	trigger := commentAgentTrigger{Source: commentTriggerSourceThreadParent, NonLeaderAgentReply: true}
	if got := keepReplayableAgentTriggers([]commentAgentTrigger{trigger}, false); len(got) != 0 {
		t.Fatal("timestamp-only guest worker reply must not replay")
	}
	if got := keepReplayableAgentTriggers([]commentAgentTrigger{trigger}, true); len(got) != 1 {
		t.Fatal("planned guest worker reply must replay")
	}
}

func TestGuestSquadWorkerRouteOnMemberAssignedIssue_GH8301(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1`, issueID, testUserID)
	fx.Issue.AssigneeType = pgtype.Text{String: "member", Valid: true}
	fx.Issue.AssigneeID = parseUUID(testUserID)

	leaderTaskID := dbfx.Task(t, fx.LeaderID, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": fx.SquadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	delegationID := dbfx.Comment(t, issueID, "delegate to guest worker", testutil.Cols{
		"author_type": "agent", "author_id": fx.LeaderID, "source_task_id": leaderTaskID,
	})
	workerTaskID := dbfx.Task(t, fx.OtherID, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": delegationID, "squad_id": fx.SquadID,
		"delegated_from_task_id": leaderTaskID,
		"originator_user_id":     testUserID, "accountable_user_id": testUserID,
	})
	parent, err := testHandler.Queries.GetComment(ctx, parseUUID(delegationID))
	if err != nil {
		t.Fatal(err)
	}
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, fx.Issue, "done", &parent, "agent", fx.OtherID, commentTriggerComputeOptions{
		AuthoringTaskID: parseUUID(workerTaskID), OriginatorUserID: testUserID,
	})
	if len(triggers) != 1 || triggers[0].Source != commentTriggerSourceThreadParent || triggers[0].Squad == nil {
		t.Fatalf("guest worker route = %+v, want one squad leader thread-parent trigger", triggers)
	}
	if got := keepReplayableAgentTriggers(triggers, true); len(got) != 1 {
		t.Fatal("planned guest worker reply must replay")
	}
}

func TestCreateComment_GuestSquadWorkerRouting_GH8301(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		mode                    string
		name                    string
		wantAssigned, wantGuest int
	}{
		{mode: "live", name: "live guest delegation", wantGuest: 1},
		{mode: "leader_changed", name: "changed guest leader fails closed"},
		{mode: "permission_denied", name: "guest permission denied fails closed"},
		{mode: "deleted", name: "deleted guest delegation uses assigned squad", wantAssigned: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode := tc.mode
			var outsiderID string
			if mode == "permission_denied" {
				outsiderID = dbfx.User(t, "GH-8301 outsider", "gh-8301-outsider-"+uuid.NewString()+"@multica.test")
			}
			assignedLeaderID := dbfx.Agent(t, "GH-8301 assigned leader "+mode, testRuntimeID)
			guestLeaderID := dbfx.Agent(t, "GH-8301 exact guest leader "+mode, testRuntimeID)
			replacementID := dbfx.Agent(t, "GH-8301 replacement leader "+mode, testRuntimeID)
			workerID := dbfx.Agent(t, "GH-8301 exact guest worker "+mode, testRuntimeID)
			assignedSquadID := dbfx.Squad(t, "GH-8301 assigned squad "+mode, assignedLeaderID)
			guestSquadID := dbfx.Squad(t, "GH-8301 exact guest squad "+mode, guestLeaderID)
			issueID := dbfx.Issue(t, "guest route beats assigned fallback "+mode, testutil.Cols{
				"assignee_type": "squad",
				"assignee_id":   assignedSquadID,
			})
			guestLeaderTaskID := dbfx.Task(t, guestLeaderID, testutil.Cols{
				"runtime_id":          testRuntimeID,
				"issue_id":            issueID,
				"status":              "completed",
				"is_leader_task":      true,
				"squad_id":            guestSquadID,
				"originator_user_id":  testUserID,
				"accountable_user_id": testUserID,
			})
			delegationID := dbfx.Comment(t, issueID, "delegate to guest worker", testutil.Cols{
				"author_type":    "agent",
				"author_id":      guestLeaderID,
				"source_task_id": guestLeaderTaskID,
			})
			workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
				"runtime_id":             testRuntimeID,
				"issue_id":               issueID,
				"status":                 "running",
				"trigger_comment_id":     delegationID,
				"squad_id":               guestSquadID,
				"delegated_from_task_id": guestLeaderTaskID,
				"originator_user_id":     testUserID,
				"accountable_user_id":    testUserID,
			})
			dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)

			switch mode {
			case "leader_changed":
				dbfx.Exec(t, `UPDATE squad SET leader_id = $2 WHERE id = $1`, guestSquadID, replacementID)
			case "permission_denied":
				dbfx.Exec(t, `UPDATE agent SET owner_id = $2 WHERE id = $1`, guestLeaderID, outsiderID)
			case "deleted":
				dbfx.Comment(t, issueID, "keep delegation reply tree", testutil.Cols{"parent_id": delegationID})
				deleted, err := testHandler.deleteComment(context.Background(), parseUUID(delegationID), parseUUID(testWorkspaceID))
				if err != nil {
					t.Fatal(err)
				}
				if deleted.Tombstone == nil {
					t.Fatal("delegation with replies was removed instead of tombstoned")
				}
			}

			r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
				"content":   "guest work complete",
				"parent_id": delegationID,
			})
			r.Header.Set("X-Agent-ID", workerID)
			r.Header.Set("X-Task-ID", workerTaskID)
			r = withURLParam(r, "id", issueID)
			testutil.Call(t, testHandler.CreateComment, r).Want(http.StatusCreated)

			if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE AND squad_id = $3`, issueID, assignedLeaderID, assignedSquadID); got != tc.wantAssigned {
				t.Fatalf("assigned squad leader received %d task(s), want %d", got, tc.wantAssigned)
			}
			if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE AND squad_id = $3`, issueID, guestLeaderID, guestSquadID); got != tc.wantGuest {
				t.Fatalf("exact guest squad leader received %d task(s), want %d", got, tc.wantGuest)
			}
		})
	}
}

// TestCreateComment_WorkerAgentCommentWakesSquadLeader_MUL4015 pins the
// full CreateComment behavior for the scenario reported in MUL-4015:
//
//   - Issue is assigned to a squad (leader L).
//   - L delegates work by @-mentioning a distinct worker agent W. That
//     triggers a task for W (leader→worker handoff).
//   - W completes its work and posts a plain "done" comment via CreateComment
//     using the CLI's X-Agent-ID + X-Task-ID pair.
//
// Expected: a new leader-role task is queued for L so the leader can
// coordinate the next step (assign more work, close out, etc.).
//
// This closes a gap between the compute-level test
// (TestShouldEnqueueSquadLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader,
// worker_agent_comment_wakes_squad_leader case, which uses empty
// commentTriggerComputeOptions) and the DualRole test (which reuses the leader
// agent as its own worker via is_leader_task=false). A pure worker agent has
// its own task row, its own OriginatorUserID lineage, and posts via the full
// HTTP CreateComment surface.
func TestCreateComment_WorkerAgentCommentWakesSquadLeader_MUL4015(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	// Seed a running worker task for W (fx.OtherID) — the worker agent
	// was triggered by an earlier @-mention from L and is currently
	// executing. is_leader_task=FALSE marks the task as a worker role.
	var workerRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.OtherID).Scan(&workerRuntimeID); err != nil {
		t.Fatalf("load worker runtime: %v", err)
	}
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'running', FALSE, $4, $4)
		RETURNING id
	`, fx.OtherID, workerRuntimeID, issueID, testUserID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}

	// Seed a completed leader task for L so the self-trigger guard would
	// suppress if it were keyed only on the leader's own history. The completed
	// status keeps it out of the pending-task dedup.
	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'completed', TRUE, $4, $4)
	`, fx.LeaderID, leaderRuntimeID, issueID, testUserID); err != nil {
		t.Fatalf("seed leader task: %v", err)
	}

	// W posts a result comment in its agent identity (X-Agent-ID + X-Task-ID,
	// the pair required by resolveActor to trust the agent header).
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "done — pushed the change, PR is up",
	})
	r.Header.Set("X-Agent-ID", fx.OtherID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// A new leader-role task is enqueued for L so the leader coordinates
	// next steps.
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, fx.LeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 1 {
		t.Fatalf("after worker comment: expected 1 queued leader task for L, got %d", leaderTasks)
	}
}

// TestCreateComment_WorkerAgentCommentQueuesSeparatelyFromLeaderAssignment
// pins the dedup behavior: when the squad leader ALREADY has a queued or
// dispatched task on the issue, a worker's completion comment does not double-
// enqueue a second leader task. This is the desired "coalescing" behavior in
// production — the leader is going to run once and will observe the worker's
// comment in that run. Regression coverage so nobody drops the dedup and
// starts stacking duplicate leader runs.
func TestCreateComment_WorkerAgentCommentQueuesSeparatelyFromLeaderAssignment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	var workerRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.OtherID).Scan(&workerRuntimeID); err != nil {
		t.Fatalf("load worker runtime: %v", err)
	}
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'running', FALSE, $4, $4)
		RETURNING id
	`, fx.OtherID, workerRuntimeID, issueID, testUserID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}

	// Seed an ALREADY QUEUED leader task — models the race where the leader
	// has already been re-triggered (by @mention or child-done) and is
	// waiting to run.
	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, 'queued', TRUE, $4, $4)
	`, fx.LeaderID, leaderRuntimeID, issueID, testUserID); err != nil {
		t.Fatalf("seed queued leader task: %v", err)
	}

	// W posts a result comment.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "done",
	})
	r.Header.Set("X-Agent-ID", fx.OtherID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The new comment thread queues independently of the assignment run.
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, fx.LeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 2 {
		t.Fatalf("expected separate assignment and comment tasks, got %d", leaderTasks)
	}
}

// TestCreateComment_WorkerAgentCommentWakesPrivateSquadLeader_MUL4015 pins
// the private-leader case of the MUL-4015 regression. The default agent
// permission_mode is 'private' (owner-only invocation), so this is the common
// production shape when the assigning member ALSO owns the squad's leader.
//
// The failure mode is:
//
//   - Member M owns squad leader L (private) and worker W (private).
//   - M assigns the issue to the squad → L's task carries originator=M.
//   - L runs and posts a comment @-mentioning W via HTTP CreateComment.
//     The HTTP handler creates the comment but does NOT set source_task_id
//     on the row, breaking the originator inheritance chain.
//   - W's task is enqueued with originator=NULL because
//     resolveOriginatorFromTriggerComment reads back an agent comment whose
//     source_task_id is invalid.
//   - W runs, W posts a "done" comment. invokeOriginatorFromRequest returns ""
//     (W's task originator is NULL).
//   - routeAssignedSquadLeaderFallback calls canInvokeAgent(L, "agent", W, "").
//     For a private leader that fails closed: effectiveUser is empty and
//     L.OwnerID != "".
//   - Leader is never woken → the leader→worker→leader loop stays broken.
//
// The fix: HTTP CreateComment must stamp source_task_id on agent-authored
// comments (using X-Task-ID) so the trigger-chain originator inheritance
// survives the mention hop.
func TestCreateComment_WorkerAgentCommentWakesPrivateSquadLeader_MUL4015(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Manually build a private-leader + private-worker fixture. testUserID owns
	// both agents. There is no workspace or member invocation target: the only
	// admissible caller under canInvokeAgent is the owner themselves.
	privateAgent := func(name string) string {
		var agentID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config,
				runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
				instructions, custom_env, custom_args, mcp_config
			)
			VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
			RETURNING id
		`, testWorkspaceID, name, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
			t.Fatalf("failed to create private agent %q: %v", name, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		})
		return agentID
	}

	leaderID := privateAgent("MUL-4015 Private Leader")
	workerID := privateAgent("MUL-4015 Private Worker")

	// Squad with the private leader.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'MUL-4015 Private Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create private squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Issue assigned to the squad, created by M (testUserID). CreatorType=member
	// keeps the assign-time originator resolution to M.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, 'private squad worker-comment MUL-4015', 'squad', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, squadID).Scan(&issueID); err != nil {
		t.Fatalf("create private squad issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Simulate the leader→worker mention hop that would happen in production:
	//   1. Seed a running leader task with originator=M.
	//   2. Post the leader's @Worker mention comment via HTTP CreateComment
	//      (with X-Agent-ID=L, X-Task-ID=leader-task). This is the path that
	//      currently loses source_task_id on the created comment row.
	//   3. Verify the worker's task was enqueued.
	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, leaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	var leaderTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, originator_user_id, accountable_user_id, squad_id)
		VALUES ($1, $2, $3, 'running', TRUE, $4, $4, $5)
		RETURNING id
	`, leaderID, leaderRuntimeID, issueID, testUserID, squadID).Scan(&leaderTaskID); err != nil {
		t.Fatalf("seed leader task: %v", err)
	}

	// Post the leader's mention comment via HTTP.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Worker](mention://agent/" + workerID + ") please handle",
	})
	r.Header.Set("X-Agent-ID", leaderID)
	r.Header.Set("X-Task-ID", leaderTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("leader mention CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, workerID).Scan(&workerTaskID); err != nil {
		t.Fatalf("worker task not enqueued from leader mention: %v", err)
	}

	// The worker's task MUST have inherited originator=M so the later
	// canInvokeAgent(private leader) can pass on the private path. This is
	// the load-bearing assertion.
	var workerOriginator pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT originator_user_id FROM agent_task_queue WHERE id = $1`, workerTaskID).Scan(&workerOriginator); err != nil {
		t.Fatalf("read worker originator: %v", err)
	}
	if !workerOriginator.Valid || uuidToString(workerOriginator) != testUserID {
		t.Fatalf("worker task originator = %v (valid=%v), want %s — originator inheritance broken (comment.source_task_id not stamped)",
			uuidToString(workerOriginator), workerOriginator.Valid, testUserID)
	}

	// Flip the worker task to running so it can post a comment as an
	// authenticated agent.
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, workerTaskID); err != nil {
		t.Fatalf("advance worker task to running: %v", err)
	}
	// Complete the leader task so it stops showing as pending.
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, leaderTaskID); err != nil {
		t.Fatalf("complete leader task: %v", err)
	}

	// Now the worker posts a "done" comment. This must wake the private
	// leader via routeAssignedSquadLeaderFallback → canInvokeAgent(L, "agent",
	// W, originator=M): the effective user M matches L.OwnerID so the
	// private-only gate opens.
	//
	// The CLI contract requires the worker's reply carry parent_id equal to
	// its task's trigger_comment_id (which is L's mention comment). Look up
	// that trigger_comment_id from the worker task's row.
	var workerTriggerCommentID string
	if err := testPool.QueryRow(ctx, `SELECT trigger_comment_id FROM agent_task_queue WHERE id = $1`, workerTaskID).Scan(&workerTriggerCommentID); err != nil {
		t.Fatalf("read worker trigger_comment_id: %v", err)
	}
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "done — pushed the change",
		"parent_id": workerTriggerCommentID,
	})
	r.Header.Set("X-Agent-ID", workerID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("worker done CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var leaderTasksQueued int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, leaderID).Scan(&leaderTasksQueued); err != nil {
		t.Fatalf("count queued leader tasks: %v", err)
	}
	if leaderTasksQueued != 1 {
		t.Fatalf("after worker done: expected 1 queued leader task for private L, got %d — leader→worker→leader loop broken for private leader (MUL-4015)",
			leaderTasksQueued)
	}
}
