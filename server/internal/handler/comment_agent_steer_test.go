package handler

import (
	"context"
	"fmt"
	"strings"
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// agentSteerFixture is a running, steer-capable target turn (the embedded
// supplementFixture) plus a running caller agent whose chain was started by
// the workspace owner, so its @mention may start the target.
type agentSteerFixture struct {
	supplementFixture
	callerAgent, callerTask string
}

func newAgentSteerFixture(t *testing.T) agentSteerFixture {
	t.Helper()
	f := newSupplementFixture(t, "claude", "running", true)
	dbfx.Exec(t, `UPDATE agent_task_queue SET delivered_comment_ids = ARRAY[trigger_comment_id] WHERE id = $1`, f.taskID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	callerAgent, callerTask := runningAgentCaller(t, f, "Steer caller")
	return agentSteerFixture{supplementFixture: f, callerAgent: callerAgent, callerTask: callerTask}
}

func runningAgentCaller(t *testing.T, f supplementFixture, name string) (agentID, taskID string) {
	t.Helper()
	agentID, taskID = runningSteerableTask(t, f, name)
	dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id = $2, accountable_user_id = $2, originator_source = 'comment_source' WHERE id = $1`, taskID, testUserID)
	return agentID, taskID
}

func agentRequest(method, path string, body any, agentID, taskID string) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func postAgentComment(t *testing.T, issueID, agentID, taskID string, body map[string]any) CommentResponse {
	t.Helper()
	req := agentRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body, agentID, taskID)
	var created CommentResponse
	testutil.Call(t, testHandler.CreateComment, withURLParam(req, "id", issueID)).Want(http.StatusCreated).JSON(&created)
	return created
}

func steeredAgent(created CommentResponse, agentID string) bool {
	outcome, ok := outcomeFor(created.TriggerOutcomes, agentID)
	return ok && outcome.Status == DispatchSteered
}

func TestAgentCommentSteersAnAgentItMayInvoke(t *testing.T) {
	for _, tc := range []struct {
		name         string
		body         func(f agentSteerFixture) map[string]any
		noOriginator bool
		self         bool
		wantSteered  bool
	}{
		{name: "named turn", wantSteered: true, body: func(f agentSteerFixture) map[string]any {
			return map[string]any{"content": agentMention("Target", f.agentID) + " only fix web", "steer_task_ids": []string{f.taskID}}
		}},
		{name: "steer resolves the running turn", wantSteered: true, body: func(f agentSteerFixture) map[string]any {
			return map[string]any{"content": agentMention("Target", f.agentID) + " only fix web", "steer": true}
		}},
		{name: "no steer intent keeps the normal trigger", body: func(f agentSteerFixture) map[string]any {
			return map[string]any{"content": agentMention("Target", f.agentID) + " only fix web"}
		}},
		{name: "chain that may not invoke the target", noOriginator: true, body: func(f agentSteerFixture) map[string]any {
			return map[string]any{"content": agentMention("Target", f.agentID) + " only fix web", "steer": true}
		}},
		{name: "own running turn", self: true, body: func(f agentSteerFixture) map[string]any {
			return map[string]any{"content": agentMention("Me", f.callerAgent) + " note to self", "steer_task_ids": []string{f.callerTask}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAgentSteerFixture(t)
			if tc.noOriginator {
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id = NULL, accountable_user_id = NULL, originator_source = NULL WHERE id = $1`, f.callerTask)
			}
			created := postAgentComment(t, f.issueID, f.callerAgent, f.callerTask, tc.body(f))
			target, targetTask := f.agentID, f.taskID
			if tc.self {
				target, targetTask = f.callerAgent, f.callerTask
			}
			if got := steeredAgent(created, target); got != tc.wantSteered {
				t.Fatalf("steered = %v, want %v; outcomes %+v", got, tc.wantSteered, created.TriggerOutcomes)
			}
			receipts := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE comment_id = $1 AND task_id = $2`, created.ID, targetTask)
			if want := map[bool]int{true: 1, false: 0}[tc.wantSteered]; receipts != want {
				t.Fatalf("receipts = %d, want %d", receipts, want)
			}
		})
	}
}

// insertAgentBinding records an earlier agent-authored steer of taskID.
func insertAgentBinding(t *testing.T, f supplementFixture, taskID, authorAgentID string) {
	t.Helper()
	commentID := dbfx.Comment(t, f.issueID, "earlier steer", testutil.Cols{"author_type": "agent", "author_id": authorAgentID})
	dbfx.Exec(t, `INSERT INTO task_supplement (task_id, workspace_id, issue_id, comment_id, author_id, client_request_id, status)
		VALUES ($1, $2, $3, $4, $5, $4, 'delivered')`, taskID, testWorkspaceID, f.issueID, commentID, authorAgentID)
}

func TestAgentSteeringIsCappedPerAgentAndPerTurn(t *testing.T) {
	t.Run("per authoring agent", func(t *testing.T) {
		f := newAgentSteerFixture(t)
		body := map[string]any{"content": agentMention("Target", f.agentID) + " narrow it", "steer": true}
		for i := range maxAgentSteersPerAgentPerTurn + 1 {
			created := postAgentComment(t, f.issueID, f.callerAgent, f.callerTask, body)
			if got, want := steeredAgent(created, f.agentID), i < maxAgentSteersPerAgentPerTurn; got != want {
				t.Fatalf("steer %d: steered = %v, want %v; outcomes %+v", i+1, got, want, created.TriggerOutcomes)
			}
		}
	})
	t.Run("per turn, agents only", func(t *testing.T) {
		f := newAgentSteerFixture(t)
		for i := range maxAgentSteersPerTurn - 1 {
			insertAgentBinding(t, f.supplementFixture, f.taskID, dbfx.Agent(t, fmt.Sprintf("Earlier steerer %d", i), f.runtimeID))
		}
		body := map[string]any{"content": agentMention("Target", f.agentID) + " narrow it", "steer": true}
		if created := postAgentComment(t, f.issueID, f.callerAgent, f.callerTask, body); !steeredAgent(created, f.agentID) {
			t.Fatalf("last slot not used: %+v", created.TriggerOutcomes)
		}
		otherAgent, otherTask := runningAgentCaller(t, f.supplementFixture, "Second caller")
		if created := postAgentComment(t, f.issueID, otherAgent, otherTask, body); steeredAgent(created, f.agentID) {
			t.Fatalf("full turn was steered: %+v", created.TriggerOutcomes)
		}
		var member CommentResponse
		postSteeredComment(t, f.issueID, "", agentMention("Target", f.agentID)+" member note", f.taskID).Want(http.StatusCreated).JSON(&member)
		if !steeredAgent(member, f.agentID) {
			t.Fatalf("member steering was capped: %+v", member.TriggerOutcomes)
		}
	})
}

func TestAgentSteeringRaceForTheLastSlotBindsOnce(t *testing.T) {
	f := newAgentSteerFixture(t)
	for range maxAgentSteersPerAgentPerTurn - 1 {
		insertAgentBinding(t, f.supplementFixture, f.taskID, f.callerAgent)
	}
	const racers = 4
	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := range racers {
		commentID := dbfx.Comment(t, f.issueID, "racing steer", testutil.Cols{"author_type": "agent", "author_id": f.callerAgent})
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, results[i] = testHandler.bindCommentSteer(context.Background(), db.BindCommentTaskSupplementParams{
				TaskID: parseUUID(f.taskID), IssueID: parseUUID(f.issueID), AgentID: parseUUID(f.agentID),
				WorkspaceID: parseUUID(testWorkspaceID), AuthorType: "agent",
				MaxAgentBindings: maxAgentSteersPerTurn, MaxBindingsPerAgent: maxAgentSteersPerAgentPerTurn,
				CommentID: parseUUID(commentID), AuthorID: parseUUID(f.callerAgent), ClientRequestID: parseUUID(commentID),
			})
		}()
	}
	close(start)
	wg.Wait()
	bound := 0
	for _, err := range results {
		if err == nil {
			bound++
		}
	}
	if bound != 1 {
		t.Fatalf("%d racers bound the last slot, want 1: %v", bound, results)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1`, f.taskID); n != maxAgentSteersPerAgentPerTurn {
		t.Fatalf("turn has %d bindings, want %d", n, maxAgentSteersPerAgentPerTurn)
	}
}

