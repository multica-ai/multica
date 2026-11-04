package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// digestEnv is a workspace of its own, so a test sees only the squads it makes
// and the digest's caps and totals are exact. testUserID is its owner.
type digestEnv struct {
	WorkspaceID string
	RuntimeID   string
	AgentID     string
}

func newDigestEnv(t *testing.T) digestEnv {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ws := dbfx.Workspace(t, "Digest "+uuid.NewString(), "digest-"+uuid.NewString(), testutil.Cols{"issue_prefix": "DIG"})
	dbfx.Member(t, ws, testUserID, "owner")
	runtimeID := dbfx.Runtime(t, "Digest runtime "+uuid.NewString(), testutil.Cols{"workspace_id": ws})
	agentID := dbfx.Agent(t, "Digest agent "+uuid.NewString(), runtimeID, testutil.Cols{"workspace_id": ws})
	return digestEnv{WorkspaceID: ws, RuntimeID: runtimeID, AgentID: agentID}
}

// squad inserts a seated squad (floor and slot set) unless over says otherwise.
func (e digestEnv) squad(t *testing.T, name string, over ...testutil.Cols) string {
	t.Helper()
	floorID := dbfx.Insert(t, "company_floor", testutil.Cols{"workspace_id": e.WorkspaceID, "name": "F " + uuid.NewString()[:8], "position": 0})
	cols := testutil.Cols{"workspace_id": e.WorkspaceID, "floor_id": floorID, "company_slot": 0}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	return dbfx.Squad(t, name, e.AgentID, cols)
}

// issue inserts an issue assigned to the squad.
func (e digestEnv) issue(t *testing.T, squadID, title, status string, over ...testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{
		"workspace_id":  e.WorkspaceID,
		"status":        status,
		"assignee_type": "squad",
		"assignee_id":   squadID,
	}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	return dbfx.Issue(t, title, cols)
}

func (e digestEnv) pendingProposal(t *testing.T, squadID, title string) string {
	t.Helper()
	issueID := dbfx.Issue(t, title, testutil.Cols{"workspace_id": e.WorkspaceID})
	return dbfx.Insert(t, "proposal", testutil.Cols{
		"workspace_id": e.WorkspaceID, "squad_id": squadID, "issue_id": issueID,
		"created_by_type": "agent", "created_by_id": e.AgentID,
	})
}

func (e digestEnv) failedRun(t *testing.T, squadID string, over ...testutil.Cols) {
	t.Helper()
	cols := testutil.Cols{"status": "failed", "completed_at": testutil.Raw("now()"), "runtime_id": e.RuntimeID}
	if squadID != "" {
		cols["squad_id"] = squadID
	}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	dbfx.Task(t, e.AgentID, cols)
}

