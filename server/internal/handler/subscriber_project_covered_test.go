package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A direct row of a member who also watches the project says so: the project
// delivers every event, so the direct reason does not describe a reduced tier.
func TestIssueSubscribersMarkProjectCoveredRows(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_subscriber WHERE project_id = $1`, f.projectID)
		testPool.Exec(context.Background(), `DELETE FROM issue_subscriber WHERE issue_id = $1`, f.secretIssue)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project_subscriber (workspace_id, project_id, user_id) VALUES ($1, $2, $3)
	`, testWorkspaceID, f.projectID, f.insider); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{f.insider, testUserID} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason) VALUES ($1, 'member', $2, 'commenter')
		`, f.secretIssue, userID); err != nil {
			t.Fatal(err)
		}
	}

	var resp []SubscriberResponse
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+f.secretIssue+"/subscribers", nil), testUserID), "id", f.secretIssue)
	testutil.Call(t, testHandler.ListIssueSubscribers, req).Want(http.StatusOK).JSON(&resp)

	got := map[string][]SubscriberResponse{}
	for _, s := range resp {
		got[s.UserID] = append(got[s.UserID], s)
	}
	if rows := got[f.insider]; len(rows) != 1 || rows[0].Reason != "commenter" || !rows[0].ProjectCovered {
		t.Fatalf("project watcher rows = %+v, want one commenter row marked project_covered", rows)
	}
	if rows := got[testUserID]; len(rows) != 1 || rows[0].ProjectCovered {
		t.Fatalf("plain commenter rows = %+v, want one row without project_covered", rows)
	}
}
