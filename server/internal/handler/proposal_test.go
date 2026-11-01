package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// proposalTestFixture is a squad with a leader agent that is running a task,
// and a title prefix every row a test creates carries so cleanup finds it.
type proposalTestFixture struct {
	SquadID      string
	LeaderID     string
	LeaderName   string
	LeaderTaskID string
	Prefix       string
}

func newProposalTestFixture(t *testing.T) proposalTestFixture {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fx := proposalTestFixture{Prefix: "PROPTEST " + suffix, LeaderName: "Prop Leader " + suffix}
	fx.LeaderID = createHandlerTestAgent(t, fx.LeaderName, nil)
	fx.SquadID = dbfx.Squad(t, "Prop Squad "+suffix, fx.LeaderID)
	dbfx.SquadMember(t, fx.SquadID, "agent", fx.LeaderID, testutil.Cols{"role": "leader"})
	fx.LeaderTaskID = createHandlerTestTaskForAgent(t, fx.LeaderID)
	registerProposalCleanup(t, fx.Prefix, fx.SquadID)
	return fx
}

// registerProposalCleanup removes what the API created for a test. It is
// registered after the fixture rows, so it runs before they are deleted.
func registerProposalCleanup(t *testing.T, prefix string, squadIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND title LIKE $2)`, testWorkspaceID, prefix+"%")
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND title LIKE $2`, testWorkspaceID, prefix+"%")
		testPool.Exec(ctx, `DELETE FROM project WHERE workspace_id = $1 AND title LIKE $2`, testWorkspaceID, prefix+"%")
		testPool.Exec(ctx, `DELETE FROM proposal WHERE workspace_id = $1 AND squad_id = ANY($2::uuid[])`, testWorkspaceID, squadIDs)
	})
}

func (fx proposalTestFixture) title(name string) string { return fx.Prefix + " " + name }

func proposalBody(squadID, title string, over map[string]any) map[string]any {
	body := map[string]any{
		"squad_id":       squadID,
		"title":          title,
		"summary":        "Competitors shipped usage-based pricing.",
		"evidence":       []string{"https://example.com/pricing-teardown"},
		"recommendation": "Pilot usage-based pricing for new accounts.",
		"options":        []string{"Keep seat pricing", "Pilot usage pricing"},
		"cost_risk":      "Two weeks of engineering; churn risk on the pilot cohort.",
		"ask":            "Approve the pilot.",
	}
	for k, v := range over {
		body[k] = v
	}
	return body
}

func asAgent(req *http.Request, agentID, taskID string) *http.Request {
	return testutil.WithHeaders(req, "X-Agent-ID", agentID, "X-Task-ID", taskID)
}

func asUser(req *http.Request, userID string) *http.Request {
	return testutil.WithHeaders(req, "X-User-ID", userID)
}

// fileProposal files a proposal as the fixture's leader agent.
func (fx proposalTestFixture) fileProposal(t *testing.T, name string, over map[string]any) ProposalResponse {
	t.Helper()
	req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title(name), over)), fx.LeaderID, fx.LeaderTaskID)
	var out ProposalResponse
	testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusCreated).JSON(&out)
	return out
}

func decideRequest(proposalID string, body map[string]any) *http.Request {
	req := newRequest("POST", "/api/proposals/"+proposalID+"/decision", body)
	return withURLParam(req, "id", proposalID)
}

func getRequest(path, id string) *http.Request {
	return withURLParam(newRequest("GET", path, nil), "id", id)
}

// seedProposalRow inserts a bare proposal row (no backing issue) for the limit
// tests, which only count rows.
func seedProposalRow(t *testing.T, squadID, state string, urgent bool, age time.Duration) string {
	t.Helper()
	return dbfx.Insert(t, "proposal", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"squad_id":        squadID,
		"issue_id":        testutil.Raw("gen_random_uuid()"),
		"urgent":          urgent,
		"state":           state,
		"created_by_type": "agent",
		"created_by_id":   testutil.Raw("gen_random_uuid()"),
		"created_at":      testutil.Raw(fmt.Sprintf("now() - interval '%d seconds'", int(age.Seconds()))),
	})
}

