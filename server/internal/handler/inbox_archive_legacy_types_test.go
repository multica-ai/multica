package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestArchivedInboxFilterKeepsLegacyTypes(t *testing.T) {
	for _, tc := range []struct{ kind, group string }{
		{"task_completed", "run"},
		{"review_requested", "review"},
		{"agent_blocked", "run"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ws := dbfx.Workspace(t, "Legacy archive", "legacy-archive-"+uuid.NewString())
			dbfx.Member(t, ws, testUserID, "owner")
			issue := dbfx.Issue(t, "Historical notification", testutil.Cols{"workspace_id": ws})
			id := dbfx.Insert(t, "inbox_item", testutil.Cols{
				"workspace_id": ws, "recipient_type": "member", "recipient_id": testUserID,
				"type": tc.kind, "severity": "info", "title": "Historical notification",
				"issue_id": issue, "archived": true,
			})
			var page archivedInboxPageResponse
			testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage),
				inboxRequest(http.MethodGet, "/api/inbox/archived/page?types="+tc.group, ws)).Want(http.StatusOK).JSON(&page)
			if len(page.Items) != 1 || page.Items[0].ID != id {
				t.Fatalf("historical %s notification missing from %s filter: %+v", tc.kind, tc.group, page.Items)
			}
		})
	}
}
