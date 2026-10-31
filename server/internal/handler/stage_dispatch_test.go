package handler

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// stageRig is a parent issue with staged sub-issues and a squad led by an agent.
type stageRig struct {
	t      *testing.T
	parent string
	squad  string
	leader string
	owner  string // the parent's own assignee, woken by child_done
	kids   map[string]string
}

func newStageRig(t *testing.T, flagOn bool) *stageRig {
	t.Helper()
	return newStageRigWith(t, flagOn, nil)
}

// newStageRigWith builds the rig with the given sub-issues instead of the
// default ones.
func newStageRigWith(t *testing.T, flagOn bool, kids map[string]testutil.Cols) *stageRig {
	t.Helper()
	withFeatureFlag(t, testHandler, featureflags.StageDispatchV1, flagOn)
	r := &stageRig{t: t, kids: map[string]string{}}
	r.leader = dbfx.Agent(t, "stage leader", testRuntimeID)
	r.owner = dbfx.Agent(t, "parent owner", testRuntimeID)
	r.squad = dbfx.Insert(t, "squad", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": "stage squad " + r.leader, "description": "",
		"leader_id": r.leader, "creator_id": testUserID,
	})
	r.parent = dbfx.Issue(t, "parent", testutil.Cols{"assignee_type": "agent", "assignee_id": r.owner, "status": "in_progress"})
	if kids == nil {
		kids = map[string]testutil.Cols{
			"s1":    {"stage": 1},
			"s2":    {"stage": 2, "status": "backlog"},
			"s3":    {"stage": 3},
			"loose": {},
		}
	}
	for name, cols := range kids {
		cols["parent_issue_id"] = r.parent
		r.kids[name] = dbfx.Issue(t, "stage "+name, cols)
	}
	ids := []string{r.parent}
	for _, id := range r.kids {
		ids = append(ids, id)
	}
	for _, id := range ids {
		id := id
		t.Cleanup(func() { cleanupChildDoneIssue(id) })
	}
	dbfx.Cleanup(t, "DELETE FROM squad WHERE id=$1", r.squad)
	return r
}

func (r *stageRig) set(body map[string]any) *testutil.Response {
	return testutil.Call(r.t, testHandler.SetIssueStageDispatch, withURLParam(newRequest(http.MethodPut, "/", body), "id", r.parent))
}

