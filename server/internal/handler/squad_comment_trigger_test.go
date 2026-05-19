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

// squadCommentTriggerFixture wires a squad assigned to a fresh issue and
// returns the loaded db.Issue plus the leader agent UUID for use in
// shouldEnqueueSquadLeaderOnComment integration tests.
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

// TestShouldEnqueueSquadLeaderOnComment_SkipsWhenMemberMentionsAnyone
// encodes Bohan's rule (MUL-2170): a member comment that explicitly @mentions
// anyone — agent, member, squad, or @all — must NOT wake the squad leader.
// Issue cross-references are not routing and do not suppress the leader.
// Agent-authored comments are exempt: the leader still coordinates threads.
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
			name:        "agent comment with @agent still triggers leader",
			content:     "delegating to [@Other](mention://agent/" + fx.OtherID + ")",
			authorType:  "agent",
			authorID:    fx.OtherID,
			want:        true,
			description: "agent-authored replies always reach leader so it can coordinate next step",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := testHandler.shouldEnqueueSquadLeaderOnComment(ctx, fx.Issue, tc.content, tc.authorType, tc.authorID)
			if got != tc.want {
				t.Fatalf("%s\n  content=%q author=%s/%s\n  got=%v want=%v",
					tc.description, tc.content, tc.authorType, tc.authorID, got, tc.want)
			}
		})
	}
}

// TestShouldEnqueueSquadLeaderOnComment_LeaderSelfTriggerByRole covers the
// role-aware self-trigger guard added for MUL-2218. The leader agent itself
// should be skipped only when its last activity on the issue was a leader
// task — never just because the comment author equals the leader ID. This
// matters for dual-role agents (leader + worker of the same squad): a
// comment posted from the worker task must still wake the leader.
func TestShouldEnqueueSquadLeaderOnComment_LeaderSelfTriggerByRole(t *testing.T) {
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
	insertTask := func(isLeader bool, status string) {
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

	t.Run("no prior task wakes leader (fresh external trigger)", func(t *testing.T) {
		clearTasks()
		if got := testHandler.shouldEnqueueSquadLeaderOnComment(ctx, fx.Issue, "noted", "agent", fx.LeaderID); !got {
			t.Fatalf("no prior task: expected leader to be enqueued, got skip")
		}
	})

	t.Run("prior leader task suppresses self-trigger", func(t *testing.T) {
		clearTasks()
		insertTask(true, "completed")
		if got := testHandler.shouldEnqueueSquadLeaderOnComment(ctx, fx.Issue, "noted", "agent", fx.LeaderID); got {
			t.Fatalf("after leader task: expected skip (anti-loop), got enqueue")
		}
	})

	t.Run("prior worker task still wakes leader (dual-role agent)", func(t *testing.T) {
		clearTasks()
		insertTask(false, "completed")
		if got := testHandler.shouldEnqueueSquadLeaderOnComment(ctx, fx.Issue, "result", "agent", fx.LeaderID); !got {
			t.Fatalf("after worker task: expected leader to be enqueued (MUL-2218), got skip")
		}
	})

	t.Run("most recent task is the one that matters", func(t *testing.T) {
		clearTasks()
		insertTask(true, "completed")  // older leader task
		insertTask(false, "completed") // newer worker task
		if got := testHandler.shouldEnqueueSquadLeaderOnComment(ctx, fx.Issue, "result", "agent", fx.LeaderID); !got {
			t.Fatalf("latest task is worker: expected leader to be enqueued, got skip")
		}
	})
}

// TestCreateComment_SquadLeaderSkipOnlyInspectsCurrentMention drives the
// full CreateComment handler to lock the call-site wiring (comment.go) for
// the squad-leader-skip rule. Specifically it proves that:
//
//   - A member top-level comment that @mentions another agent does NOT
//     enqueue the squad leader (the mentioned agent owns the next step).
//   - A subsequent member REPLY in the same thread, containing no mentions
//     of its own, DOES enqueue the squad leader — i.e. the parent's
//     @agent mention is not inherited into the leader-skip decision.
//
// The matching unit test above exercises the helper in isolation; this
// test catches a class of regression where someone refactors comment.go
// to pass the parent's content (or the merged thread content) by mistake.
func TestCreateComment_SquadLeaderSkipOnlyInspectsCurrentMention(t *testing.T) {
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

	// 2. Member posts a reply in the same thread with NO mentions.
	//    The leader-skip helper must inspect only the reply's body (empty),
	//    NOT the parent's @OtherAgent mention. Leader must wake up.
	postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})
	if got := countQueued(fx.LeaderID); got != 1 {
		t.Fatalf("after plain reply: expected 1 leader task (no parent inheritance), got %d", got)
	}
}

// TestCreateComment_DualRoleAgentWorkerCommentWakesLeader is the full-stack
// regression test for MUL-2218. Scenario:
//
//   - Agent L is the leader of squad S and also a worker assigned tasks on
//     issues belonging to S.
//   - L is woken in its worker role (is_leader_task=false) and posts a comment.
//   - The squad-leader self-trigger guard MUST still wake L in its leader role
//     so it can react to the worker output (e.g. delegate the next step).
//
// Before the fix the role-blind authorID == leaderID check skipped the
// leader, leaving the issue stalled.
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

	// A NEW leader-role task must be enqueued for L on this issue so the
	// leader role can react to its own worker output.
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
// demotes a retried leader task to a worker task, and the self-trigger guard
// in shouldEnqueueSquadLeaderOnComment / comment.go stops recognising the
// retried leader's own comments — re-opening the bug this issue fixes.
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