func newWorkspaceMemberUser(t *testing.T, role string) string {
	t.Helper()
	uid := dbfx.User(t, "Prop "+role, fmt.Sprintf("prop-%s-%d@example.com", role, time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, uid, role)
	return uid
}

func TestCreateProposal_ShapeAndNoNotification(t *testing.T) {
	fx := newProposalTestFixture(t)
	resp := fx.fileProposal(t, "shape", nil)

	if resp.State != "pending" || resp.Urgent || resp.UrgentReason != "" {
		t.Fatalf("unexpected proposal: %+v", resp)
	}
	if resp.SquadID != fx.SquadID || resp.CreatedByType != "agent" || resp.CreatedByID != fx.LeaderID {
		t.Fatalf("proposal creator/squad wrong: %+v", resp)
	}
	if resp.HandoffSquadID != nil || resp.DecidedBy != nil || resp.DecidedAt != nil {
		t.Fatalf("a new proposal carries no handoff or decision: %+v", resp)
	}

	var status, originType, originID, creatorType, creatorID, description string
	var assigneeType, assigneeID *string
	dbfx.QueryRow(t, `
		SELECT status, origin_type, origin_id::text, creator_type, creator_id::text, description, assignee_type, assignee_id::text
		FROM issue WHERE id = $1`, resp.IssueID,
	).Scan(&status, &originType, &originID, &creatorType, &creatorID, &description, &assigneeType, &assigneeID)

	if status != "in_review" {
		t.Errorf("backing issue status = %q, want in_review", status)
	}
	if originType != "proposal" || originID != resp.ID {
		t.Errorf("backing issue origin = %s/%s, want proposal/%s", originType, originID, resp.ID)
	}
	if creatorType != "agent" || creatorID != fx.LeaderID {
		t.Errorf("backing issue creator = %s/%s, want agent/%s", creatorType, creatorID, fx.LeaderID)
	}
	if assigneeType != nil || assigneeID != nil {
		t.Errorf("a non-urgent proposal issue must be unassigned, got %v/%v", assigneeType, assigneeID)
	}

	// The fixed shape, in order.
	last := -1
	for _, heading := range []string{"## Summary", "## Evidence", "## Recommendation", "## Options considered", "## Cost and risk", "## Ask"} {
		idx := strings.Index(description, heading)
		if idx <= last {
			t.Fatalf("description missing %q after position %d:\n%s", heading, last, description)
		}
		last = idx
	}
	if !strings.Contains(description, "- https://example.com/pricing-teardown") || !strings.Contains(description, "- Pilot usage pricing") {
		t.Errorf("description should list evidence and options:\n%s", description)
	}

	// Unassigned means no run for anyone.
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, resp.IssueID); n != 0 {
		t.Errorf("a proposal issue must not enqueue a run, got %d tasks", n)
	}

	// The response is readable back by id and in the list.
	var got ProposalResponse
	testutil.Call(t, testHandler.GetProposal, getRequest("/api/proposals/"+resp.ID, resp.ID)).Want(http.StatusOK).JSON(&got)
	if got.ID != resp.ID || got.IssueID != resp.IssueID || got.Title != fx.title("shape") {
		t.Errorf("GET returned %+v", got)
	}
}

func TestCreateProposal_UrgentAssignsTheBoss(t *testing.T) {
	fx := newProposalTestFixture(t)

	var bossUserID string
	dbfx.QueryRow(t, `SELECT user_id::text FROM member WHERE workspace_id = $1 AND role = 'owner' ORDER BY created_at ASC, id ASC LIMIT 1`, testWorkspaceID).Scan(&bossUserID)

	resp := fx.fileProposal(t, "urgent", map[string]any{"urgent": true, "urgent_reason": "The vendor contract lapses tomorrow."})
	if !resp.Urgent || resp.UrgentReason != "The vendor contract lapses tomorrow." {
		t.Fatalf("urgent not recorded: %+v", resp)
	}
	var assigneeType, assigneeID, description string
	dbfx.QueryRow(t, `SELECT assignee_type, assignee_id::text, description FROM issue WHERE id = $1`, resp.IssueID).Scan(&assigneeType, &assigneeID, &description)
	if assigneeType != "member" || assigneeID != bossUserID {
		t.Errorf("urgent proposal assigned to %s/%s, want member/%s", assigneeType, assigneeID, bossUserID)
	}
	if !strings.Contains(description, "The vendor contract lapses tomorrow.") {
		t.Errorf("description should carry the urgent reason:\n%s", description)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, resp.IssueID); n != 0 {
		t.Errorf("assigning the boss must not enqueue a run, got %d tasks", n)
	}
}

