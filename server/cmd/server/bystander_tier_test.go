package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestDeliverToBystander(t *testing.T) {
	cases := []struct {
		notifType         string
		handoff           bool
		threadParticipant bool
		want              bool
	}{
		{"mentioned", false, false, true},
		{"status_changed", true, false, true},
		{"status_changed", false, false, false},
		{"new_comment", false, true, true},
		{"new_comment", false, false, false},
		{"task_failed", true, true, false},
		{"assignee_changed", true, true, false},
		{"priority_changed", true, true, false},
		{"reaction_added", true, true, false},
	}
	for _, c := range cases {
		if got := deliverToBystander(c.notifType, c.handoff, c.threadParticipant); got != c.want {
			t.Errorf("deliverToBystander(%q, handoff=%v, thread=%v) = %v, want %v",
				c.notifType, c.handoff, c.threadParticipant, got, c.want)
		}
	}
}

func setOthersIssuesPref(t *testing.T, userID, value string) {
	t.Helper()
	workspaceFixture(t).Exec(t, `
		INSERT INTO notification_preference (workspace_id, user_id, preferences)
		VALUES ($1, $2, jsonb_build_object('others_issues', $3::text))
		ON CONFLICT (workspace_id, user_id) DO UPDATE SET preferences = EXCLUDED.preferences
	`, testWorkspaceID, userID, value)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM notification_preference WHERE workspace_id = $1 AND user_id = $2`,
			testWorkspaceID, userID)
	})
}

func inboxCount(t *testing.T, queries *db.Queries, recipientID string) int {
	t.Helper()
	return len(inboxItemsForRecipient(t, queries, recipientID))
}

// On an issue assigned to someone else, a passive subscription hears only the
// handoff; the owner, an explicit follower and a member who chose "all" hear
// the routine move too.
func TestBystanderTier_StatusChange(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	owner := createNotificationMember(t, "bystander-owner@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-owner@multica.ai") })
	watcher := createNotificationMember(t, "bystander-watcher@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-watcher@multica.ai") })
	follower := createNotificationMember(t, "bystander-follower@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-follower@multica.ai") })
	optedIn := createNotificationMember(t, "bystander-all@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-all@multica.ai") })
	setOthersIssuesPref(t, optedIn, "all")

	issueID := workspaceFixture(t).Issue(t, "bystander tier issue", testutil.Cols{
		"assignee_type": "member",
		"assignee_id":   owner,
	})
	t.Cleanup(func() { cleanupInboxForIssue(t, issueID) })

	addTestSubscriber(t, issueID, "member", owner, "assignee")
	addTestSubscriber(t, issueID, "member", watcher, "commenter")
	addTestSubscriber(t, issueID, "member", follower, "manual")
	addTestSubscriber(t, issueID, "member", optedIn, "creator")

	publishStatusChange(bus, issueID, "in_progress", "todo")

	for name, c := range map[string]struct {
		id   string
		want int
	}{
		"owner":    {owner, 1},
		"watcher":  {watcher, 0},
		"follower": {follower, 1},
		"opted in": {optedIn, 1},
	} {
		if got := inboxCount(t, queries, c.id); got != c.want {
			t.Errorf("%s after in_progress: %d inbox items, want %d", name, got, c.want)
		}
	}

	publishStatusChange(bus, issueID, "in_review", "in_progress")
	if got := inboxCount(t, queries, watcher); got != 1 {
		t.Errorf("watcher after in_review: %d inbox items, want 1", got)
	}
}

// An issue assigned to an agent the subscriber owns is their own issue.
func TestBystanderTier_OwnAgentIsOwnIssue(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	owner := createNotificationMember(t, "bystander-agent-owner@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-agent-owner@multica.ai") })
	other := createNotificationMember(t, "bystander-agent-other@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-agent-other@multica.ai") })

	f := workspaceFixture(t)
	runtimeID := f.Runtime(t, "bystander-runtime", testutil.Cols{"owner_id": owner})
	agentID := f.Agent(t, "bystander-agent", runtimeID, testutil.Cols{"owner_id": owner})
	issueID := f.Issue(t, "bystander tier issue", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	t.Cleanup(func() { cleanupInboxForIssue(t, issueID) })

	addTestSubscriber(t, issueID, "member", owner, "creator")
	addTestSubscriber(t, issueID, "member", other, "creator")

	publishStatusChange(bus, issueID, "in_progress", "todo")

	if got := inboxCount(t, queries, owner); got != 1 {
		t.Errorf("agent owner: %d inbox items, want 1", got)
	}
	if got := inboxCount(t, queries, other); got != 0 {
		t.Errorf("other member: %d inbox items, want 0", got)
	}
}

// A bystander still hears a reply in a thread they took part in, and nothing
// from a thread they did not.
func TestBystanderTier_ThreadReply(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	owner := createNotificationMember(t, "bystander-thread-owner@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-thread-owner@multica.ai") })
	watcher := createNotificationMember(t, "bystander-thread-watcher@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-thread-watcher@multica.ai") })

	f := workspaceFixture(t)
	issueID := f.Issue(t, "bystander tier issue", testutil.Cols{
		"assignee_type": "member",
		"assignee_id":   owner,
	})
	t.Cleanup(func() { cleanupInboxForIssue(t, issueID) })
	addTestSubscriber(t, issueID, "member", watcher, "commenter")

	rootID := f.Comment(t, issueID, "a question from the watcher", testutil.Cols{"author_id": watcher})
	publishComment := func(content string, parentID *string) {
		commentID := f.Comment(t, issueID, content, testutil.Cols{"author_id": owner, "parent_id": parentID})
		bus.Publish(events.Event{
			Type:        protocol.EventCommentCreated,
			WorkspaceID: testWorkspaceID,
			ActorType:   "member",
			ActorID:     owner,
			Payload: map[string]any{
				"comment": handler.CommentResponse{
					ID:         commentID,
					IssueID:    issueID,
					AuthorType: "member",
					AuthorID:   owner,
					Content:    content,
					Type:       "comment",
					ParentID:   parentID,
				},
				"issue_title":  "bystander tier issue",
				"issue_status": "in_progress",
			},
		})
	}

	publishComment("an unrelated update", nil)
	if got := inboxCount(t, queries, watcher); got != 0 {
		t.Fatalf("watcher after an unrelated comment: %d inbox items, want 0", got)
	}

	publishComment("an answer", &rootID)
	items := inboxItemsForRecipient(t, queries, watcher)
	if len(items) != 1 || items[0].Type != "new_comment" {
		t.Fatalf("watcher after a reply in their thread: %+v, want one new_comment", items)
	}
}

// A member who watches the project and commented on someone else's issue in it
// has a commenter row, but the project path still delivers every event — once.
func TestBystanderTier_ProjectWatcherKeepsEveryEvent(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	owner := createNotificationMember(t, "bystander-project-owner@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-project-owner@multica.ai") })
	watcher := createNotificationMember(t, "bystander-project-watcher@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-project-watcher@multica.ai") })
	bystander := createNotificationMember(t, "bystander-project-plain@multica.ai")
	t.Cleanup(func() { cleanupTestUser(t, "bystander-project-plain@multica.ai") })

	projectID := createTestProject(t)
	f := workspaceFixture(t)
	issueID := f.Issue(t, "bystander tier issue", testutil.Cols{
		"assignee_type": "member",
		"assignee_id":   owner,
		"project_id":    projectID,
	})
	t.Cleanup(func() { cleanupInboxForIssue(t, issueID) })
	addProjectSubscribers(t, projectID, watcher)
	addTestSubscriber(t, issueID, "member", watcher, "commenter")
	addTestSubscriber(t, issueID, "member", bystander, "commenter")

	content := "a routine update"
	commentID := f.Comment(t, issueID, content, testutil.Cols{"author_id": owner})
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     owner,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         commentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   owner,
				Content:    content,
				Type:       "comment",
			},
			"issue_title":  "bystander tier issue",
			"issue_status": "in_progress",
		},
	})
	publishStatusChange(bus, issueID, "in_progress", "todo")

	if got := inboxTypes(t, queries, watcher); len(got) != 2 {
		t.Errorf("project watcher: inbox %v, want the comment and the status change once each", got)
	}
	if got := inboxCount(t, queries, bystander); got != 0 {
		t.Errorf("plain bystander: %d inbox items, want 0", got)
	}
}