func (e digestEnv) digest(t *testing.T, since time.Time) service.Digest {
	t.Helper()
	path := "/api/company/digest"
	if !since.IsZero() {
		path += "?since=" + since.UTC().Format(time.RFC3339)
	}
	req := newRequest("GET", path, nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	var out service.Digest
	testutil.Call(t, testHandler.GetCompanyDigest, req).Want(http.StatusOK).JSON(&out)
	return out
}

func findDigestSquad(t *testing.T, d service.Digest, id string) service.DigestSquad {
	t.Helper()
	for _, sq := range d.Squads {
		if sq.ID == id {
			return sq
		}
	}
	t.Fatalf("squad %s not reported in full; squads=%d quiet=%d", id, len(d.Squads), d.Quiet.Count)
	return service.DigestSquad{}
}

func TestDigest_SquadCountsAndRules(t *testing.T) {
	e := newDigestEnv(t)
	sq := e.squad(t, "Research")
	since := time.Now().Add(-24 * time.Hour)

	// Done: one inside the window, one before it, a cancelled one that is not
	// done, and a custom status in the done category.
	e.issue(t, sq, "shipped today", "done")
	e.issue(t, sq, "shipped last month", "done", testutil.Cols{"updated_at": testutil.Raw("now() - interval '30 days'")})
	e.issue(t, sq, "dropped", "cancelled")
	dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": e.WorkspaceID, "key": "shipped", "name": "Shipped", "category": "done", "color": "#000000",
	})
	e.issue(t, sq, "custom done", "shipped")

	// In flight: one fresh, one stale (no activity for more than 3 days), one
	// just under the line.
	e.issue(t, sq, "fresh", "in_progress", testutil.Cols{"last_activity_at": testutil.Raw("now() - interval '1 hour'")})
	e.issue(t, sq, "stale", "in_progress", testutil.Cols{"last_activity_at": testutil.Raw("now() - interval '4 days'")})
	e.issue(t, sq, "almost stale", "in_progress", testutil.Cols{"last_activity_at": testutil.Raw("now() - interval '71 hours'")})
	// A blocked issue is blocked, not in progress and not stale.
	e.issue(t, sq, "blocked", "blocked", testutil.Cols{"last_activity_at": testutil.Raw("now() - interval '9 days'")})

	e.failedRun(t, sq)
	e.failedRun(t, sq, testutil.Cols{"completed_at": testutil.Raw("now() - interval '5 days'")}) // before the window
	e.failedRun(t, "")                                                                           // no squad: workspace-level only

	// Leader no-action evaluations on the squad's issues count; other outcomes
	// and other issues do not.
	noActionIssue := e.issue(t, sq, "leader had nothing", "todo")
	for _, outcome := range []string{"no_action", "no_action", "delegated"} {
		dbfx.Insert(t, "activity_log", testutil.Cols{
			"workspace_id": e.WorkspaceID, "issue_id": noActionIssue, "actor_type": "agent", "actor_id": e.AgentID,
			"action": "squad_leader_evaluated", "details": testutil.Raw(fmt.Sprintf(`'{"outcome":%q}'::jsonb`, outcome)),
		})
	}

	p1 := e.pendingProposal(t, sq, "Pilot usage pricing")
	_ = e.pendingProposal(t, sq, "Open an EU company")
	dbfx.Exec(t, `UPDATE proposal SET state = 'approved' WHERE id = $1`, p1)

	got := findDigestSquad(t, e.digest(t, since), sq)
	if got.Done != 2 || got.InProgress != 3 || got.Blocked != 1 || got.Stale != 1 || got.FailedRuns != 1 || got.LeaderNoAction != 2 {
		t.Fatalf("counts wrong: %+v", got)
	}
	if got.PendingProposalCount != 1 || len(got.PendingProposals) != 1 || got.PendingProposals[0].Title != "Open an EU company" {
		t.Fatalf("only the pending proposal is listed: %+v", got.PendingProposals)
	}
	if len(got.RecentDone) != 2 {
		t.Fatalf("recent_done = %+v, want the two issues done since the window", got.RecentDone)
	}
	for _, ref := range got.RecentDone {
		if !strings.HasPrefix(ref.Mention, "[DIG-") || !strings.HasSuffix(ref.Mention, "](mention://issue/"+ref.ID+")") {
			t.Fatalf("mention link malformed: %+v", ref)
		}
	}
}