func TestCreateProposal_RejectsInvalidBodies(t *testing.T) {
	fx := newProposalTestFixture(t)
	cases := []struct {
		name string
		over map[string]any
	}{
		{"missing title", map[string]any{"title": ""}},
		{"missing summary", map[string]any{"summary": " "}},
		{"no evidence", map[string]any{"evidence": []string{}}},
		{"blank evidence", map[string]any{"evidence": []string{"  "}}},
		{"no options", map[string]any{"options": []string{}}},
		{"missing recommendation", map[string]any{"recommendation": ""}},
		{"missing cost_risk", map[string]any{"cost_risk": ""}},
		{"missing ask", map[string]any{"ask": ""}},
		{"urgent without reason", map[string]any{"urgent": true}},
		{"reason without urgent", map[string]any{"urgent_reason": "now"}},
		{"bad squad id", map[string]any{"squad_id": "not-a-uuid"}},
		{"bad handoff id", map[string]any{"handoff_squad_id": "nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("invalid"), tc.over)), fx.LeaderID, fx.LeaderTaskID)
			testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusBadRequest)
		})
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM proposal WHERE squad_id = $1`, fx.SquadID); n != 0 {
		t.Errorf("invalid bodies must create nothing, got %d proposals", n)
	}
}

func TestCreateProposal_Limits(t *testing.T) {
	const day = 24 * time.Hour
	type limitCase struct {
		name   string
		seed   func(t *testing.T, fx proposalTestFixture)
		over   map[string]any
		wantOK bool
		// message fragment the 409 must carry so the agent can act on it
		wantMsg string
	}
	urgent := map[string]any{"urgent": true, "urgent_reason": "cannot wait"}
	cases := []limitCase{
		{
			name: "3 pending in the squad blocks the 4th",
			seed: func(t *testing.T, fx proposalTestFixture) {
				for i := 0; i < service.ProposalMaxPendingPerSquad; i++ {
					seedProposalRow(t, fx.SquadID, "pending", false, time.Hour)
				}
			},
			wantMsg: "waiting for the boss's decision",
		},
		{
			name: "2 pending in the squad allows a 3rd",
			seed: func(t *testing.T, fx proposalTestFixture) {
				seedProposalRow(t, fx.SquadID, "pending", false, time.Hour)
				seedProposalRow(t, fx.SquadID, "pending", false, time.Hour)
			},
			wantOK: true,
		},
		{
			name: "3 created this week blocks the 4th even when decided",
			seed: func(t *testing.T, fx proposalTestFixture) {
				for i := 0; i < service.ProposalMaxCreatedPerSquadPerWeek; i++ {
					seedProposalRow(t, fx.SquadID, "rejected", false, 2*day)
				}
			},
			wantMsg: "last 7 days",
		},
		{
			name: "3 created more than 7 days ago do not count",
			seed: func(t *testing.T, fx proposalTestFixture) {
				for i := 0; i < service.ProposalMaxCreatedPerSquadPerWeek; i++ {
					seedProposalRow(t, fx.SquadID, "approved", false, 8*day)
				}
			},
			wantOK: true,
		},
		{
			name: "10 pending in the workspace blocks any squad",
			seed: func(t *testing.T, fx proposalTestFixture) {
				for i := 0; i < service.ProposalMaxPendingPerWorkspace; i++ {
					seedProposalRow(t, dbidString(t), "pending", false, time.Hour)
				}
			},
			wantMsg: "across the workspace",
		},
		{
			name: "9 pending in the workspace allows one more",
			seed: func(t *testing.T, fx proposalTestFixture) {
				for i := 0; i < service.ProposalMaxPendingPerWorkspace-1; i++ {
					seedProposalRow(t, dbidString(t), "pending", false, time.Hour)
				}
			},
			wantOK: true,
		},
		{
			name: "1 urgent this week blocks another urgent",
			seed: func(t *testing.T, fx proposalTestFixture) {
				seedProposalRow(t, fx.SquadID, "approved", true, 2*day)
			},
			over:    urgent,
			wantMsg: "without --urgent",
		},
		{
			name: "1 urgent this week still allows a non-urgent proposal",
			seed: func(t *testing.T, fx proposalTestFixture) {
				seedProposalRow(t, fx.SquadID, "approved", true, 2*day)
			},
			wantOK: true,
		},
		{
			name: "an urgent proposal older than 7 days does not count",
			seed: func(t *testing.T, fx proposalTestFixture) {
				seedProposalRow(t, fx.SquadID, "approved", true, 8*day)
			},
			over:   urgent,
			wantOK: true,
		},
		{
			name: "another squad's proposals do not count",
			seed: func(t *testing.T, fx proposalTestFixture) {
				for i := 0; i < service.ProposalMaxPendingPerSquad; i++ {
					seedProposalRow(t, dbidString(t), "pending", true, time.Hour)
				}
			},
			over:   urgent,
			wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newProposalTestFixture(t)
			tc.seed(t, fx)
			req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("limit"), tc.over)), fx.LeaderID, fx.LeaderTaskID)
			res := testutil.Call(t, testHandler.CreateProposal, req)
			if tc.wantOK {
				res.Want(http.StatusCreated)
				return
			}
			res.Want(http.StatusConflict)
			body := res.Map()
			if body["code"] != "proposal_limit" {
				t.Errorf("code = %v, want proposal_limit", body["code"])
			}
			if msg, _ := body["error"].(string); !strings.Contains(msg, tc.wantMsg) {
				t.Errorf("error %q should contain %q", msg, tc.wantMsg)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND title LIKE $2`, testWorkspaceID, fx.Prefix+"%"); n != 0 {
				t.Errorf("a refused proposal must not leave an issue behind, got %d", n)
			}
		})
	}
}