func (r *stageRig) sweep() {
	r.t.Helper()
	if err := testHandler.StageDispatchService().Sweep(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

func (r *stageRig) assignee(name string) (assigneeType, assigneeID, status string) {
	dbfx.QueryRow(r.t, "SELECT COALESCE(assignee_type,''), COALESCE(assignee_id::text,''), status FROM issue WHERE id=$1", r.kids[name]).
		Scan(&assigneeType, &assigneeID, &status)
	return
}

func (r *stageRig) onSquad(name string) bool {
	typ, id, _ := r.assignee(name)
	return typ == "squad" && id == r.squad
}

func (r *stageRig) runs(agent, issue string) int {
	return dbfx.Count(r.t, "SELECT count(*) FROM agent_task_queue WHERE agent_id=$1 AND issue_id=$2", agent, issue)
}

func (r *stageRig) hasDispatch() bool {
	return dbfx.Count(r.t, "SELECT count(*) FROM issue_stage_dispatch WHERE issue_id=$1", r.parent) == 1
}

func TestStageDispatchAssignsStagesInOrder(t *testing.T) {
	r := newStageRig(t, true)
	var resp StageDispatchResponse
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK).JSON(&resp)
	if resp.SquadID == nil || *resp.SquadID != r.squad || resp.DispatchBy == nil || *resp.DispatchBy != testUserID {
		t.Fatalf("response %+v", resp)
	}
	// Stage 1 goes to the squad and its leader starts; the later stages and
	// the unstaged sub-issue stay as they are.
	if !r.onSquad("s1") || r.runs(r.leader, r.kids["s1"]) != 1 {
		t.Fatalf("stage 1 not dispatched: %v, leader runs %d", r.onSquad("s1"), r.runs(r.leader, r.kids["s1"]))
	}
	for _, name := range []string{"s2", "s3", "loose"} {
		if typ, _, _ := r.assignee(name); typ != "" {
			t.Fatalf("%s was assigned (%s) before its turn", name, typ)
		}
	}

	// Closing stage 1 opens stage 2 at once; backlog moves to todo so work
	// starts.
	updateChildStatus(t, r.kids["s1"], "done")
	if !r.onSquad("s2") {
		t.Fatal("stage 2 was not dispatched after stage 1 closed")
	}
	if _, _, status := r.assignee("s2"); status != "todo" {
		t.Fatalf("stage 2 status %q, want todo", status)
	}
	if r.onSquad("s3") {
		t.Fatal("dispatch ran ahead of the open stage")
	}
	// The parent's own assignee is not woken for a stage the server dispatched.
	if n := r.runs(r.owner, r.parent); n != 0 {
		t.Fatalf("parent owner woken %d times by a dispatched stage", n)
	}

	// A change that records no sub-issue event is caught by the sweep.
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", r.kids["s2"])
	r.sweep()
	if !r.onSquad("s3") {
		t.Fatal("the sweep did not open stage 3")
	}

	// Off: nothing further is assigned.
	r.set(map[string]any{"squad_id": nil}).Want(http.StatusOK).JSON(&resp)
	if resp.SquadID != nil {
		t.Fatalf("still on: %+v", resp)
	}
	dbfx.Exec(t, "UPDATE issue SET assignee_type=NULL, assignee_id=NULL WHERE id=$1", r.kids["s3"])
	r.sweep()
	if r.onSquad("s3") {
		t.Fatal("dispatch continued after it was turned off")
	}
}

func TestStageDispatchLeavesAssignedWorkAndNeedsAMember(t *testing.T) {
	r := newStageRig(t, true)
	other := dbfx.Agent(t, "someone else", testRuntimeID)
	dbfx.Exec(t, "UPDATE issue SET assignee_type='agent', assignee_id=$2 WHERE id=$1", r.kids["s1"], other)
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
	if typ, id, _ := r.assignee("s1"); typ != "agent" || id != other {
		t.Fatalf("an assigned sub-issue was taken over: %s %s", typ, id)
	}

	agentReq := withURLParam(newRequest(http.MethodPut, "/", map[string]any{"squad_id": r.squad}), "id", r.parent)
	agentReq.Header.Set("X-Agent-ID", r.leader)
	agentReq.Header.Set("X-Task-ID", dbfx.Insert(t, "agent_task_queue", testutil.Cols{
		"agent_id": r.leader, "runtime_id": testRuntimeID, "issue_id": r.parent, "status": "running",
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	}))
	agentReq.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, testHandler.SetIssueStageDispatch, agentReq).Want(http.StatusForbidden)

	// An agent's run cannot switch a member's dispatch off either.
	offReq := withURLParam(newRequest(http.MethodPut, "/", map[string]any{"squad_id": nil}), "id", r.parent)
	offReq.Header = agentReq.Header.Clone()
	testutil.Call(t, testHandler.SetIssueStageDispatch, offReq).Want(http.StatusForbidden)
	if !r.hasDispatch() {
		t.Fatal("an agent switched dispatch off")
	}

	r.set(map[string]any{"squad_id": "00000000-0000-0000-0000-000000000000"}).Want(http.StatusBadRequest)
}

// Without stage dispatch the closed stage wakes the parent's assignee as
// before: the silence above belongs to dispatch, not to a quiet rule.
func TestClosedStageStillWakesTheParentOwnerWithoutDispatch(t *testing.T) {
	r := newStageRig(t, true)
	updateChildStatus(t, r.kids["s1"], "done")
	if n := r.runs(r.owner, r.parent); n != 1 {
		t.Fatalf("parent owner runs = %d, want 1", n)
	}
	if r.onSquad("s2") {
		t.Fatal("a stage was dispatched without stage dispatch")
	}
}

// With the flag off the endpoint is absent, and a parent that had dispatch
// on behaves as before: no assignment, and the closed stage wakes its owner.
func TestStageDispatchFlagOff(t *testing.T) {
	r := newStageRig(t, false)
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusNotFound)
	dbfx.Exec(t, "INSERT INTO issue_stage_dispatch (issue_id, workspace_id, squad_id, dispatch_by) VALUES ($1, $2, $3, $4)",
		r.parent, testWorkspaceID, r.squad, testUserID)
	r.sweep()
	updateChildStatus(t, r.kids["s1"], "done")
	if r.onSquad("s1") || r.onSquad("s2") {
		t.Fatal("dispatch ran with the flag off")
	}
	if n := r.runs(r.owner, r.parent); n != 1 {
		t.Fatalf("parent owner runs = %d, want 1", n)
	}
}

// A stage that closes with cancelled work does not open the next one: the
// parent's assignee is woken to decide, and dispatch resumes once the
// cancelled sub-issue leaves the stage.
func TestCancelledStageHaltsDispatchAndWakesTheParentOwner(t *testing.T) {
	r := newStageRig(t, true)
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
	updateChildStatus(t, r.kids["s1"], "cancelled")
	if r.onSquad("s2") {
		t.Fatal("stage 2 was dispatched after stage 1 was cancelled")
	}
	if n := r.runs(r.owner, r.parent); n != 1 {
		t.Fatalf("parent owner runs = %d, want 1", n)
	}

	dbfx.Exec(t, "UPDATE issue SET stage=NULL WHERE id=$1", r.kids["s1"])
	r.sweep()
	if !r.onSquad("s2") {
		t.Fatal("dispatch did not resume once the cancelled sub-issue left the stage")
	}
}

