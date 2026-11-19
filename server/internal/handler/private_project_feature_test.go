package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// usePrivateProjectsFeature points the shared test handler at a stub that
// returns decision for private_projects in the test workspace, and clears the
// workspace's recorded attempts before and after.
func usePrivateProjectsFeature(t *testing.T, decision entitlement.FeatureDecision) {
	t.Helper()
	stub := entitlementtest.New()
	stub.SetFeature(uuid.MustParse(testWorkspaceID), entitlement.FeaturePrivateProjects, decision)
	prior := testHandler.Features
	testHandler.Features = stub
	clearFeatureAttempts(t)
	t.Cleanup(func() {
		testHandler.Features = prior
		clearFeatureAttempts(t)
	})
}

func clearFeatureAttempts(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM entitlement_feature_attempt WHERE workspace_id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("clear feature attempts: %v", err)
	}
}

type featureAttemptCounts struct {
	observed, degraded, enforced int64
	lastOperation                string
}

func privateProjectAttempts(t *testing.T) featureAttemptCounts {
	t.Helper()
	var c featureAttemptCounts
	err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(observed_count), 0), COALESCE(SUM(degraded_count), 0), COALESCE(SUM(enforced_count), 0),
		       COALESCE(MAX(last_operation), '')
		FROM entitlement_feature_attempt
		WHERE workspace_id = $1 AND feature = 'private_projects'`, testWorkspaceID,
	).Scan(&c.observed, &c.degraded, &c.enforced, &c.lastOperation)
	if err != nil {
		t.Fatalf("read feature attempts: %v", err)
	}
	return c
}

func wantFeatureLocked(t *testing.T, resp *testutil.Response) {
	t.Helper()
	var body struct {
		Code    string `json:"code"`
		Feature string `json:"feature"`
	}
	resp.Want(http.StatusForbidden).JSON(&body)
	if body.Code != "feature_locked" || body.Feature != "private_projects" {
		t.Fatalf("locked body = %+v, want feature_locked/private_projects", body)
	}
}

func createPrivateProjectAs(t *testing.T, userID, title string) *testutil.Response {
	t.Helper()
	body := map[string]any{"title": title, "visibility": "private"}
	resp := testutil.Call(t, testHandler.CreateProject, asUser(newRequest(http.MethodPost, "/api/projects", body), userID))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id IN (SELECT id FROM project WHERE title = $1)`, title)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE title = $1`, title)
	})
	return resp
}

func projectCountByTitle(t *testing.T, title string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project WHERE title = $1`, title).Scan(&n); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	return n
}

// The example feature end to end: the same create works under off, is
// admitted and recorded under observe, and is rejected and recorded under
// enforce without writing anything.
func TestPrivateProjectCreateUnderEachFeatureAction(t *testing.T) {
	for _, tc := range []struct {
		action   entitlement.Action
		want     int
		attempts featureAttemptCounts
	}{
		{entitlement.ActionOff, http.StatusCreated, featureAttemptCounts{}},
		{entitlement.ActionObserve, http.StatusCreated, featureAttemptCounts{observed: 1, lastOperation: "create"}},
		{entitlement.ActionEnforce, http.StatusForbidden, featureAttemptCounts{enforced: 1, lastOperation: "create"}},
	} {
		t.Run(string(tc.action), func(t *testing.T) {
			usePrivateProjectsFeature(t, entitlement.FeatureDecision{Action: tc.action, Reason: entitlement.ReasonRefreshed, PolicyRevision: 4})
			title := "feature create " + string(tc.action) + " " + uuid.NewString()
			resp := createPrivateProjectAs(t, testUserID, title)
			if tc.want == http.StatusForbidden {
				wantFeatureLocked(t, resp)
				if n := projectCountByTitle(t, title); n != 0 {
					t.Fatalf("rejected create wrote %d projects", n)
				}
			} else {
				resp.Want(tc.want)
			}
			if got := privateProjectAttempts(t); got != tc.attempts {
				t.Fatalf("attempts = %+v, want %+v", got, tc.attempts)
			}
		})
	}
}