func dbidString(t *testing.T) string {
	t.Helper()
	var id string
	dbfx.QueryRow(t, `SELECT gen_random_uuid()::text`).Scan(&id)
	return id
}

func TestCreateProposal_ConcurrentCreatesRespectTheSquadLimit(t *testing.T) {
	fx := newProposalTestFixture(t)
	const attempts = 8
	codes := make(chan int, attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title(fmt.Sprintf("race %d", i)), nil)), fx.LeaderID, fx.LeaderTaskID)
			rec := testutil.Call(t, testHandler.CreateProposal, req)
			codes <- rec.Code
		}(i)
	}
	created := 0
	for i := 0; i < attempts; i++ {
		switch code := <-codes; code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if created != service.ProposalMaxPendingPerSquad {
		t.Fatalf("concurrent creates produced %d proposals, want exactly %d", created, service.ProposalMaxPendingPerSquad)
	}
}

func TestCreateProposal_Membership(t *testing.T) {
	fx := newProposalTestFixture(t)
	outsider := createHandlerTestAgent(t, "Prop Outsider "+fx.Prefix, nil)
	outsiderTask := createHandlerTestTaskForAgent(t, outsider)
	memberOutside := newWorkspaceMemberUser(t, "member")
	memberInside := newWorkspaceMemberUser(t, "member")
	dbfx.SquadMember(t, fx.SquadID, "member", memberInside)
	admin := newWorkspaceMemberUser(t, "admin")

	body := func(name string) map[string]any { return proposalBody(fx.SquadID, fx.title(name), nil) }

	t.Run("agent outside the squad is refused", func(t *testing.T) {
		req := asAgent(newRequest("POST", "/api/proposals", body("outsider")), outsider, outsiderTask)
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusForbidden)
	})
	t.Run("squad agent whose task has finished is refused", func(t *testing.T) {
		done := dbfx.Task(t, fx.LeaderID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "status": "completed"})
		req := asAgent(newRequest("POST", "/api/proposals", body("finished")), fx.LeaderID, done)
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusForbidden)
	})
	t.Run("squad agent running a task is allowed", func(t *testing.T) {
		req := asAgent(newRequest("POST", "/api/proposals", body("leader")), fx.LeaderID, fx.LeaderTaskID)
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusCreated)
	})
	t.Run("workspace member outside the squad is refused", func(t *testing.T) {
		req := asUser(newRequest("POST", "/api/proposals", body("member out")), memberOutside)
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusForbidden)
	})
	t.Run("squad member is allowed and recorded as a member", func(t *testing.T) {
		req := asUser(newRequest("POST", "/api/proposals", body("member in")), memberInside)
		var out ProposalResponse
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusCreated).JSON(&out)
		if out.CreatedByType != "member" || out.CreatedByID != memberInside {
			t.Errorf("creator = %s/%s, want member/%s", out.CreatedByType, out.CreatedByID, memberInside)
		}
	})
	t.Run("admin outside the squad is allowed", func(t *testing.T) {
		req := asUser(newRequest("POST", "/api/proposals", body("admin")), admin)
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusCreated)
	})
	t.Run("unknown handoff squad is refused", func(t *testing.T) {
		over := map[string]any{"handoff_squad_id": dbidString(t)}
		req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("handoff"), over)), fx.LeaderID, fx.LeaderTaskID)
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusBadRequest)
	})
}

