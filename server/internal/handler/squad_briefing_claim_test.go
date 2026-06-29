package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// claimAgentInstructionsForTest claims the next queued task for runtimeID and
// returns the claimed task id plus the agent Instructions carried on the claim
// response (the field the squad-leader briefing is injected into). Empty task
// id means no task was claimed.
func claimAgentInstructionsForTest(t *testing.T, runtimeID string) (taskID string, instructions string, raw string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "squad-briefing-claim")
	req = withURLParam(req, "runtimeId", runtimeID)

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			ID    string `json:"id"`
			Agent *struct {
				Instructions string `json:"instructions"`
			} `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		return "", "", w.Body.String()
	}
	var instr string
	if resp.Task.Agent != nil {
		instr = resp.Task.Agent.Instructions
	}
	return resp.Task.ID, instr, w.Body.String()
}

// squadBriefingClaimFixture wires a runtime + leader agent + squad and returns
// the IDs needed to enqueue leader tasks against that runtime.
type squadBriefingClaimFixture struct {
	RuntimeID string
	AgentID   string // squad leader, has the runtime and empty instructions
	SquadID   string
	IssueID   string // assignee_type='agent' (NOT squad) — reproduces MUL-3724
}

func newSquadBriefingClaimFixture(t *testing.T, ctx context.Context, name string) squadBriefingClaimFixture {
	t.Helper()

	runtimeID := createClaimReclaimRuntime(t, ctx, name+" runtime")
	// Leader agent + an issue assigned to that agent (assignee_type='agent').
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" leader")
	// Force empty instructions so the test asserts the briefing alone — this
	// mirrors MUL-3724 where the leader's own instructions were blank.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET instructions = '' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("clear leader instructions: %v", err)
	}
	// Make the issue assignee an agent (NOT the squad). The pre-fix code only
	// injected the briefing when issue.assignee_type='squad', so this is the
	// exact gap the fix closes: a comment @squad-mention leader task running on
	// an agent-assigned issue.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("set issue agent assignee: %v", err)
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name+" squad", agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	return squadBriefingClaimFixture{
		RuntimeID: runtimeID,
		AgentID:   agentID,
		SquadID:   squadID,
		IssueID:   issueID,
	}
}

func enqueueClaimTask(t *testing.T, ctx context.Context, fx squadBriefingClaimFixture, isLeader bool, withSquadID bool) string {
	t.Helper()
	var taskID string
	var squadArg any
	if withSquadID {
		squadArg = fx.SquadID
	} else {
		squadArg = nil
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id)
		VALUES ($1, $2, $3, 'queued', 0, $4, $5)
		RETURNING id
	`, fx.AgentID, fx.RuntimeID, fx.IssueID, isLeader, squadArg).Scan(&taskID); err != nil {
		t.Fatalf("enqueue claim task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestClaim_LeaderTaskFromCommentMention_InjectsBriefing is the MUL-3724
// reproduction: a leader task (is_leader_task=true) carrying a squad_id, on an
// issue assigned to a plain AGENT (not the squad). The pre-fix gate
// (issue.assignee_type='squad') would NOT inject the briefing here, so the
// leader booted with no squad context and degraded into doing the work itself.
// After the fix the briefing is keyed off the task flag + squad_id, so it is
// injected regardless of issue assignee.
func TestClaim_LeaderTaskFromCommentMention_InjectsBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing inject")
	want := enqueueClaimTask(t, ctx, fx, true /*isLeader*/, true /*withSquadID*/)

	got, instr, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q: %s", got, want, raw)
	}
	if !strings.Contains(instr, "## Squad Operating Protocol") || !strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("expected squad-leader briefing in agent instructions, got:\n%s", instr)
	}
}

// TestClaim_NonLeaderTask_NoBriefing guards the negative: a task that is NOT a
// leader task (is_leader_task=false), even with a squad_id present, must not
// receive the briefing. This keeps worker/mention runs free of leader framing.
func TestClaim_NonLeaderTask_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing nonleader")
	enqueueClaimTask(t, ctx, fx, false /*isLeader*/, true /*withSquadID*/)

	_, instr, _ := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("non-leader task must NOT get squad briefing, got:\n%s", instr)
	}
}

// TestClaim_LeaderTaskWithDanglingSquadID_NoBriefing is the load-bearing
// contract for dropping the FK on agent_task_queue.squad_id (migration 127):
// when a squad is hard-deleted AFTER a leader task was enqueued, the task row
// keeps a now-dangling squad_id. The claim must still succeed (HTTP 200, task
// delivered) and simply skip briefing injection — GetSquadInWorkspace returns
// no row, so the err != nil guard makes this identical to "condition not
// matched". Never a 500, never a stale/empty briefing. Without the FK nothing
// in the DB prevents the dangling row, so this guard lives entirely in the
// claim handler and must stay tested.
func TestClaim_LeaderTaskWithDanglingSquadID_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing dangling")
	want := enqueueClaimTask(t, ctx, fx, true /*isLeader*/, true /*withSquadID*/)

	// Hard-delete the squad AFTER enqueue, leaving task.squad_id dangling.
	// There is no FK (migration 127), so the task row is untouched.
	if _, err := testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, fx.SquadID); err != nil {
		t.Fatalf("delete squad: %v", err)
	}
	// Confirm the task still carries the (now orphaned) squad_id — i.e. the
	// delete did not cascade/null it, which is the whole point of no-FK.
	var stillSet bool
	if err := testPool.QueryRow(ctx,
		`SELECT squad_id = $2 FROM agent_task_queue WHERE id = $1`, want, fx.SquadID,
	).Scan(&stillSet); err != nil {
		t.Fatalf("reload task squad_id: %v", err)
	}
	if !stillSet {
		t.Fatalf("expected task.squad_id to remain the dangling UUID after squad delete (no FK)")
	}

	got, instr, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q (claim must still succeed 200): %s", got, want, raw)
	}
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("dangling squad_id must NOT get squad briefing, got:\n%s", instr)
	}
}

// leader tasks enqueued before migration 127 (or by an old binary) have a NULL
// squad_id. The claim handler must skip injection rather than panic or guess —
// equivalent to the pre-fix "condition not matched" behavior, never a stale
// briefing.
func TestClaim_LeaderTaskWithoutSquadID_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing nullsquad")
	enqueueClaimTask(t, ctx, fx, true /*isLeader*/, false /*withSquadID*/)

	_, instr, _ := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("leader task with NULL squad_id must NOT get squad briefing, got:\n%s", instr)
	}
}