// An agent's steering receipt has no human watching it, so a message the turn
// never read is replayed as an ordinary mention instead of being lost.
func TestAgentSteeredCommentReplaysUnlessDelivered(t *testing.T) {
	for _, tc := range []struct {
		status        string
		wantFollowUps int
	}{
		{status: "pending", wantFollowUps: 1},
		{status: "failed", wantFollowUps: 1},
		{status: "delivered", wantFollowUps: 0},
	} {
		t.Run(tc.status, func(t *testing.T) {
			f := newAgentSteerFixture(t)
			created := postAgentComment(t, f.issueID, f.callerAgent, f.callerTask, map[string]any{
				"content": agentMention("Target", f.agentID) + " only fix web", "steer": true,
			})
			if !steeredAgent(created, f.agentID) {
				t.Fatalf("not steered: %+v", created.TriggerOutcomes)
			}
			dbfx.Exec(t, `UPDATE task_supplement SET status = $2, failure_reason = CASE WHEN $2 = 'failed' THEN 'provider_rejected' END WHERE comment_id = $1`, created.ID, tc.status)
			completeTaskViaDaemon(t, f.taskID)
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND trigger_comment_id = $2`, f.agentID, created.ID); n != tc.wantFollowUps {
				t.Fatalf("follow-ups = %d, want %d", n, tc.wantFollowUps)
			}
			// A replayed steer loses its failed receipt, so no client offers to
			// resend what the follow-up already handles.
			if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE comment_id = $1`, created.ID); n != 1-tc.wantFollowUps {
				t.Fatalf("receipts after completion = %d, want %d", n, 1-tc.wantFollowUps)
			}
		})
	}
}