func TestProposals_WorkspaceIsolation(t *testing.T) {
	fx := newProposalTestFixture(t)
	resp := fx.fileProposal(t, "isolated", nil)

	otherWS := dbfx.Workspace(t, "Prop Other WS", fmt.Sprintf("prop-other-%d", time.Now().UnixNano()))
	dbfx.Member(t, otherWS, testUserID, "owner")
	inOther := func(req *http.Request) *http.Request { return testutil.WithHeaders(req, "X-Workspace-ID", otherWS) }

	t.Run("get from another workspace is 404", func(t *testing.T) {
		testutil.Call(t, testHandler.GetProposal, inOther(getRequest("/api/proposals/"+resp.ID, resp.ID))).Want(http.StatusNotFound)
	})
	t.Run("list in another workspace is empty", func(t *testing.T) {
		var out []ProposalResponse
		testutil.Call(t, testHandler.ListProposals, inOther(newRequest("GET", "/api/proposals", nil))).Want(http.StatusOK).JSON(&out)
		if len(out) != 0 {
			t.Errorf("other workspace sees %d proposals", len(out))
		}
	})
	t.Run("decision from another workspace is 404 and changes nothing", func(t *testing.T) {
		req := inOther(decideRequest(resp.ID, map[string]any{"decision": "approve"}))
		testutil.Call(t, testHandler.DecideProposal, req).Want(http.StatusNotFound)
		var state string
		dbfx.QueryRow(t, `SELECT state FROM proposal WHERE id = $1`, resp.ID).Scan(&state)
		if state != "pending" {
			t.Errorf("state = %s, want pending", state)
		}
	})
	t.Run("filing for another workspace's squad is 404", func(t *testing.T) {
		req := inOther(asUser(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("cross"), nil)), testUserID))
		testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusNotFound)
	})
}

func TestListProposals_FiltersAndOrder(t *testing.T) {
	fx := newProposalTestFixture(t)
	first := fx.fileProposal(t, "first", nil)
	second := fx.fileProposal(t, "second", nil)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(first.ID, map[string]any{"decision": "approve"})).Want(http.StatusOK)

	list := func(query string) []ProposalResponse {
		var out []ProposalResponse
		testutil.Call(t, testHandler.ListProposals, newRequest("GET", "/api/proposals?"+query, nil)).Want(http.StatusOK).JSON(&out)
		return out
	}

	all := list("squad_id=" + fx.SquadID)
	if len(all) != 2 || all[0].ID != second.ID || all[1].ID != first.ID {
		t.Fatalf("expected [second, first] newest first, got %+v", all)
	}
	if all[0].Title != fx.title("second") || all[0].IssueIdentifier == "" {
		t.Errorf("list rows should carry the issue title and identifier: %+v", all[0])
	}
	if got := list("squad_id=" + fx.SquadID + "&state=pending"); len(got) != 1 || got[0].ID != second.ID {
		t.Errorf("pending filter returned %+v", got)
	}
	if got := list("squad_id=" + fx.SquadID + "&state=approved"); len(got) != 1 || got[0].ID != first.ID {
		t.Errorf("approved filter returned %+v", got)
	}
	if got := list("squad_id=" + dbidString(t)); len(got) != 0 {
		t.Errorf("unknown squad returned %+v", got)
	}
	testutil.Call(t, testHandler.ListProposals, newRequest("GET", "/api/proposals?state=bogus", nil)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ListProposals, newRequest("GET", "/api/proposals?squad_id=bogus", nil)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.GetProposal, getRequest("/api/proposals/"+dbidString(t), dbidString(t))).Want(http.StatusNotFound)
}

