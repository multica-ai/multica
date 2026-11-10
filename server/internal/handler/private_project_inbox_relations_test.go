package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type inboxItemState struct{ read, archived bool }

func loadInboxItemState(t *testing.T, itemID string) inboxItemState {
	t.Helper()
	var s inboxItemState
	if err := testPool.QueryRow(context.Background(),
		`SELECT read, archived FROM inbox_item WHERE id = $1`, itemID,
	).Scan(&s.read, &s.archived); err != nil {
		t.Fatalf("load inbox item %s: %v", itemID, err)
	}
	return s
}

func insertSecretInboxItem(t *testing.T, f privateProjectFixture, recipient string) string {
	t.Helper()
	return dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": recipient,
		"type": "mentioned", "severity": "info", "title": f.marker + " secret notification", "issue_id": f.secretIssue,
	})
}

// Read, unread, archive and unarchive all load the item through
// loadInboxItemForUser, so an item on an issue the caller can no longer see
// must 404 there and stay untouched.
func TestPrivateProjectInboxItemMutationsNeedVisibility(t *testing.T) {
	f := newPrivateProjectFixture(t)
	type mutation struct {
		name    string
		path    string
		handler http.HandlerFunc
		before  inboxItemState
		after   inboxItemState
	}
	mutations := []mutation{
		{"read", "/read", testHandler.MarkInboxRead, inboxItemState{false, false}, inboxItemState{true, false}},
		{"unread", "/unread", testHandler.MarkInboxUnread, inboxItemState{true, false}, inboxItemState{false, false}},
		{"archive", "/archive", testHandler.ArchiveInboxItem, inboxItemState{false, false}, inboxItemState{false, true}},
		{"unarchive", "/unarchive", testHandler.UnarchiveInboxItem, inboxItemState{false, true}, inboxItemState{false, false}},
	}
	call := func(m mutation, userID, itemID string) *testutil.Response {
		dbfx.Exec(t, `UPDATE inbox_item SET read = $2, archived = $3 WHERE id = $1`, itemID, m.before.read, m.before.archived)
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/inbox/"+itemID+m.path, nil), userID), "id", itemID)
		return testutil.Call(t, inboxWorkspaceHandler(m.handler), req)
	}
	allowed := func(who, userID, itemID string) {
		t.Helper()
		for _, m := range mutations {
			call(m, userID, itemID).Want(http.StatusOK)
			if got := loadInboxItemState(t, itemID); got != m.after {
				t.Errorf("%s %s: state = %+v, want %+v", who, m.name, got, m.after)
			}
		}
	}
	blocked := func(who, userID, itemID string) {
		t.Helper()
		for _, m := range mutations {
			call(m, userID, itemID).Want(http.StatusNotFound)
			if got := loadInboxItemState(t, itemID); got != m.before {
				t.Errorf("%s %s: refused mutation changed state to %+v, want %+v", who, m.name, got, m.before)
			}
		}
	}

	ownerItem := insertSecretInboxItem(t, f, testUserID)
	insiderItem := insertSecretInboxItem(t, f, f.insider)
	// Delivered before the outsider lost access (or written by a path that
	// never checked): the row exists, but the outsider may not act on it.
	outsiderItem := insertSecretInboxItem(t, f, f.outsider)

	allowed("workspace owner outside the member list", testUserID, ownerItem)
	allowed("project member", f.insider, insiderItem)
	blocked("workspace member outside the project", f.outsider, outsiderItem)

	removeProjectMemberAsOwner(t, f, f.insider)
	blocked("revoked project member", f.insider, insiderItem)

	// Notifications outside the private project stay actionable after revocation.
	publicItem := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": f.insider,
		"type": "mentioned", "severity": "info", "title": f.marker + " public notification", "issue_id": f.publicIssue,
	})
	allowed("revoked project member on a public issue", f.insider, publicItem)
}

func TestPrivateProjectSubscribedIssueIDsHidden(t *testing.T) {
	f := newPrivateProjectFixture(t)
	subscribe := func(issueID, userID string) {
		dbfx.InsertNoID(t, "issue_subscriber", testutil.Cols{
			"issue_id": issueID, "user_type": "member", "user_id": userID, "reason": "manual",
		}, "issue_id = $1 AND user_id = $2", issueID, userID)
	}
	for _, userID := range []string{testUserID, f.insider, f.outsider} {
		subscribe(f.secretIssue, userID)
		subscribe(f.publicIssue, userID)
	}
	subscribed := func(userID string) map[string]bool {
		t.Helper()
		var out struct {
			IssueIDs []string `json:"issue_ids"`
		}
		req := asUser(newRequest(http.MethodGet, "/api/issues/subscribed", nil), userID)
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListSubscribedIssueIDs), req).Want(http.StatusOK).JSON(&out)
		ids := map[string]bool{}
		for _, id := range out.IssueIDs {
			ids[id] = true
		}
		return ids
	}
	check := func(who, userID string, wantSecret bool) {
		t.Helper()
		got := subscribed(userID)
		if got[f.secretIssue] != wantSecret || !got[f.publicIssue] {
			t.Errorf("%s: subscribed secret=%v public=%v, want secret=%v public=true", who, got[f.secretIssue], got[f.publicIssue], wantSecret)
		}
	}
	check("workspace owner outside the member list", testUserID, true)
	check("project member", f.insider, true)
	check("workspace member outside the project", f.outsider, false)

	removeProjectMemberAsOwner(t, f, f.insider)
	check("revoked project member", f.insider, false)
}

