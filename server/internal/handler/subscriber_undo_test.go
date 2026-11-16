package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Undo of an unsubscribe restores the subscription as it was. Subscribing
// again would rewrite every reason to manual: a commenter or delegated
// watcher would start getting every event, and a project watcher would pick
// up a direct row that outlives the project subscription (MUL-9343).
func TestUndoUnsubscribeRestoresTheSubscription(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_subscriber WHERE project_id = $1`, f.projectID)
		testPool.Exec(context.Background(), `DELETE FROM issue_subscriber WHERE issue_id = $1`, f.secretIssue)
	})
	issuePath := "/api/issues/" + f.secretIssue

	call := func(h http.HandlerFunc, path, userID string) *testutil.Response {
		t.Helper()
		req := withURLParam(asUser(newRequest(http.MethodPost, path, nil), userID), "id", f.secretIssue)
		return testutil.Call(t, h, req)
	}
	reasonOf := func(userID string) string {
		t.Helper()
		var resp []SubscriberResponse
		req := withURLParam(asUser(newRequest(http.MethodGet, issuePath+"/subscribers", nil), testUserID), "id", f.secretIssue)
		testutil.Call(t, testHandler.ListIssueSubscribers, req).Want(http.StatusOK).JSON(&resp)
		for _, s := range resp {
			if s.UserType == "member" && s.UserID == userID {
				return s.Reason
			}
		}
		return ""
	}
	storedRows := func(userID string) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM issue_subscriber WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2`,
			f.secretIssue, userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	undo := func(userID string, want int) map[string]any {
		t.Helper()
		resp := call(testHandler.UndoUnsubscribeFromIssue, issuePath+"/unsubscribe/undo", userID).Want(want)
		var body map[string]any
		if want == http.StatusOK {
			resp.JSON(&body)
		}
		return body
	}

	for _, reason := range []string{"commenter", "delegated"} {
		t.Run(reason, func(t *testing.T) {
			testPool.Exec(ctx, `DELETE FROM issue_subscriber WHERE issue_id = $1 AND user_id = $2`, f.secretIssue, testUserID)
			if _, err := testPool.Exec(ctx, `
				INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason, created_at)
				VALUES ($1, 'member', $2, $3, now() - interval '1 hour')
			`, f.secretIssue, testUserID, reason); err != nil {
				t.Fatal(err)
			}
			call(testHandler.UnsubscribeFromIssue, issuePath+"/unsubscribe", testUserID).Want(http.StatusOK)
			if got := reasonOf(testUserID); got != "" {
				t.Fatalf("still listed as %q after unsubscribing", got)
			}
			if body := undo(testUserID, http.StatusOK); body["reason"] != reason {
				t.Fatalf("undo answered %v, want reason %q", body, reason)
			}
			if got := reasonOf(testUserID); got != reason {
				t.Fatalf("reason after undo = %q, want %q", got, reason)
			}
		})
	}

	t.Run("project", func(t *testing.T) {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO project_subscriber (workspace_id, project_id, user_id) VALUES ($1, $2, $3)
		`, testWorkspaceID, f.projectID, f.insider); err != nil {
			t.Fatal(err)
		}
		call(testHandler.UnsubscribeFromIssue, issuePath+"/unsubscribe", f.insider).Want(http.StatusOK)
		if got := reasonOf(f.insider); got != "" {
			t.Fatalf("project watcher still listed as %q after unsubscribing", got)
		}
		if body := undo(f.insider, http.StatusOK); body["reason"] != "project" {
			t.Fatalf("undo answered %v, want reason project", body)
		}
		if got := reasonOf(f.insider); got != "project" {
			t.Fatalf("reason after undo = %q, want project", got)
		}
		if n := storedRows(f.insider); n != 0 {
			t.Fatalf("undo left %d issue_subscriber rows; the project subscription alone should cover the issue", n)
		}
	})

	t.Run("nothing to undo", func(t *testing.T) {
		undo(f.insider, http.StatusConflict)
	})

	t.Run("subtree opt-out stays", func(t *testing.T) {
		testPool.Exec(ctx, `DELETE FROM issue_subscriber WHERE issue_id = $1 AND user_id = $2`, f.secretIssue, testUserID)
		call(testHandler.UnsubscribeFromIssueSubtree, issuePath+"/unsubscribe/subtree", testUserID).Want(http.StatusOK)
		undo(testUserID, http.StatusConflict)
		var scope string
		if err := testPool.QueryRow(ctx, `
			SELECT opt_out_scope FROM issue_subscriber
			WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2 AND unsubscribed_at IS NOT NULL
		`, f.secretIssue, testUserID).Scan(&scope); err != nil || scope != "subtree" {
			t.Fatalf("subtree opt-out after undo: scope=%q err=%v", scope, err)
		}
	})
}
