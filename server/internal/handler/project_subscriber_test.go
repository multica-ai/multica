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
