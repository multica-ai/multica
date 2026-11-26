package handler

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestIssueTreeWalksStopOnParentCycle pins that the subscriber queries walking
// an issue's ancestors or descendants finish when the stored parent chain loops
// (A -> B -> A). Older servers could write such a loop, and a walk that never
// stops keeps the request and its database connection busy until the client
// gives up. Each query must answer well inside the deadline and still give the
// result it gives on a tree without the loop.
func TestIssueTreeWalksStopOnParentCycle(t *testing.T) {
	projectID := dbfx.Project(t, "cycle walk project")
	a := dbfx.Issue(t, "cycle walk A", testutil.Cols{"project_id": projectID})
	b := dbfx.Issue(t, "cycle walk B", testutil.Cols{"project_id": projectID, "parent_issue_id": a})
	dbfx.Exec(t, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, b, a)
	// An opt-out on an unrelated issue, as anyone who ever left an issue has.
	// With it the opt-out checks have to read the whole ancestor chain instead
	// of finding nothing to compare against.
	other := dbfx.Issue(t, "cycle walk unrelated")
	dbfx.Exec(t, `INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason, unsubscribed_at, opt_out_scope)
		VALUES ($1, 'member', $2, 'manual', now(), 'issue')`, other, testUserID)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `UPDATE issue SET parent_issue_id = NULL WHERE id = ANY($1::uuid[])`, []string{a, b})
		testPool.Exec(ctx, `DELETE FROM issue_subscriber WHERE issue_id = ANY($1::uuid[])`, []string{a, b, other})
		testPool.Exec(ctx, `DELETE FROM project_subscriber WHERE project_id = $1`, projectID)
	})

	q := db.New(testPool)
	userID := parseUUID(testUserID)
	workspaceID := parseUUID(testWorkspaceID)
	withDeadline := func(t *testing.T) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		return ctx
	}

	t.Run("project subscribers", func(t *testing.T) {
		if err := q.AddProjectSubscriber(withDeadline(t), db.AddProjectSubscriberParams{
			ProjectID: parseUUID(projectID), WorkspaceID: workspaceID, UserID: userID,
		}); err != nil {
			t.Fatalf("AddProjectSubscriber: %v", err)
		}
		rows, err := q.ListProjectSubscribersForIssue(withDeadline(t), parseUUID(a))
		if err != nil {
			t.Fatalf("ListProjectSubscribersForIssue on a parent loop: %v", err)
		}
		if len(rows) != 1 || rows[0].UserID != userID {
			t.Fatalf("project subscribers = %+v, want only the test user", rows)
		}
	})

	t.Run("delegated subscriber", func(t *testing.T) {
		affected, err := q.AddDelegatedSubscriber(withDeadline(t), db.AddDelegatedSubscriberParams{
			IssueID: parseUUID(a), UserID: userID, Reason: "delegated", WorkspaceID: workspaceID,
		})
		if err != nil {
			t.Fatalf("AddDelegatedSubscriber on a parent loop: %v", err)
		}
		if affected != 1 {
			t.Fatalf("AddDelegatedSubscriber affected %d rows, want 1", affected)
		}
	})

	t.Run("subtree unsubscribe and opt-out", func(t *testing.T) {
		dbfx.Exec(t, `INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
			VALUES ($1, 'member', $2, 'manual') ON CONFLICT DO NOTHING`, b, testUserID)
		ids, err := q.UnsubscribeFromIssueSubtree(withDeadline(t), db.UnsubscribeFromIssueSubtreeParams{
			ID: parseUUID(a), UserType: "member", UserID: userID,
		})
		if err != nil {
			t.Fatalf("UnsubscribeFromIssueSubtree on a parent loop: %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("UnsubscribeFromIssueSubtree retired %v, want A and B once each", ids)
		}
		optedOut, err := q.HasAncestorOptOut(withDeadline(t), db.HasAncestorOptOutParams{
			ID: parseUUID(b), UserType: "member", UserID: userID,
		})
		if err != nil {
			t.Fatalf("HasAncestorOptOut on a parent loop: %v", err)
		}
		if !optedOut {
			t.Fatal("HasAncestorOptOut lost the subtree opt-out on a parent loop")
		}
	})
}