func TestPrivateProjectLabelUsageCountHidden(t *testing.T) {
	f := newPrivateProjectFixture(t)
	labelID := dbfx.Insert(t, "issue_label", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": f.marker + " label", "color": "#f00", "resource_type": "issue",
	})
	dbfx.Cleanup(t, `DELETE FROM issue_to_label WHERE label_id = $1`, labelID)
	dbfx.Exec(t, `INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1, $3), ($2, $3)`, f.secretIssue, f.publicIssue, labelID)

	usage := func(userID string) int64 {
		t.Helper()
		var out struct {
			Labels []LabelResponse `json:"labels"`
		}
		req := asUser(newRequest(http.MethodGet, "/api/labels?resource_type=issue", nil), userID)
		testutil.Call(t, testHandler.ListLabels, req).Want(http.StatusOK).JSON(&out)
		for _, l := range out.Labels {
			if l.ID == labelID {
				return l.UsageCount
			}
		}
		t.Fatalf("label %s missing from %s's list", labelID, userID)
		return 0
	}
	for _, tc := range []struct {
		name   string
		userID string
		want   int64
	}{
		{"workspace owner outside the member list", testUserID, 2},
		{"project member", f.insider, 2},
		{"workspace member outside the project", f.outsider, 1},
	} {
		if got := usage(tc.userID); got != tc.want {
			t.Errorf("%s: usage_count = %d, want %d", tc.name, got, tc.want)
		}
	}

	removeProjectMemberAsOwner(t, f, f.insider)
	if got := usage(f.insider); got != 1 {
		t.Errorf("revoked project member: usage_count = %d, want 1", got)
	}
}

// A project-scoped view preference on a project the caller cannot see answers
// exactly like one on a project that does not exist, for reads and writes.
func TestPrivateProjectViewPreferenceScopeHidden(t *testing.T) {
	f := newPrivateProjectFixture(t)
	const missingProject = "00000000-0000-0000-0000-00000000beef"
	for _, userID := range []string{testUserID, f.insider, f.outsider} {
		dbfx.Cleanup(t, `DELETE FROM issue_view_preference WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
	}
	get := func(userID, projectID string) *testutil.Response {
		req := asUser(newRequest(http.MethodGet, "/api/issue-view-preferences?scope_type=project&scope_id="+projectID, nil), userID)
		return testutil.Call(t, testHandler.GetIssueViewPreference, req)
	}
	put := func(userID, projectID string) *testutil.Response {
		req := asUser(newRequest(http.MethodPut, "/api/issue-view-preferences", map[string]any{
			"scope_type": "project", "scope_id": projectID, "prefs": map[string]any{"hidden": []string{"builtin:all"}},
		}), userID)
		return testutil.Call(t, testHandler.PutIssueViewPreference, req)
	}
	stored := func(userID string) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(context.Background(),
			`SELECT count(*) FROM issue_view_preference WHERE workspace_id = $1 AND user_id = $2 AND scope_type = 'project' AND scope_id = $3`,
			testWorkspaceID, userID, f.projectID,
		).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	blocked := func(who, userID string) {
		t.Helper()
		before := stored(userID)
		missingGet := get(userID, missingProject).Want(http.StatusNotFound).Text()
		missingPut := put(userID, missingProject).Want(http.StatusNotFound).Text()
		if body := get(userID, f.projectID).Want(http.StatusNotFound).Text(); body != missingGet {
			t.Errorf("%s: hidden GET body %q differs from missing %q", who, body, missingGet)
		}
		if body := put(userID, f.projectID).Want(http.StatusNotFound).Text(); body != missingPut {
			t.Errorf("%s: hidden PUT body %q differs from missing %q", who, body, missingPut)
		}
		if after := stored(userID); after != before {
			t.Errorf("%s: refused PUT changed stored preferences from %d to %d", who, before, after)
		}
	}

	for _, tc := range []struct{ name, userID string }{
		{"workspace owner outside the member list", testUserID},
		{"project member", f.insider},
	} {
		put(tc.userID, f.projectID).Want(http.StatusOK)
		get(tc.userID, f.projectID).Want(http.StatusOK)
		if stored(tc.userID) != 1 {
			t.Errorf("%s: project preference was not stored", tc.name)
		}
	}
	blocked("workspace member outside the project", f.outsider)

	removeProjectMemberAsOwner(t, f, f.insider)
	blocked("revoked project member", f.insider)
}