// A workspace-visible project is not the paid feature: creating one is never
// restricted or recorded.
func TestWorkspaceProjectCreateIgnoresPrivateProjectsFeature(t *testing.T) {
	usePrivateProjectsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionEnforce})
	title := "feature public " + uuid.NewString()
	resp := testutil.Call(t, testHandler.CreateProject, newRequest(http.MethodPost, "/api/projects", map[string]any{"title": title}))
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE title = $1`, title) })
	resp.Want(http.StatusCreated)
	if got := privateProjectAttempts(t); got != (featureAttemptCounts{}) {
		t.Fatalf("attempts = %+v, want none", got)
	}
}

// After a downgrade the existing private project keeps working exactly as
// configured, can be reduced and made public, and only additions are refused.
func TestPrivateProjectEnforcedKeepsExistingConfigAndAllowsReductions(t *testing.T) {
	f := newPrivateProjectFixture(t)
	usePrivateProjectsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionEnforce, Reason: entitlement.ReasonRefreshed})

	// Access control is untouched: still members only.
	getProject := func(userID string) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/projects/"+f.projectID, nil), userID), "id", f.projectID)
		return testutil.Call(t, testHandler.GetProject, req)
	}
	getProject(f.insider).Want(http.StatusOK)
	getProject(f.outsider).Want(http.StatusNotFound)
	getProject(f.outsideAdmin).Want(http.StatusNotFound)

	// Edits that do not add to the private configuration.
	updateProjectAs(t, testUserID, f.projectID, map[string]any{"title": f.marker + " renamed"}).Want(http.StatusOK)
	updateProjectAs(t, testUserID, f.projectID, map[string]any{"member_ids": []string{testUserID, f.insider}}).Want(http.StatusOK)
	addProjectMembersAs(t, testUserID, f.projectID, f.insider).Want(http.StatusOK)
	updateProjectAs(t, testUserID, f.projectID, map[string]any{"lead_type": "member", "lead_id": f.insider}).Want(http.StatusOK)
	updateProjectAs(t, testUserID, f.projectID, map[string]any{"lead_type": nil, "lead_id": nil}).Want(http.StatusOK)
	if got := privateProjectAttempts(t); got != (featureAttemptCounts{}) {
		t.Fatalf("unchanged configuration recorded attempts: %+v", got)
	}

	// Additions, directly or through a lead who is not a member yet.
	wantFeatureLocked(t, addProjectMembersAs(t, testUserID, f.projectID, f.outsider))
	wantFeatureLocked(t, updateProjectAs(t, testUserID, f.projectID, map[string]any{"member_ids": []string{f.outsider}}))
	wantFeatureLocked(t, updateProjectAs(t, testUserID, f.projectID, map[string]any{"lead_type": "member", "lead_id": f.outsider}))
	getProject(f.outsider).Want(http.StatusNotFound)
	if got := privateProjectAttempts(t); got.enforced != 3 || got.lastOperation != "add_members" {
		t.Fatalf("attempts = %+v, want 3 enforced add_members", got)
	}

	// Reductions.
	req := testutil.WithURLParams(newRequest(http.MethodDelete, "/api/projects/"+f.projectID+"/members/"+f.insider, nil),
		"id", f.projectID, "userId", f.insider)
	testutil.Call(t, testHandler.RemoveProjectMember, req).Want(http.StatusNoContent)
	updateProjectAs(t, testUserID, f.projectID, map[string]any{"visibility": "workspace"}).Want(http.StatusOK)
	getProject(f.outsider).Want(http.StatusOK)

	// Making it private again adds to the configuration.
	wantFeatureLocked(t, updateProjectAs(t, testUserID, f.projectID, map[string]any{"visibility": "private"}))
	if got := privateProjectAttempts(t); got.enforced != 4 || got.lastOperation != "make_private" {
		t.Fatalf("attempts = %+v, want 4 enforced, last make_private", got)
	}
}

// The restriction is commercial. A caller the ordinary rules already refuse
// gets the ordinary 403, and nothing is recorded as a feature attempt.
func TestPrivateProjectFeatureRunsAfterAuthorization(t *testing.T) {
	f := newPrivateProjectFixture(t)
	usePrivateProjectsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionEnforce})

	var body struct {
		Code string `json:"code"`
	}
	addProjectMembersAs(t, f.insider, f.projectID, f.outsider).Want(http.StatusForbidden).JSON(&body)
	if body.Code == "feature_locked" {
		t.Fatal("a caller without permission must get the ordinary permission error")
	}
	if got := privateProjectAttempts(t); got != (featureAttemptCounts{}) {
		t.Fatalf("attempts = %+v, want none", got)
	}
}

// A workspace owner's recovery join keeps a private project with no members
// reachable, so it is never restricted.
func TestPrivateProjectOwnerRecoveryJoinIsNotRestricted(t *testing.T) {
	f := newPrivateProjectFixture(t)
	removeProjectMemberRow(t, f.projectID, testUserID)
	usePrivateProjectsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionEnforce})

	req := withURLParam(newRequest(http.MethodPost, "/api/projects/"+f.projectID+"/join", nil), "id", f.projectID)
	testutil.Call(t, testHandler.JoinPrivateProject, req).Want(http.StatusOK)
}

// Additions admitted only because a stale enforce was downgraded during a
// Cloud outage are recorded apart from a configured observe rollout, and a
// configured observe served from a stale cache still counts as observed.
func TestPrivateProjectStaleDecisionsAreRecordedByOrigin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision entitlement.FeatureDecision
		want     featureAttemptCounts
	}{
		{"stale enforce downgraded", entitlement.FeatureDecision{Action: entitlement.ActionObserve, Reason: entitlement.ReasonStale, Downgraded: true},
			featureAttemptCounts{degraded: 1, lastOperation: "add_members"}},
		{"stale observe", entitlement.FeatureDecision{Action: entitlement.ActionObserve, Reason: entitlement.ReasonStale},
			featureAttemptCounts{observed: 1, lastOperation: "add_members"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPrivateProjectFixture(t)
			usePrivateProjectsFeature(t, tc.decision)
			addProjectMembersAs(t, testUserID, f.projectID, f.outsider).Want(http.StatusOK)
			if got := privateProjectAttempts(t); got != tc.want {
				t.Fatalf("attempts = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Recording happens before the response is written. A contended attempt row
// must cost at most the record's own short timeout, and the already-decided
// result is returned either way: the committed create for observe, the
// refusal for enforce.
func TestPrivateProjectAttemptRecordCannotHoldTheResponse(t *testing.T) {
	for _, tc := range []struct {
		action entitlement.Action
		want   int
	}{
		{entitlement.ActionObserve, http.StatusCreated},
		{entitlement.ActionEnforce, http.StatusForbidden},
	} {
		t.Run(string(tc.action), func(t *testing.T) {
			usePrivateProjectsFeature(t, entitlement.FeatureDecision{Action: tc.action})
			ctx := context.Background()
			// Hold today's attempt row for this workspace and feature in an
			// open transaction, so the handler's upsert has to wait for it.
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, `
				INSERT INTO entitlement_feature_attempt (workspace_id, feature, day, last_operation, last_action, last_reason)
				VALUES ($1, 'private_projects', (now() AT TIME ZONE 'UTC')::date, 'lock', 'observe', '')`, testWorkspaceID); err != nil {
				t.Fatalf("hold attempt row: %v", err)
			}

			title := "feature contended " + string(tc.action) + " " + uuid.NewString()
			started := time.Now()
			resp := createPrivateProjectAs(t, testUserID, title)
			elapsed := time.Since(started)
			resp.Want(tc.want)
			if elapsed > 3*time.Second {
				t.Fatalf("response took %s while the attempt row was locked", elapsed)
			}
			wantProjects := 0
			if tc.want == http.StatusCreated {
				wantProjects = 1
			}
			if n := projectCountByTitle(t, title); n != wantProjects {
				t.Fatalf("projects = %d, want %d", n, wantProjects)
			}
		})
	}
}

func TestGetWorkspaceFeatures(t *testing.T) {
	get := func(t *testing.T, userID string) *testutil.Response {
		t.Helper()
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/features", nil), userID), "id", testWorkspaceID)
		return testutil.Call(t, testHandler.GetWorkspaceFeatures, req)
	}
	state := func(t *testing.T) string {
		t.Helper()
		var resp WorkspaceFeaturesResponse
		get(t, testUserID).Want(http.StatusOK).JSON(&resp)
		return resp.Features["private_projects"].State
	}

	t.Run("self-host leaves every feature available", func(t *testing.T) {
		prior := testHandler.Features
		testHandler.Features = nil
		t.Cleanup(func() { testHandler.Features = prior })
		if got := state(t); got != "available" {
			t.Fatalf("state = %q, want available", got)
		}
	})
	for _, tc := range []struct {
		decision entitlement.FeatureDecision
		want     string
	}{
		{entitlement.FeatureDecision{Action: entitlement.ActionOff}, "available"},
		{entitlement.FeatureDecision{Action: entitlement.ActionObserve}, "available"},
		{entitlement.FeatureDecision{Action: entitlement.ActionObserve, Reason: entitlement.ReasonStale}, "available"},
		{entitlement.FeatureDecision{Action: entitlement.ActionEnforce}, "locked"},
	} {
		t.Run(string(tc.decision.Action)+"/"+string(tc.decision.Reason), func(t *testing.T) {
			usePrivateProjectsFeature(t, tc.decision)
			if got := state(t); got != tc.want {
				t.Fatalf("state = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("non-member is refused", func(t *testing.T) {
		resp := get(t, uuid.NewString())
		if resp.Code == http.StatusOK {
			t.Fatal("a non-member must not read the workspace's features")
		}
	})
}