func TestDecideProposal_ApproveWithoutHandoff(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "approve", nil)

	var out ProposalResponse
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "approve", "note": "Go ahead."})).
		Want(http.StatusOK).JSON(&out)
	if out.State != "approved" || out.DecisionNote != "Go ahead." || out.DecidedBy == nil || *out.DecidedBy != testUserID || out.DecidedAt == nil {
		t.Fatalf("unexpected decided proposal: %+v", out)
	}
	if out.Handoff != nil {
		t.Errorf("no handoff squad, but response carries handoff %+v", out.Handoff)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, p.IssueID).Scan(&status)
	if status != "done" {
		t.Errorf("backing issue status = %s, want done", status)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE workspace_id = $1 AND title LIKE $2`, testWorkspaceID, fx.Prefix+"%"); n != 0 {
		t.Errorf("approval without a handoff must not create a project, got %d", n)
	}

	// A decided proposal cannot be decided again.
	res := testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "reject", "note": "changed my mind"})).Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_decided" {
		t.Errorf("code = %v, want proposal_decided", res.Map()["code"])
	}
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, p.IssueID).Scan(&status)
	if status != "done" {
		t.Errorf("second decision changed the issue to %s", status)
	}
}

func TestDecideProposal_ApproveWithHandoffStartsTheWork(t *testing.T) {
	fx := newProposalTestFixture(t)
	handoffLeader := createHandlerTestAgent(t, "Prop Handoff Leader "+fx.Prefix, nil)
	handoffSquad := dbfx.Squad(t, "Prop Handoff Squad "+fx.Prefix, handoffLeader)
	dbfx.SquadMember(t, handoffSquad, "agent", handoffLeader, testutil.Cols{"role": "leader"})
	registerProposalCleanup(t, fx.Prefix, fx.SquadID)

	p := fx.fileProposal(t, "handoff", map[string]any{"handoff_squad_id": handoffSquad})
	if p.HandoffSquadID == nil || *p.HandoffSquadID != handoffSquad {
		t.Fatalf("handoff squad not recorded: %+v", p)
	}

	var out ProposalResponse
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "approve", "note": "Start with the pilot cohort."})).
		Want(http.StatusOK).JSON(&out)
	if out.Handoff == nil || out.Handoff.ProjectID == "" || out.Handoff.KickoffIssueID == "" {
		t.Fatalf("approval with a handoff should return the project and kickoff ids: %+v", out)
	}

	var projectTitle string
	dbfx.QueryRow(t, `SELECT title FROM project WHERE id = $1 AND workspace_id = $2`, out.Handoff.ProjectID, testWorkspaceID).Scan(&projectTitle)
	if projectTitle != fx.title("handoff") {
		t.Errorf("project title = %q, want the proposal title", projectTitle)
	}

	var assigneeType, assigneeID, projectID, creatorType, description string
	dbfx.QueryRow(t, `SELECT assignee_type, assignee_id::text, project_id::text, creator_type, description FROM issue WHERE id = $1`,
		out.Handoff.KickoffIssueID).Scan(&assigneeType, &assigneeID, &projectID, &creatorType, &description)
	if assigneeType != "squad" || assigneeID != handoffSquad || projectID != out.Handoff.ProjectID || creatorType != "member" {
		t.Errorf("kickoff issue = %s/%s project %s creator %s", assigneeType, assigneeID, projectID, creatorType)
	}
	if !strings.Contains(description, "mention://issue/"+p.IssueID) {
		t.Errorf("kickoff description should link back to the proposal issue:\n%s", description)
	}
	if !strings.Contains(description, "Start with the pilot cohort.") {
		t.Errorf("kickoff description should carry the boss's note:\n%s", description)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, out.Handoff.KickoffIssueID, handoffLeader); n != 1 {
		t.Errorf("handoff squad leader should have one queued task on the kickoff issue, got %d", n)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, p.IssueID).Scan(&status)
	if status != "done" {
		t.Errorf("backing issue status = %s, want done", status)
	}
}

func TestDecideProposal_ApproveWithArchivedHandoffSquadChangesNothing(t *testing.T) {
	fx := newProposalTestFixture(t)
	handoffLeader := createHandlerTestAgent(t, "Prop Archived Leader "+fx.Prefix, nil)
	handoffSquad := dbfx.Squad(t, "Prop Archived Squad "+fx.Prefix, handoffLeader)
	p := fx.fileProposal(t, "archived handoff", map[string]any{"handoff_squad_id": handoffSquad})
	dbfx.Exec(t, `UPDATE squad SET archived_at = now() WHERE id = $1`, handoffSquad)

	res := testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "approve"})).Want(http.StatusConflict)
	if res.Map()["code"] != "handoff_squad_unavailable" {
		t.Errorf("code = %v", res.Map()["code"])
	}
	var state, status string
	dbfx.QueryRow(t, `SELECT p.state, i.status FROM proposal p JOIN issue i ON i.id = p.issue_id WHERE p.id = $1`, p.ID).Scan(&state, &status)
	if state != "pending" || status != "in_review" {
		t.Errorf("a refused approval must leave the proposal untouched, got %s/%s", state, status)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE workspace_id = $1 AND title LIKE $2`, testWorkspaceID, fx.Prefix+"%"); n != 0 {
		t.Errorf("a refused approval must not leave a project, got %d", n)
	}
}

