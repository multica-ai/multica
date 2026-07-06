package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCommentMentionsAnyone covers the pure helper that drives the
// "skip leader on @<anyone>" behavior. Routing-style mentions
// (agent/member/squad/all) count; issue cross-references do not. Kept as a
// unit test so it runs without a database connection.
func TestCommentMentionsAnyone(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty", content: "", want: false},
		{name: "plain text", content: "please take a look", want: false},
		{name: "literal at sign only", content: "ping @alice", want: false},
		{name: "agent mention", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) handle this", want: true},
		{name: "member mention", content: "[@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "squad mention", content: "[@Squad](mention://squad/44444444-4444-4444-4444-444444444444)", want: true},
		{name: "mention all", content: "[@all](mention://all/all)", want: true},
		{name: "issue mention only", content: "see [MUL-1](mention://issue/33333333-3333-3333-3333-333333333333)", want: false},
		{name: "issue + plain text", content: "see [MUL-1](mention://issue/33333333-3333-3333-3333-333333333333) for context", want: false},
		{name: "agent plus member", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) cc [@B](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "issue plus member", content: "blocks [MUL-1](mention://issue/33333333-3333-3333-3333-333333333333) — [@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commentMentionsAnyone(tc.content); got != tc.want {
				t.Fatalf("commentMentionsAnyone(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

// shouldEnqueueSquadLeaderOnCommentForTest reports whether the shared cascade
// would wake the issue's assigned squad leader.
func shouldEnqueueSquadLeaderOnCommentForTest(ctx context.Context, issue db.Issue, content, authorType, authorID string) bool {
	return triggersContainIssueAssigneeSquadLeader(testHandler.computeCommentAgentTriggers(ctx, issue, content, nil, authorType, authorID, commentTriggerComputeOptions{}))
}

func shouldEnqueueSquadLeaderOnReplyForTest(ctx context.Context, issue db.Issue, content string, parent *db.Comment, authorType, authorID string) bool {
	return triggersContainIssueAssigneeSquadLeader(testHandler.computeCommentAgentTriggers(ctx, issue, content, parent, authorType, authorID, commentTriggerComputeOptions{}))
}

func triggersContainIssueAssigneeSquadLeader(triggers []commentAgentTrigger) bool {
	for _, trigger := range triggers {
		if trigger.Source == commentTriggerSourceIssueAssignee && trigger.Squad != nil {
			return true
		}
	}
	return false
}

// squadCommentTriggerFixture wires a squad assigned to a fresh issue and
// returns the loaded db.Issue plus the leader agent UUID for use in
// cascade integration tests.
type squadCommentTriggerFixture struct {
	Issue    db.Issue
	SquadID  string
	LeaderID string
	OtherID  string // second agent in workspace (with runtime), used as a non-leader @mention target
}

func newSquadCommentTriggerFixture(t *testing.T) squadCommentTriggerFixture {
	t.Helper()
	ctx := context.Background()

	// Reuse the seeded "Handler Test Agent" as the leader — it has a runtime.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	// Spin up a second agent in the same workspace as a non-leader mention
	// target. createHandlerTestAgent installs a t.Cleanup row deletion.
	otherID := createHandlerTestAgent(t, "Squad Comment Other", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Squad Comment Trigger", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, $3, 'squad', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, "squad comment trigger", squadID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	return squadCommentTriggerFixture{
		Issue:    issue,
		SquadID:  squadID,
		LeaderID: leaderID,
		OtherID:  otherID,
	}
}

// TestShouldEnqueueSquadLeaderOnComment_SkipsWhenCommentRoutesElsewhere
// pins the cascade: explicit participant mentions do not also wake the assigned
// squad leader. Issue cross-references are not routing and do not suppress the
// leader.
func TestShouldEnqueueSquadLeaderOnComment_SkipsWhenMemberMentionsAnyone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSquadCommentTriggerFixture(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		content     string
		authorType  string
		authorID    string
		want        bool
		description string
	}{
		{
			name:        "member plain comment triggers leader",
			content:     "what is the latest on this?",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "no @ in body → leader must coordinate as today",
		},
		{
			name:        "member issue cross-reference only triggers leader",
			content:     "blocked by [MUL-1](mention://issue/" + testUserID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "issue mentions are not routing — leader still owns dispatch",
		},
		{
			name:        "member mentions another member skips leader",
			content:     "[@self](mention://member/" + testUserID + ") please weigh in",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at a human — leader stays out (extended rule)",
		},
		{
			name:        "member mentions non-leader agent skips leader",
			content:     "[@Other](mention://agent/" + fx.OtherID + ") please take this",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at an agent — leader stays out",
		},
		{
			name:        "member mentions leader skips leader on comment path",
			content:     "[@Leader](mention://agent/" + fx.LeaderID + ") your call",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "even @leader is dispatched via the mention path; comment path must not double-enqueue",
		},
		{
			name:        "member mention all skips leader",
			content:     "[@all](mention://all/all) heads up",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@all is a broadcast — leader does not need to wake to evaluate routing",
		},
		{
			name:        "member mentions a squad skips leader",
			content:     "handing to [@Other Squad](mention://squad/" + fx.SquadID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@squad routes the issue to that squad's leader — current leader stays out",
		},
		{
			name:        "agent comment with @agent does not also trigger leader",
			content:     "delegating to [@Other](mention://agent/" + fx.OtherID + ")",
			authorType:  "agent",
			authorID:    fx.OtherID,
			want:        false,
			description: "explicit @agent routes only to the mentioned target; the assigned squad-leader fallback must not also fire (no double-enqueue)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, tc.content, tc.authorType, tc.authorID)
			if got != tc.want {
				t.Fatalf("%s\n  content=%q author=%s/%s\n  got=%v want=%v",
					tc.description, tc.content, tc.authorType, tc.authorID, got, tc.want)
			}
		})
	}
}

// TestShouldEnqueueSquadLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader
// pins the MUL-3879 restored behavior in the new MUL-3794 cascade: an
// agent-authored worker-result comment on a squad-assigned issue wakes the
// assigned squad leader so the leader→worker→leader coordination loop stays
// closed, while the leader's own self-trigger loop stays suppressed via
// lastTaskWasLeader.
func TestShouldEnqueueSquadLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSquadCommentTriggerFixture(t)
	ctx := context.Background()
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	clearTasks := func() {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
			t.Fatalf("clear tasks: %v", err)
		}
	}
	// insertLeaderTask seeds a task for the leader agent so the
	// lastTaskWasLeader guard can read the agent's most recent role on the
	// issue. Separate Exec calls get distinct created_at values, so the last
	// inserted row is the "latest" task.
	insertLeaderTask := func(isLeader bool, status string) {
		t.Helper()
		var runtimeID string
		if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
			t.Fatalf("load runtime: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task)
			VALUES ($1, $2, $3, $4, $5)
		`, fx.LeaderID, runtimeID, issueID, status, isLeader); err != nil {
			t.Fatalf("insert task: %v", err)
		}
	}

	// Case 1: a worker agent (not the leader) posts a result comment on the
	// squad-assigned issue — the assigned leader must wake to coordinate.
	t.Run("worker agent comment wakes squad leader", func(t *testing.T) {
		clearTasks()
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, "pushed the fix, PR is up", "agent", fx.OtherID); !got {
			t.Fatalf("worker agent comment: expected leader to wake, got skip")
		}
	})

	// Case 2: a dual-role agent (leader of the squad, also runs worker tasks)
	// posts while its latest task on the issue was a worker task — the leader
	// role must still wake because the comment is a worker result, not a
	// leader self-trigger.
	t.Run("dual-role worker comment wakes leader when latest task is worker", func(t *testing.T) {
		clearTasks()
		insertLeaderTask(true, "completed")  // older leader task
		insertLeaderTask(false, "completed") // newer worker task → latest role is worker
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, "done with my worker slice", "agent", fx.LeaderID); !got {
			t.Fatalf("dual-role worker comment: expected leader to wake, got skip")
		}
	})

	// Case 3: the leader posts while its latest task was a leader task — this
	// is a self-trigger loop and must stay suppressed.
	t.Run("leader comment from latest leader task does not self-trigger", func(t *testing.T) {
		clearTasks()
		insertLeaderTask(false, "completed") // older worker task
		insertLeaderTask(true, "completed")  // newer leader task → latest role is leader
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, "coordinating next steps", "agent", fx.LeaderID); got {
			t.Fatalf("leader self-trigger: expected skip, got wake")
		}
	})

	// Case 4: an agent-authored comment carrying an explicit @agent mention
	// routes only to the mentioned target — the assigned squad leader must NOT
	// also be enqueued via the fallback path (no double-enqueue).
	t.Run("explicit mention does not double-enqueue assigned leader", func(t *testing.T) {
		clearTasks()
		content := "handing to [@Other](mention://agent/" + fx.OtherID + ")"
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Issue, content, "agent", fx.LeaderID); got {
			t.Fatalf("explicit mention: expected no assigned-leader fallback, got wake")
		}
	})
}

// TestCreateComment_SquadPlainReplyToMemberParentKeepsRootMentionOwner drives the
// full CreateComment handler to lock the cascade's reply behavior:
//
//   - A member top-level comment that @mentions another agent does NOT
//     enqueue the squad leader (the mentioned agent owns the next step).
//   - A subsequent member reply to that member-authored root with no explicit
//     agent mention continues to the root owner instead of the assignee.
func TestCreateComment_SquadPlainReplyToMemberParentKeepsRootMentionOwner(t *testing.T) {
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

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode comment: %v", err)
		}
		return resp
	}

	// 1. Member top-level comment mentions OtherAgent.
	//    Leader must be skipped; OtherAgent must be enqueued via the mention path.
	parent := postMemberComment(map[string]any{
		"content": "[@Other](mention://agent/" + fx.OtherID + ") please take this",
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after parent (@OtherAgent): expected 0 leader tasks (skipped), got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after parent (@OtherAgent): expected 1 OtherAgent task (mention path), got %d", got)
	}

	// 2. Mark OtherAgent's parent task done so queued-task counts below only
	//    reflect what the plain reply does.
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'completed'
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, fx.OtherID); err != nil {
		t.Fatalf("complete OtherAgent parent task: %v", err)
	}

	// 3. Member posts a reply in the same thread with NO mentions.
	//    The root's @OtherAgent mention owns the thread, so the reply returns
	//    to OtherAgent instead of falling back to the assigned squad leader.
	postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after plain reply: expected 0 leader tasks, got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after plain reply: expected 1 OtherAgent task, got %d", got)
	}
}

// TestCreateComment_DualRoleAgentWorkerCommentWakesLeader pins the MUL-3879
// restored coordination loop at the full-handler level. Scenario:
//
//   - Agent L is the leader of squad S and also runs worker tasks on issues
//     belonging to S.
//   - L is woken in its worker role (is_leader_task=false) and posts a result
//     comment.
//   - A leader-role task IS enqueued so the squad leader can coordinate the
//     next step — the worker result must not silently strand the issue.
func TestCreateComment_DualRoleAgentWorkerCommentWakesLeader(t *testing.T) {
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

	// Seed a worker task for the leader agent on this issue so the guard
	// infers "agent's last activity was a worker task" — i.e. L is running
	// in its worker role when it posts the comment. We make it running (not
	// completed) so we can hand its ID back through X-Task-ID for the
	// resolveActor agent-identity check.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task)
		VALUES ($1, $2, $3, 'running', FALSE)
		RETURNING id
	`, fx.LeaderID, runtimeID, issueID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}

	// L posts a comment in its agent identity (X-Agent-ID + X-Task-ID, the
	// pair required by resolveActor to trust the agent header).
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "done — pushed the change",
	})
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// A new leader-role task is enqueued so the leader coordinates next steps.
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, fx.LeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 1 {
		t.Fatalf("after worker comment from dual-role agent: expected 1 queued leader task, got %d", leaderTasks)
	}
}

