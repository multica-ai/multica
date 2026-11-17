package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The tier GET /api/issues/{id}/subscribers reports for each member is the
// tier the fanout delivers to them. The issue page badges read that field, so
// a mismatch here is a badge telling someone the wrong thing (MUL-9360).
func TestSubscriberDeliveryMatchesFanout(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	member := func(email string) string {
		id := createNotificationMember(t, email)
		t.Cleanup(func() { cleanupTestUser(t, email) })
		return id
	}
	owner := member("delivery-owner@multica.ai")
	bystander := member("delivery-bystander@multica.ai")
	follower := member("delivery-follower@multica.ai")
	projectWatcher := member("delivery-project-watcher@multica.ai")
	delegate := member("delivery-delegate@multica.ai")
	optedIn := member("delivery-opted-in@multica.ai")
	setOthersIssuesPref(t, optedIn, "all")
	// Status changes are muted by default; this test is about the tiers.
	enableStatusChanges(t, owner, bystander, follower, projectWatcher, delegate, optedIn)

	projectID := createTestProject(t)
	f := workspaceFixture(t)
	issueID := f.Issue(t, "delivery tier issue", testutil.Cols{
		"assignee_type": "member",
		"assignee_id":   owner,
		"project_id":    projectID,
	})
	t.Cleanup(func() { cleanupInboxForIssue(t, issueID) })

	addTestSubscriber(t, issueID, "member", owner, "assignee")
	addTestSubscriber(t, issueID, "member", bystander, "commenter")
	addTestSubscriber(t, issueID, "member", follower, "manual")
	addTestSubscriber(t, issueID, "member", projectWatcher, "commenter")
	addProjectSubscribers(t, projectID, projectWatcher)
	addTestSubscriber(t, issueID, "member", delegate, "delegated")
	addTestSubscriber(t, issueID, "member", optedIn, "creator")

	cases := []struct {
		name string
		id   string
		want inbox.Delivery
	}{
		{"own issue", owner, inbox.DeliveryAll},
		{"someone else's issue", bystander, inbox.DeliveryKeyUpdates},
		{"manual follow", follower, inbox.DeliveryAll},
		{"project subscription on top", projectWatcher, inbox.DeliveryAll},
		{"delegated", delegate, inbox.DeliveryDelegated},
		{"others_issues=all", optedIn, inbox.DeliveryAll},
	}

	resp := authRequest(t, http.MethodGet, "/api/issues/"+issueID+"/subscribers", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list subscribers: status %d", resp.StatusCode)
	}
	var rows []handler.SubscriberResponse
	readJSON(t, resp, &rows)
	listed := map[string]inbox.Delivery{}
	for _, row := range rows {
		if row.UserType == "member" {
			listed[row.UserID] = row.Delivery
		}
	}
	for _, c := range cases {
		if got := listed[c.id]; got != c.want {
			t.Errorf("%s: listed delivery %q, want %q", c.name, got, c.want)
		}
	}

	// A routine status move and a top-level comment, both by someone outside
	// the subscriber list, separate the three tiers.
	publishStatusChange(bus, issueID, "in_progress", "todo")
	content := "a routine update"
	commentID := f.Comment(t, issueID, content, testutil.Cols{"author_id": testUserID})
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         commentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   testUserID,
				Content:    content,
				Type:       "comment",
			},
			"issue_title":  "delivery tier issue",
			"issue_status": "in_progress",
		},
	})

	received := map[inbox.Delivery][]string{
		inbox.DeliveryAll:        {"new_comment", "status_changed"},
		inbox.DeliveryKeyUpdates: {},
		inbox.DeliveryDelegated:  {"new_comment"},
	}
	for _, c := range cases {
		got := inboxTypes(t, queries, c.id)
		slices.Sort(got)
		if want := received[listed[c.id]]; !slices.Equal(got, want) {
			t.Errorf("%s: listed %q but received %v, want %v", c.name, listed[c.id], got, want)
		}
	}
}