// TestDigest_SnoozedProposalIsListedOnlyWhenDue: a snoozed proposal is left out of
// the digest until its snooze date passes, while the pending cap still counts it
// (spec 2.4: a snoozed proposal "still counts towards the pending caps").
func TestDigest_SnoozedProposalIsListedOnlyWhenDue(t *testing.T) {
	e := newDigestEnv(t)
	sq := e.squad(t, "Snoozers")
	since := time.Now().Add(-24 * time.Hour)

	open := e.pendingProposal(t, sq, "Still open")
	snoozed := e.pendingProposal(t, sq, "Snoozed for later")
	dbfx.Exec(t, `UPDATE proposal SET snoozed_until = now() + interval '3 days' WHERE id = $1`, snoozed)

	got := findDigestSquad(t, e.digest(t, since), sq)
	if got.PendingProposalCount != 1 || len(got.PendingProposals) != 1 || got.PendingProposals[0].ID != open {
		t.Fatalf("before the snooze date only the open proposal is listed: count=%d list=%+v",
			got.PendingProposalCount, got.PendingProposals)
	}

	dbfx.Exec(t, `UPDATE proposal SET snoozed_until = now() - interval '1 hour' WHERE id = $1`, snoozed)
	got = findDigestSquad(t, e.digest(t, since), sq)
	if got.PendingProposalCount != 2 || len(got.PendingProposals) != 2 {
		t.Fatalf("after the snooze date both proposals are listed: count=%d list=%+v",
			got.PendingProposalCount, got.PendingProposals)
	}

	dbfx.Exec(t, `UPDATE proposal SET snoozed_until = now() + interval '3 days' WHERE id = $1`, snoozed)
	capped, err := testHandler.Queries.CountPendingProposalsBySquad(context.Background(), db.CountPendingProposalsBySquadParams{
		WorkspaceID: parseUUID(e.WorkspaceID), SquadID: parseUUID(sq),
	})
	if err != nil {
		t.Fatalf("count pending proposals for the cap: %v", err)
	}
	if capped != 2 {
		t.Fatalf("the pending cap counts the snoozed proposal too: got %d, want 2", capped)
	}
}

func TestDigest_RecentDoneIsCappedAtFiveNewestFirst(t *testing.T) {
	e := newDigestEnv(t)
	sq := e.squad(t, "Shippers")
	for i := 0; i < 7; i++ {
		e.issue(t, sq, fmt.Sprintf("done %d", i), "done", testutil.Cols{"updated_at": testutil.Raw(fmt.Sprintf("now() - interval '%d minutes'", i+1))})
	}
	got := findDigestSquad(t, e.digest(t, time.Now().Add(-time.Hour)), sq)
	if got.Done != 7 || len(got.RecentDone) != 5 {
		t.Fatalf("done=%d recent=%d, want 7 and 5", got.Done, len(got.RecentDone))
	}
	if got.RecentDone[0].Title != "done 0" || got.RecentDone[4].Title != "done 4" {
		t.Fatalf("recent_done must be newest first: %+v", got.RecentDone)
	}
}

func TestDigest_OrdersByWhatNeedsAttentionFirst(t *testing.T) {
	e := newDigestEnv(t)
	busy := e.squad(t, "A busy")
	stale := e.squad(t, "B stale")
	failed := e.squad(t, "C failed")
	blocked := e.squad(t, "D blocked")
	proposing := e.squad(t, "E proposing")

	e.issue(t, busy, "done", "done")
	e.issue(t, stale, "stale", "in_progress", testutil.Cols{"last_activity_at": testutil.Raw("now() - interval '5 days'")})
	e.failedRun(t, failed)
	e.issue(t, blocked, "blocked", "blocked")
	e.pendingProposal(t, proposing, "Decide me")

	d := e.digest(t, time.Now().Add(-time.Hour))
	var names []string
	for _, sq := range d.Squads {
		names = append(names, sq.Name)
	}
	want := "E proposing,D blocked,C failed,B stale,A busy"
	if strings.Join(names, ",") != want {
		t.Fatalf("order = %v, want %s", names, want)
	}
}

