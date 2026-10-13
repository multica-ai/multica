package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Whole-thread deletion has its own route; ordinary DELETE must keep replies.
func TestDeleteCommentThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	member := func(name, role string) string {
		t.Helper()
		id := dbfx.Insert(t, "user", testutil.Cols{"name": name, "email": name + "@thread-delete.test"})
		dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": id, "role": role})
		return id
	}
	author := member("root-author", "member")
	other := member("reply-author", "member")
	admin := member("moderator", "admin")
	unrelated := member("unrelated-member", "member")

	for _, tt := range []struct {
		name   string
		actor  string
		thread bool
		status int
	}{
		{"root author removes mixed-author thread", author, true, http.StatusNoContent},
		{"owner removes mixed-author thread", testUserID, true, http.StatusNoContent},
		{"admin removes mixed-author thread", admin, true, http.StatusNoContent},
		{"unrelated member cannot remove thread", unrelated, true, http.StatusForbidden},
		{"ordinary delete still keeps replies", author, false, http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			issue := dbfx.Issue(t, tt.name)
			root := dbfx.Comment(t, issue, "root", testutil.Cols{"author_id": author})
			reply := dbfx.Comment(t, issue, "reply", testutil.Cols{"parent_id": root, "author_id": other})
			nested := dbfx.Comment(t, issue, "nested reply", testutil.Cols{"parent_id": reply})
			sibling := dbfx.Comment(t, issue, "sibling reply", testutil.Cols{"parent_id": root, "author_id": other})
			untouched := dbfx.Comment(t, issue, "other thread")
			untouchedReply := dbfx.Comment(t, issue, "other thread reply", testutil.Cols{"parent_id": untouched})

			path, handle := "/api/comments/"+root, testHandler.DeleteComment
			if tt.thread {
				path, handle = path+"/thread", testHandler.DeleteCommentThread
			}
			req := withURLParam(newRequest(http.MethodDelete, path, nil), "commentId", root)
			req.Header.Set("X-User-ID", tt.actor)
			testutil.Call(t, handle, req).Want(tt.status)

			// Read through the public timeline, including tombstones, rather than
			// inspecting storage: a deleted thread must disappear, not just blank.
			entries, status := fetchTimeline(t, issue)
			if status != http.StatusOK {
				t.Fatalf("timeline status = %d, want 200", status)
			}
			want := map[string]string{untouched: "other thread", untouchedReply: "other thread reply"}
			if !tt.thread || tt.status == http.StatusForbidden {
				want[root] = "root"
				want[reply] = "reply"
				want[nested] = "nested reply"
				want[sibling] = "sibling reply"
			}
			if !tt.thread {
				want[root] = ""
			}
			if len(entries) != len(want) {
				t.Errorf("timeline has %d entries, want %d", len(entries), len(want))
			}
			for _, entry := range entries {
				content, exists := want[entry.ID]
				if !exists || entry.Content == nil || *entry.Content != content {
					t.Errorf("unexpected timeline entry %s (expected content %q, present %v)", entry.ID, content, exists)
				}
				deleted := !tt.thread && entry.ID == root
				if (entry.DeletedAt != nil) != deleted {
					t.Errorf("entry %s deleted = %v, want %v", entry.ID, entry.DeletedAt != nil, deleted)
				}
			}
		})
	}
}
