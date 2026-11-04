package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Squad autonomy and standing approvals over HTTP (spec 4.3 and 4.4): members read,
// owners and admins change, agents are refused.

func TestSquadAutonomy_ReadsDefaultToLevelOneAndChangesAreBossOnly(t *testing.T) {
	fx := newCompanyWorkspace(t)
	wsfx := testutil.New(testPool, fx.ID, fx.Owner)
	leader := wsfx.Agent(t, "autonomy leader", "")
	squad := wsfx.Squad(t, "Autonomy squad", leader)

	get := func(userID string) *testutil.Response {
		return testutil.Call(t, testHandler.GetSquadAutonomy,
			testutil.WithURLParams(companyReq(userID, fx.ID, http.MethodGet, "/api/squads/"+squad+"/autonomy", nil), "id", squad))
	}
	put := func(userID string, body any) *testutil.Response {
		return testutil.Call(t, testHandler.SetSquadAutonomy,
			testutil.WithURLParams(companyReq(userID, fx.ID, http.MethodPut, "/api/squads/"+squad+"/autonomy", body), "id", squad))
	}

	var out SquadAutonomyResponse
	get(fx.Member).Want(http.StatusOK).JSON(&out)
	if out.Level != 1 || out.LevelName != "Work" || out.ChangedAt != nil || out.Reason != "" {
		t.Fatalf("a new squad reads %+v, want L1 Work with no reason", out)
	}

	put(fx.Member, map[string]any{"level": 2, "reason": "a member asks"}).Want(http.StatusForbidden)
	put(fx.Admin, map[string]any{"level": 2, "reason": "Reviewed drafts for two weeks."}).Want(http.StatusOK).JSON(&out)
	if out.Level != 2 || out.LevelName != "Trusted" || out.Reason != "Reviewed drafts for two weeks." || out.ChangedAt == nil {
		t.Fatalf("an admin's change returned %+v", out)
	}
	get(fx.Member).Want(http.StatusOK).JSON(&out)
	if out.Level != 2 || out.Reason != "Reviewed drafts for two weeks." {
		t.Fatalf("the change is not read back: %+v", out)
	}
}

func TestSquadAutonomy_AgentsCannotChangeALevel(t *testing.T) {
	fx := newCompanyWorkspace(t)
	wsfx := testutil.New(testPool, fx.ID, fx.Owner)
	runtime := wsfx.Runtime(t, "autonomy runtime")
	leader := wsfx.Agent(t, "autonomy agent leader", runtime)
	squad := wsfx.Squad(t, "Agent autonomy squad", leader)
	task := wsfx.Task(t, leader, testutil.Cols{"runtime_id": runtime})
	req := testutil.JSONRequest(http.MethodPut, "/api/squads/"+squad+"/autonomy", map[string]any{"level": 3, "reason": "I know best"})
	req = testutil.WithHeaders(req, "X-Workspace-ID", fx.ID, "X-User-ID", fx.Owner, "X-Agent-ID", leader, "X-Task-ID", task)
	req = testutil.WithURLParams(req, "id", squad)
	testutil.Call(t, testHandler.SetSquadAutonomy, req).Want(http.StatusForbidden)
}

func TestSquadAutonomy_ValidatesTheLevelAndReason(t *testing.T) {
	fx := newCompanyWorkspace(t)
	wsfx := testutil.New(testPool, fx.ID, fx.Owner)
	leader := wsfx.Agent(t, "autonomy validate leader", "")
	squad := wsfx.Squad(t, "Validate autonomy squad", leader)
	put := func(body any) *testutil.Response {
		return testutil.Call(t, testHandler.SetSquadAutonomy,
			testutil.WithURLParams(companyReq(fx.Owner, fx.ID, http.MethodPut, "/api/squads/"+squad+"/autonomy", body), "id", squad))
	}
	put(map[string]any{"reason": "no level"}).Want(http.StatusBadRequest)
	put(map[string]any{"level": 4, "reason": "too high"}).Want(http.StatusBadRequest)
	put(map[string]any{"level": 1, "reason": ""}).Want(http.StatusBadRequest)
	put(map[string]any{"level": 0, "reason": "Hold while the charter is reviewed."}).Want(http.StatusOK)
}