// A sub-issue assigned ahead of time to a later stage waits in backlog, where
// its run would never start: opening its stage moves it to todo and starts the
// run, and that run answers the closed stage, so the parent's owner is not
// woken as well.
func TestStageDispatchStartsPreassignedBacklogOfALaterStage(t *testing.T) {
	exec := dbfx.Agent(t, "pre-assigned executor", testRuntimeID)
	r := newStageRigWith(t, true, map[string]testutil.Cols{
		"s1":  {"stage": 1},
		"pre": {"stage": 2, "status": "backlog", "assignee_type": "agent", "assignee_id": exec},
	})
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
	if _, _, status := r.assignee("pre"); status != "backlog" {
		t.Fatalf("stage 2 sub-issue moved to %q before its stage opened", status)
	}

	updateChildStatus(t, r.kids["s1"], "done")
	typ, id, status := r.assignee("pre")
	if typ != "agent" || id != exec || status != "todo" {
		t.Fatalf("pre-assigned sub-issue is %s %s %s, want agent %s todo", typ, id, status, exec)
	}
	if n := r.runs(exec, r.kids["pre"]); n != 1 {
		t.Fatalf("executor runs = %d, want 1", n)
	}
	if n := r.runs(r.owner, r.parent); n != 0 {
		t.Fatalf("parent owner woken %d times though the stage was started", n)
	}
}

func (r *stageRig) update(name string, body map[string]any) {
	r.t.Helper()
	testutil.Call(r.t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/", body), "id", r.kids[name])).Want(http.StatusOK)
}

// Assigning a sub-issue of a later stage, or moving it out of backlog, starts
// no run while an earlier stage is open: the stage opening does, and that run
// answers the closed stage, so the parent's owner is not woken as well.
func TestStageDispatchHoldsAssignedWorkUntilItsStageOpens(t *testing.T) {
	exec := dbfx.Agent(t, "pre-assigned executor", testRuntimeID)
	r := newStageRigWith(t, true, map[string]testutil.Cols{
		"s1":       {"stage": 1},
		"assigned": {"stage": 2, "status": "todo"},
		"parked":   {"stage": 2, "status": "backlog", "assignee_type": "agent", "assignee_id": exec},
	})
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)

	r.update("assigned", map[string]any{"assignee_type": "agent", "assignee_id": exec})
	r.update("parked", map[string]any{"status": "todo"})
	for _, name := range []string{"assigned", "parked"} {
		if n := r.runs(exec, r.kids[name]); n != 0 {
			t.Fatalf("%s started %d runs while stage 1 was open", name, n)
		}
	}

	updateChildStatus(t, r.kids["s1"], "done")
	for _, name := range []string{"assigned", "parked"} {
		if typ, id, status := r.assignee(name); typ != "agent" || id != exec || status != "todo" {
			t.Fatalf("%s is %s %s %s, want agent %s todo", name, typ, id, status, exec)
		}
		if n := r.runs(exec, r.kids[name]); n != 1 {
			t.Fatalf("%s runs = %d after its stage opened, want 1", name, n)
		}
	}
	if n := r.runs(r.owner, r.parent); n != 0 {
		t.Fatalf("parent owner woken %d times though the stage was started", n)
	}
}

// A sub-issue assigned ahead of time that already had its run has nothing to
// start when its stage opens: the closed stage still wakes the parent's owner,
// who would otherwise never hear of it.
func TestStageDispatchWakesTheOwnerWhenItStartsNothing(t *testing.T) {
	exec := dbfx.Agent(t, "pre-assigned executor", testRuntimeID)
	r := newStageRigWith(t, true, map[string]testutil.Cols{
		"s1":  {"stage": 1},
		"pre": {"stage": 2, "status": "in_progress", "assignee_type": "agent", "assignee_id": exec},
	})
	dbfx.Task(t, exec, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": r.kids["pre"], "status": "completed"})
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)

	updateChildStatus(t, r.kids["s1"], "done")
	if n := r.runs(exec, r.kids["pre"]); n != 1 {
		t.Fatalf("dispatch started a run for work already under way (%d tasks)", n)
	}
	if n := r.runs(r.owner, r.parent); n != 1 {
		t.Fatalf("parent owner runs = %d, want 1", n)
	}
}

