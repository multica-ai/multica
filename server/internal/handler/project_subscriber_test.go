package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectSubscriptionAPI(t *testing.T) {
	f := newPrivateProjectFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_subscriber WHERE project_id = $1`, f.projectID)
	})

	call := func(method, userID string) *testutil.Response {
		req := withURLParam(asUser(newRequest(method, "/api/projects/"+f.projectID+"/subscription", nil), userID), "id", f.projectID)
		switch method {
		case http.MethodPut:
			return testutil.Call(t, testHandler.SubscribeToProject, req)
		case http.MethodDelete:
			return testutil.Call(t, testHandler.UnsubscribeFromProject, req)
		default:
			return testutil.Call(t, testHandler.GetProjectSubscription, req)
		}
	}
	subscribed := func(method, userID string) bool {
		var resp ProjectSubscriptionResponse
		call(method, userID).Want(http.StatusOK).JSON(&resp)
		return resp.Subscribed
	}

	if subscribed(http.MethodGet, f.insider) {
		t.Fatal("new viewer should start unsubscribed")
	}
	if !subscribed(http.MethodPut, f.insider) {
		t.Fatal("PUT should subscribe")
	}
	// Idempotent.
	if !subscribed(http.MethodPut, f.insider) || !subscribed(http.MethodGet, f.insider) {
		t.Fatal("subscription should persist")
	}
	if subscribed(http.MethodDelete, f.insider) || subscribed(http.MethodGet, f.insider) {
		t.Fatal("DELETE should unsubscribe")
	}

	// A member who cannot see the private project cannot watch it either.
	call(http.MethodPut, f.outsider).Want(http.StatusNotFound)
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM project_subscriber WHERE project_id = $1 AND user_id = $2`,
		f.projectID, f.outsider).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("outsider subscription rows = %d, want 0", count)
	}
}

func TestIssueSubscribersIncludeProjectSubscribers(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_subscriber WHERE project_id = $1`, f.projectID)
		testPool.Exec(context.Background(), `DELETE FROM issue_subscriber WHERE issue_id = $1`, f.secretIssue)
	})
	// The outsider's row stands for a member who lost access after subscribing.
	for _, userID := range []string{f.insider, f.outsider, testUserID} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO project_subscriber (workspace_id, project_id, user_id) VALUES ($1, $2, $3)
		`, testWorkspaceID, f.projectID, userID); err != nil {
			t.Fatal(err)
		}
	}

	issuePath := "/api/issues/" + f.secretIssue
	reasons := func() map[string][]string {
		t.Helper()
		var resp []SubscriberResponse
		req := withURLParam(asUser(newRequest(http.MethodGet, issuePath+"/subscribers", nil), testUserID), "id", f.secretIssue)
		testutil.Call(t, testHandler.ListIssueSubscribers, req).Want(http.StatusOK).JSON(&resp)
		got := map[string][]string{}
		for _, s := range resp {
			got[s.UserID] = append(got[s.UserID], s.Reason)
		}
		return got
	}
	act := func(h http.HandlerFunc, path, userID string) {
		t.Helper()
		req := withURLParam(asUser(newRequest(http.MethodPost, path, nil), userID), "id", f.secretIssue)
		testutil.Call(t, h, req).Want(http.StatusOK)
	}
	optOutScope := func(userID string) string {
		t.Helper()
		var scope string
		if err := testPool.QueryRow(ctx, `
			SELECT opt_out_scope FROM issue_subscriber
			WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2 AND unsubscribed_at IS NOT NULL
		`, f.secretIssue, userID).Scan(&scope); err != nil {
			t.Fatalf("opt-out for %s: %v", userID, err)
		}
		return scope
	}

	got := reasons()
	if r := got[f.insider]; len(r) != 1 || r[0] != "project" {
		t.Fatalf("insider reasons = %v, want [project]", r)
	}
	if r, ok := got[f.outsider]; ok {
		t.Fatalf("outsider listed with %v; they cannot see the project", r)
	}

	// Leaving the issue persists an opt-out although no row existed.
	act(testHandler.UnsubscribeFromIssue, issuePath+"/unsubscribe", f.insider)
	if r, ok := reasons()[f.insider]; ok {
		t.Fatalf("insider still listed with %v after leaving the issue", r)
	}
	if scope := optOutScope(f.insider); scope != "issue" {
		t.Fatalf("insider opt-out scope = %q, want issue", scope)
	}

	// An existing subtree opt-out is not narrowed by a second unsubscribe.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason, unsubscribed_at, opt_out_scope)
		VALUES ($1, 'member', $2, 'manual', now(), 'subtree')
	`, f.secretIssue, testUserID); err != nil {
		t.Fatal(err)
	}
	act(testHandler.UnsubscribeFromIssue, issuePath+"/unsubscribe", testUserID)
	if scope := optOutScope(testUserID); scope != "subtree" {
		t.Fatalf("owner opt-out scope = %q, want subtree", scope)
	}

	// Subscribing explicitly brings the member back once, as a direct row.
	act(testHandler.SubscribeToIssue, issuePath+"/subscribe", f.insider)
	if r := reasons()[f.insider]; len(r) != 1 || r[0] != "manual" {
		t.Fatalf("insider reasons after explicit subscribe = %v, want [manual]", r)
	}
}