func TestApprovalPolicies_BossCreatesRevokesAndReadsTheHistory(t *testing.T) {
	fx := newCompanyWorkspace(t)
	base := "/api/approval-policies"
	create := func(userID string, body any) *testutil.Response {
		return testutil.Call(t, testHandler.CreateApprovalPolicy, companyReq(userID, fx.ID, http.MethodPost, base, body))
	}

	create(fx.Member, map[string]any{"name": "Mine", "kind": "decision"}).Want(http.StatusForbidden)
	create(fx.Owner, map[string]any{"name": "Forbidden", "kind": "org_change"}).Want(http.StatusBadRequest)
	create(fx.Owner, map[string]any{"name": "Forbidden", "kind": "standing_approval"}).Want(http.StatusBadRequest)
	// Tags are matched now that proposals carry them (spec 4.4), so a tag match is accepted.
	create(fx.Owner, map[string]any{"name": "Tags", "kind": "decision", "match": map[string]any{"tags": []string{"x"}}}).Want(http.StatusCreated)

	var created ApprovalPolicyResponse
	create(fx.Admin, map[string]any{"name": "Routine notes", "kind": "decision", "max_per_week": 2}).
		Want(http.StatusCreated).JSON(&created)
	if created.Name != "Routine notes" || created.Kind != "decision" || created.MaxPerWeek != 2 || !created.Active || created.CreatedBy != fx.Admin {
		t.Fatalf("created policy = %+v", created)
	}

	var list ListApprovalPoliciesResponse
	testutil.Call(t, testHandler.ListApprovalPolicies, companyReq(fx.Member, fx.ID, http.MethodGet, base, nil)).
		Want(http.StatusOK).JSON(&list)
	// The tags policy filed above is listed too; the routine one is the one this test reads.
	if len(list.Policies) != 2 {
		t.Fatalf("list has %d policies, want the two filed above: %+v", len(list.Policies), list)
	}
	var routine *ApprovalPolicyResponse
	for i := range list.Policies {
		if list.Policies[i].ID == created.ID {
			routine = &list.Policies[i]
		}
	}
	if routine == nil || routine.Flagged {
		t.Fatalf("the routine policy is missing or flagged: %+v", list)
	}

	revoke := func(userID string) *testutil.Response {
		return testutil.Call(t, testHandler.RevokeApprovalPolicy,
			testutil.WithURLParams(companyReq(userID, fx.ID, http.MethodPost, base+"/"+created.ID+"/revoke", nil), "id", created.ID))
	}
	revoke(fx.Member).Want(http.StatusForbidden)
	var revoked ApprovalPolicyResponse
	revoke(fx.Owner).Want(http.StatusOK).JSON(&revoked)
	if revoked.Active {
		t.Fatalf("a revoked policy still reads active: %+v", revoked)
	}
	revoke(fx.Owner).Want(http.StatusOK)

	var history ListPolicyAutoApprovalsResponse
	testutil.Call(t, testHandler.ListPolicyAutoApprovals,
		testutil.WithURLParams(companyReq(fx.Member, fx.ID, http.MethodGet, base+"/"+created.ID+"/auto-approvals", nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&history)
	if len(history.AutoApprovals) != 0 || history.Flagged {
		t.Fatalf("an unused policy's history = %+v", history)
	}
	testutil.Call(t, testHandler.RevokeApprovalPolicy,
		testutil.WithURLParams(companyReq(fx.Owner, fx.ID, http.MethodPost, base+"/00000000-0000-4000-8000-0000000000ee/revoke", nil), "id", "00000000-0000-4000-8000-0000000000ee")).
		Want(http.StatusNotFound)
}