func TestDigest_QuietSquadsCollapseToOneList(t *testing.T) {
	e := newDigestEnv(t)
	loud := e.squad(t, "Loud")
	idle := e.squad(t, "Idle")
	steady := e.squad(t, "Steady")
	e.issue(t, loud, "blocked", "blocked")
	// Nothing new, but work in flight: still quiet, with its count.
	e.issue(t, steady, "working", "in_progress", testutil.Cols{"last_activity_at": testutil.Raw("now()")})

	d := e.digest(t, time.Now().Add(-time.Hour))
	if len(d.Squads) != 1 || d.Squads[0].ID != loud {
		t.Fatalf("only the loud squad is reported in full: %+v", d.Squads)
	}
	if d.Quiet.Count != 2 || len(d.Quiet.Squads) != 2 {
		t.Fatalf("quiet = %+v, want the two quiet squads", d.Quiet)
	}
	byID := map[string]int{}
	for _, q := range d.Quiet.Squads {
		byID[q.ID] = q.InProgress
	}
	if _, ok := byID[idle]; !ok || byID[steady] != 1 {
		t.Fatalf("quiet entries wrong: %+v", d.Quiet.Squads)
	}
}

func TestDigest_CapsSquadsInFullAndCountsTheRest(t *testing.T) {
	e := newDigestEnv(t)
	const total = service.DigestMaxSquads + 3
	var first, second string
	for i := 0; i < total; i++ {
		id := e.squad(t, fmt.Sprintf("cap-%02d", i))
		e.issue(t, id, "blocked", "blocked")
		switch i {
		case 0:
			first = id
		case 1:
			second = id
		}
	}
	// Two squads rank above the rest, whatever their names.
	last := e.squad(t, "cap-zz")
	e.issue(t, last, "blocked", "blocked")
	e.pendingProposal(t, last, "Decide me")

	d := e.digest(t, time.Now().Add(-time.Hour))
	if len(d.Squads) != service.DigestMaxSquads || d.OmittedSquadCount != total+1-service.DigestMaxSquads {
		t.Fatalf("squads=%d omitted=%d, want %d and %d", len(d.Squads), d.OmittedSquadCount, service.DigestMaxSquads, total+1-service.DigestMaxSquads)
	}
	if d.Squads[0].ID != last {
		t.Fatalf("the squad with a pending proposal ranks first, got %s", d.Squads[0].Name)
	}
	// The cut squads are the lowest ranked: ties fall back to name order.
	if d.Squads[1].ID != first || d.Squads[2].ID != second {
		t.Fatalf("ranking inside a tie must be by name: %s, %s", d.Squads[1].Name, d.Squads[2].Name)
	}
	if got := d.Squads[len(d.Squads)-1].Name; got != fmt.Sprintf("cap-%02d", service.DigestMaxSquads-2) {
		t.Fatalf("last squad kept = %s", got)
	}
	if d.Quiet.Count != 0 {
		t.Fatalf("no squad is quiet: %+v", d.Quiet)
	}
}

func TestDigest_WorkspaceLevel(t *testing.T) {
	e := newDigestEnv(t)
	sq := e.squad(t, "Ops")
	unseated := e.squad(t, "Unseated", testutil.Cols{"floor_id": nil, "company_slot": nil})
	archived := e.squad(t, "Gone", testutil.Cols{"floor_id": nil, "company_slot": nil, "archived_at": testutil.Raw("now()")})
	_ = archived

	e.failedRun(t, sq)
	e.failedRun(t, sq)
	dbfx.Task(t, e.AgentID, testutil.Cols{"status": "completed", "completed_at": testutil.Raw("now()"), "runtime_id": e.RuntimeID})

	dbfx.Runtime(t, "Laptop", testutil.Cols{"workspace_id": e.WorkspaceID, "status": "offline"})

	dbfx.InsertNoID(t, "task_usage_hourly", testutil.Cols{
		"bucket_hour": testutil.Raw("date_trunc('hour', now())"), "workspace_id": e.WorkspaceID,
		"runtime_id": e.RuntimeID, "agent_id": e.AgentID, "provider": "claude", "model": "m",
		"input_tokens": 1000, "output_tokens": 500, "cost_usd_ticks": int64(25_000_000_000), // $2.50
	}, "workspace_id = $1 AND bucket_hour = date_trunc('hour', now())", e.WorkspaceID)

	d := e.digest(t, time.Now().Add(-2*time.Hour))
	ws := d.Workspace
	if ws.Runs != 3 || ws.FailedRuns != 2 {
		t.Fatalf("runs=%d failed=%d, want 3 and 2", ws.Runs, ws.FailedRuns)
	}
	if ws.InputTokens != 1000 || ws.OutputTokens != 500 || ws.ReportedCostUSD != 2.5 {
		t.Fatalf("usage wrong: %+v", ws)
	}
	if ws.OfflineRuntimes.Count != 1 || ws.OfflineRuntimes.Items[0].Name != "Laptop" {
		t.Fatalf("offline runtimes: %+v", ws.OfflineRuntimes)
	}
	if ws.AgentsFailed.Count != 1 || ws.AgentsFailed.Items[0].ID != e.AgentID || ws.AgentsFailed.Items[0].FailedRuns != 2 {
		t.Fatalf("agents with failed runs: %+v", ws.AgentsFailed)
	}
	if ws.UnseatedSquads.Count != 1 || ws.UnseatedSquads.Items[0].ID != unseated {
		t.Fatalf("unseated squads (archived ones do not count): %+v", ws.UnseatedSquads)
	}
}