func TestClaimTaskSupplementAttributesAgentMessages(t *testing.T) {
	f := newAgentSteerFixture(t)
	created := postAgentComment(t, f.issueID, f.callerAgent, f.callerTask, map[string]any{
		"content": agentMention("Target", f.agentID) + " only fix web", "steer": true,
	})
	if !steeredAgent(created, f.agentID) {
		t.Fatalf("not steered: %+v", created.TriggerOutcomes)
	}
	claim := func(capabilities []string) map[string]string {
		body := map[string]any{"capabilities": capabilities}
		req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/claim", body), "taskId", f.taskID)
		var got map[string]string
		testutil.Call(t, testHandler.ClaimTaskSupplement, req).Want(http.StatusOK).JSON(&got)
		return got
	}

	// A daemon that frames every message as a human's is never handed one.
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/claim", map[string]any{}), "taskId", f.taskID)
	var old map[string]string
	testutil.Call(t, testHandler.ClaimTaskSupplement, req).Want(http.StatusOK).JSON(&old)
	if len(old) != 0 {
		t.Fatalf("old-daemon claim = %+v, want nothing", old)
	}
	current := claim([]string{protocol.DaemonCapabilityTaskSupplementAuthorV1})
	if current["author_type"] != "agent" || current["author_name"] != "Steer caller" || current["content"] != created.Content {
		t.Fatalf("author-aware claim = %+v", current)
	}
}

func TestTaskRunsCanSupplementAnswersForTheAgentCaller(t *testing.T) {
	f := newAgentSteerFixture(t)
	runs := func() map[string]bool {
		req := agentRequest(http.MethodGet, "/api/issues/"+f.issueID+"/task-runs", nil, f.callerAgent, f.callerTask)
		var resp []AgentTaskResponse
		testutil.Call(t, testHandler.ListTasksByIssue, withURLParam(req, "id", f.issueID)).Want(http.StatusOK).JSON(&resp)
		out := make(map[string]bool, len(resp))
		for _, run := range resp {
			out[run.ID] = run.CanSupplement
		}
		return out
	}
	got := runs()
	if !got[f.taskID] || got[f.callerTask] {
		t.Fatalf("can_supplement = %v, want the target's run only", got)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id = NULL, accountable_user_id = NULL, originator_source = NULL WHERE id = $1`, f.callerTask)
	if got := runs(); got[f.taskID] {
		t.Fatalf("can_supplement = %v for a chain that may not invoke the target", got)
	}
}

// A worker's plain reply reaches its squad leader without a mention. When it
// steered the leader's turn and was never read, completion still replays it.
func TestAgentImplicitSteerReplaysWhenUnread(t *testing.T) {
	f := newAgentSteerFixture(t)
	squadID := dbfx.Squad(t, "Implicit steer squad", f.agentID)
	dbfx.SquadMember(t, squadID, "agent", f.callerAgent)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'squad', assignee_id = $2 WHERE id = $1`, f.issueID, squadID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET squad_id = $2, is_leader_task = true WHERE id = $1`, f.taskID, squadID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET squad_id = $2, trigger_comment_id = $3 WHERE id = $1`, f.callerTask, squadID, f.triggerID)
	created := postAgentComment(t, f.issueID, f.callerAgent, f.callerTask, map[string]any{
		"content": "The premise is wrong; use the corrected result", "parent_id": f.triggerID, "steer": true,
	})
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1 AND comment_id = $2`, f.taskID, created.ID); n != 1 {
		t.Fatalf("setup: %d steering bindings; outcomes %+v", n, created.TriggerOutcomes)
	}
	completeTaskViaDaemon(t, f.taskID)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND trigger_comment_id = $2`, f.agentID, created.ID); n != 1 {
		t.Fatalf("unread implicit steer follow-ups = %d, want 1", n)
	}
}

func TestSquadSteerRuleFollowsTheDaemonsCLI(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squad := db.Squad{Name: "Steer rule"}
	with := buildSquadLeaderBriefing(t.Context(), testHandler.Queries, squad, false, leaderNoWorkflow, true)
	without := buildSquadLeaderBriefing(t.Context(), testHandler.Queries, squad, false, leaderNoWorkflow, false)
	if !strings.Contains(with, "--steer-active") || strings.Contains(without, "--steer") {
		t.Fatalf("steer rule: with=%v without=%v", strings.Contains(with, "--steer-active"), strings.Contains(without, "--steer"))
	}
}