// TestCreateRetryTask_InheritsIsLeaderTask locks the retry-clone contract for
// MUL-2218: auto-retry of a leader-role task must produce a child task that is
// also is_leader_task=true. Without this, MaybeRetryFailedTask silently
// demotes a retried leader task to a worker task, and role-specific claim-time
// briefing/self-mention guards lose the leader provenance.
func TestCreateRetryTask_InheritsIsLeaderTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	cases := []struct {
		name     string
		isLeader bool
	}{
		{name: "leader task retry stays leader", isLeader: true},
		{name: "worker task retry stays worker", isLeader: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parentID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, attempt, max_attempts, is_leader_task)
				VALUES ($1, $2, $3, 'failed', 1, 3, $4)
				RETURNING id
			`, fx.LeaderID, runtimeID, issueID, tc.isLeader).Scan(&parentID); err != nil {
				t.Fatalf("seed parent task: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1 OR parent_task_id = $1`, parentID)
			})

			child, err := testHandler.Queries.CreateRetryTask(ctx, util.MustParseUUID(parentID))
			if err != nil {
				t.Fatalf("CreateRetryTask: %v", err)
			}
			if child.IsLeaderTask != tc.isLeader {
				t.Fatalf("child.IsLeaderTask = %v, want %v (parent role must be inherited)", child.IsLeaderTask, tc.isLeader)
			}
		})
	}
}

