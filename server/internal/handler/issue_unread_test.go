package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListIssuesHasUnreadUsesNewestActiveInboxItem(t *testing.T) {
	ctx := context.Background()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id, number
		)
		VALUES ($1, 'Unread indicator test', 'todo', 'none', 'member', $2,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	insertItem := func(id string, read, archived bool, createdAt string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			INSERT INTO inbox_item (
				id, workspace_id, recipient_type, recipient_id, type, issue_id,
				title, read, archived, created_at
			)
			VALUES ($1, $2, 'member', $3, 'new_comment', $4, 'Update', $5, $6, $7)
		`, id, testWorkspaceID, testUserID, issueID, read, archived, createdAt); err != nil {
			t.Fatalf("create inbox item: %v", err)
		}
	}

	// An older unread item must not keep the dot alive when the newest active
	// item is read. A newer archived item is excluded from the decision.
	insertItem("00000000-0000-0000-0000-000000000001", false, false, "2026-07-16T10:00:00Z")
	insertItem("00000000-0000-0000-0000-000000000002", true, false, "2026-07-16T11:00:00Z")
	insertItem("00000000-0000-0000-0000-000000000003", false, true, "2026-07-16T12:00:00Z")

	list := func() IssueResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest(http.MethodGet, "/api/issues?limit=100", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var payload struct {
			Issues []IssueResponse `json:"issues"`
		}
		if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		for _, issue := range payload.Issues {
			if issue.ID == issueID {
				return issue
			}
		}
		t.Fatalf("issue %s missing from response", issueID)
		return IssueResponse{}
	}

	if got := list().HasUnread; got == nil || *got {
		t.Fatal("has_unread = true with a newer active read item")
	}

	insertItem("00000000-0000-0000-0000-000000000004", false, false, "2026-07-16T13:00:00Z")
	if got := list().HasUnread; got == nil || !*got {
		t.Fatal("has_unread = false with a newest active unread item")
	}

	insertItem("00000000-0000-0000-0000-000000000005", true, false, "2026-07-16T13:00:00Z")
	if got := list().HasUnread; got == nil || *got {
		t.Fatal("has_unread = true when same-time higher-id active item is read")
	}
}