func TestDecideProposal_RejectMentionsTheLeader(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "reject", nil)

	var out ProposalResponse
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "reject", "note": "Come back with a pricing model."})).
		Want(http.StatusOK).JSON(&out)
	if out.State != "rejected" || out.DecisionNote != "Come back with a pricing model." {
		t.Fatalf("unexpected rejected proposal: %+v", out)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, p.IssueID).Scan(&status)
	if status != "cancelled" {
		t.Errorf("backing issue status = %s, want cancelled", status)
	}

	var commentID, authorType, authorID, content string
	dbfx.QueryRow(t, `SELECT id::text, author_type, author_id::text, content FROM comment WHERE issue_id = $1`, p.IssueID).
		Scan(&commentID, &authorType, &authorID, &content)
	wantMention := fmt.Sprintf("[@%s](mention://agent/%s)", fx.LeaderName, fx.LeaderID)
	if authorType != "member" || authorID != testUserID {
		t.Errorf("comment author = %s/%s, want the deciding member", authorType, authorID)
	}
	if !strings.HasPrefix(content, wantMention) || !strings.Contains(content, "Come back with a pricing model.") {
		t.Errorf("comment = %q, want it to start with %q and carry the note", content, wantMention)
	}

	// The ordinary mention path wakes the proposing squad's leader.
	var triggerCommentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT trigger_comment_id::text FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`, p.IssueID, fx.LeaderID,
	).Scan(&triggerCommentID); err != nil {
		t.Fatalf("expected a queued task for the squad leader: %v", err)
	}
	if triggerCommentID != commentID {
		t.Errorf("task trigger comment = %s, want the rejection comment %s", triggerCommentID, commentID)
	}
}

func TestDecideProposal_Authorization(t *testing.T) {
	fx := newProposalTestFixture(t)
	plainMember := newWorkspaceMemberUser(t, "member")
	admin := newWorkspaceMemberUser(t, "admin")
	// One proposal serves every refusal: a refusal must leave it pending.
	p := fx.fileProposal(t, "authz", nil)
	body := map[string]any{"decision": "approve"}

	stillPending := func(t *testing.T) {
		t.Helper()
		var state string
		dbfx.QueryRow(t, `SELECT state FROM proposal WHERE id = $1`, p.ID).Scan(&state)
		if state != "pending" {
			t.Fatalf("a refused decision changed the proposal to %s", state)
		}
	}

	t.Run("plain member is refused", func(t *testing.T) {
		testutil.Call(t, testHandler.DecideProposal, asUser(decideRequest(p.ID, body), plainMember)).Want(http.StatusForbidden)
		stillPending(t)
	})
	t.Run("agent actor is refused", func(t *testing.T) {
		testutil.Call(t, testHandler.DecideProposal, asAgent(decideRequest(p.ID, body), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusForbidden)
		stillPending(t)
	})
	t.Run("agent acting with an owner's task token is refused", func(t *testing.T) {
		req := testutil.WithHeaders(asAgent(decideRequest(p.ID, body), fx.LeaderID, fx.LeaderTaskID), "X-Actor-Source", "task_token")
		testutil.Call(t, testHandler.DecideProposal, req).Want(http.StatusForbidden)
		stillPending(t)
	})
	t.Run("owner-credential machine access is refused", func(t *testing.T) {
		req := testutil.WithHeaders(decideRequest(p.ID, body), "X-Actor-Source", "cloud_pat")
		testutil.Call(t, testHandler.DecideProposal, req).Want(http.StatusForbidden)
		stillPending(t)
	})
	t.Run("invalid decision is a bad request", func(t *testing.T) {
		testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "maybe"})).Want(http.StatusBadRequest)
		testutil.Call(t, testHandler.DecideProposal, decideRequest("not-a-uuid", body)).Want(http.StatusBadRequest)
		stillPending(t)
	})
	t.Run("admin member may decide", func(t *testing.T) {
		testutil.Call(t, testHandler.DecideProposal, asUser(decideRequest(p.ID, body), admin)).Want(http.StatusOK)
	})
}