func TestDigest_IsolatedPerWorkspace(t *testing.T) {
	mine := newDigestEnv(t)
	other := newDigestEnv(t)
	otherSquad := other.squad(t, "Their squad")
	other.issue(t, otherSquad, "their blocker", "blocked")
	other.failedRun(t, otherSquad)
	other.pendingProposal(t, otherSquad, "Their proposal")
	dbfx.Runtime(t, "Their offline runtime", testutil.Cols{"workspace_id": other.WorkspaceID, "status": "offline"})

	d := mine.digest(t, time.Now().Add(-time.Hour))
	if len(d.Squads) != 0 || d.Quiet.Count != 0 {
		t.Fatalf("another workspace's squads leaked: %+v", d)
	}
	ws := d.Workspace
	if ws.Runs != 0 || ws.FailedRuns != 0 || ws.OfflineRuntimes.Count != 0 || ws.AgentsFailed.Count != 0 || ws.UnseatedSquads.Count != 0 {
		t.Fatalf("another workspace's facts leaked: %+v", ws)
	}
}

func TestDigest_AccessAndSinceValidation(t *testing.T) {
	e := newDigestEnv(t)

	// A user who is not a member of the workspace is refused.
	stranger := dbfx.User(t, "Stranger", fmt.Sprintf("digest-stranger-%d@example.com", time.Now().UnixNano()))
	req := asUser(newRequest("GET", "/api/company/digest", nil), stranger)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	testutil.Call(t, testHandler.GetCompanyDigest, req).WantOneOf(http.StatusForbidden, http.StatusNotFound)

	// Any member, not only owners and admins, may read it.
	member := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")
	req = asUser(newRequest("GET", "/api/company/digest", nil), member)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	testutil.Call(t, testHandler.GetCompanyDigest, req).Want(http.StatusOK)

	for name, since := range map[string]string{
		"not a timestamp": "yesterday",
		"future":          time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"too old":         time.Now().Add(-service.DigestMaxWindow - time.Hour).UTC().Format(time.RFC3339),
	} {
		req := newRequest("GET", "/api/company/digest?since="+since, nil)
		req.Header.Set("X-Workspace-ID", e.WorkspaceID)
		if got := testutil.Call(t, testHandler.GetCompanyDigest, req); got.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", name, got.Code)
		}
	}
}

func newWorkspaceMemberUserIn(t *testing.T, workspaceID, role string) string {
	t.Helper()
	uid := dbfx.User(t, "Digest "+role, fmt.Sprintf("digest-%s-%d@example.com", role, time.Now().UnixNano()))
	dbfx.Member(t, workspaceID, uid, role)
	return uid
}