// TestCreateComment_SquadMentionPrivateLeaderBlocksPlainMember verifies that
// a plain workspace member cannot trigger a private squad leader via @squad
// mention. This is the regression test for the P1 finding: without the
// canAccessPrivateAgent gate in the squad mention branch, a member could
// bypass the private-agent restriction by mentioning the squad instead of
// the agent directly.
func TestCreateComment_SquadMentionPrivateLeaderBlocksPlainMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Use privateAgentTestFixture to get a private agent + plain member.
	agentID, _, memberID := privateAgentTestFixture(t)

	// Create a squad with the private agent as leader.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create an issue.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'private leader squad mention test')
		RETURNING id
	`, testWorkspaceID, memberID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Plain member posts a comment mentioning the squad.
	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") please handle",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The private leader must NOT have a queued task — plain member lacks access.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("private leader got %d queued tasks from plain member squad mention; want 0 (access denied)", count)
	}
}

// TestCreateComment_SquadMentionTriggersLeader verifies that @mentioning a
// squad in a comment triggers the squad's leader agent via the mention path,
// even when the issue is NOT assigned to that squad.
func TestCreateComment_SquadMentionTriggersLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create a squad with a leader agent.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Mention Trigger Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create an issue NOT assigned to the squad (assigned to nobody).
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'squad mention trigger test')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	// Post a comment that @mentions the squad.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") please handle this",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The squad's leader should have a queued task.
	if got := countQueued(leaderID); got != 1 {
		t.Fatalf("after @squad mention: expected 1 leader task, got %d", got)
	}
}