// The assignment is recorded the way a person's is: the issue:updated event
// carries assignee_changed and the previous assignee, and a move out of
// backlog carries status_changed, which the timeline and notifications read.
func TestStageDispatchPublishesTheUpdateEvent(t *testing.T) {
	r := newStageRig(t, true)
	type update struct {
		assigneeChanged, statusChanged bool
		prevAssigneeID                 *string
		prevStatus, actorType, actorID string
	}
	var mu sync.Mutex
	seen := map[string]update{}
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		p, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		resp, ok := p["issue"].(IssueResponse)
		if !ok {
			return
		}
		u := update{actorType: e.ActorType, actorID: e.ActorID}
		u.assigneeChanged, _ = p["assignee_changed"].(bool)
		u.statusChanged, _ = p["status_changed"].(bool)
		u.prevAssigneeID, _ = p["prev_assignee_id"].(*string)
		u.prevStatus, _ = p["prev_status"].(string)
		mu.Lock()
		seen[resp.ID] = u
		mu.Unlock()
	})
	get := func(name string) update {
		mu.Lock()
		defer mu.Unlock()
		return seen[r.kids[name]]
	}

	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
	if u := get("s1"); !u.assigneeChanged || u.prevAssigneeID != nil || u.actorType != "member" || u.actorID != testUserID {
		t.Fatalf("stage 1 event %+v", u)
	}
	updateChildStatus(t, r.kids["s1"], "done")
	if u := get("s2"); !u.assigneeChanged || !u.statusChanged || u.prevStatus != "backlog" {
		t.Fatalf("stage 2 event %+v", u)
	}
}

// A squad archived after dispatch was turned on takes nothing more.
func TestStageDispatchSkipsAnArchivedSquad(t *testing.T) {
	r := newStageRig(t, true)
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
	dbfx.Exec(t, "UPDATE squad SET archived_at=now() WHERE id=$1", r.squad)
	updateChildStatus(t, r.kids["s1"], "done")
	if typ, _, _ := r.assignee("s2"); typ != "" {
		t.Fatalf("stage 2 went to an archived squad (%s)", typ)
	}
}

// Without foreign keys the rows go with what they hang on: a closed parent,
// a deleted issue, an archived squad and a removed member.
func TestStageDispatchRowsFollowTheirOwners(t *testing.T) {
	t.Run("closed parent", func(t *testing.T) {
		r := newStageRig(t, true)
		r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
		dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", r.parent)
		r.sweep()
		if r.hasDispatch() {
			t.Fatal("a closed parent kept its row")
		}
	})
	t.Run("deleted issue", func(t *testing.T) {
		r := newStageRig(t, true)
		r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
		testutil.Call(t, testHandler.DeleteIssue, withURLParam(newRequest(http.MethodDelete, "/", nil), "id", r.parent)).Want(http.StatusNoContent)
		if r.hasDispatch() {
			t.Fatal("a deleted issue kept its row")
		}
	})
	t.Run("archived squad", func(t *testing.T) {
		r := newStageRig(t, true)
		r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
		testutil.Call(t, testHandler.DeleteSquad, squadScopeReq(testUserID, http.MethodDelete, "/api/squads", nil, map[string]string{"id": r.squad})).Want(http.StatusNoContent)
		if r.hasDispatch() {
			t.Fatal("an archived squad kept its row")
		}
	})
	t.Run("removed member", func(t *testing.T) {
		r := newStageRig(t, true)
		other := dbfx.User(t, "dispatcher", "stage-dispatcher-"+r.squad+"@multica.ai")
		memberID := dbfx.Member(t, testWorkspaceID, other, "member")
		dbfx.Exec(t, "INSERT INTO issue_stage_dispatch (issue_id, workspace_id, squad_id, dispatch_by) VALUES ($1, $2, $3, $4)",
			r.parent, testWorkspaceID, r.squad, other)
		if _, err := testHandler.revokeAndRemoveMember(context.Background(), parseUUID(testWorkspaceID), parseUUID(other), parseUUID(memberID), parseUUID(testUserID)); err != nil {
			t.Fatal(err)
		}
		if r.hasDispatch() {
			t.Fatal("a removed member kept their row")
		}
	})
}

// Dispatch halted by cancelled work leaves the way open for a person: a
// sub-issue of the next stage that they assign or move to todo runs at once,
// since no pass will start it.
func TestStageDispatchHaltedByCancellationDoesNotHoldManualWork(t *testing.T) {
	exec := dbfx.Agent(t, "manual executor", testRuntimeID)
	r := newStageRigWith(t, true, map[string]testutil.Cols{
		"s1": {"stage": 1},
		"s2": {"stage": 2, "status": "todo"},
	})
	r.set(map[string]any{"squad_id": r.squad}).Want(http.StatusOK)
	updateChildStatus(t, r.kids["s1"], "cancelled")
	if r.onSquad("s2") {
		t.Fatal("stage 2 was dispatched after stage 1 was cancelled")
	}

	r.update("s2", map[string]any{"assignee_type": "agent", "assignee_id": exec})
	if n := r.runs(exec, r.kids["s2"]); n != 1 {
		t.Fatalf("manual assignment after a halt started %d runs, want 1", n)
	}
}
