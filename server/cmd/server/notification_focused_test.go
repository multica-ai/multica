package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const focusRecipient = "11111111-1111-1111-1111-111111111111"

// stubFocusFacts is a fixed inboxFocusFacts for the pure decision tests.
type stubFocusFacts struct {
	assigneeType, assigneeID string
	status                   string
	actor                    string
	commentAuthor            string
	rootAuthorType           string
	rootAuthorID             string
	reply                    bool
	directMention            bool
}

func (s stubFocusFacts) targetAssignee() (string, string) { return s.assigneeType, s.assigneeID }
func (s stubFocusFacts) newStatus() string                { return s.status }
func (s stubFocusFacts) actorType() string                { return s.actor }
func (s stubFocusFacts) commentAuthorType() string        { return s.commentAuthor }
func (s stubFocusFacts) threadRootAuthor() (string, string, bool) {
	return s.rootAuthorType, s.rootAuthorID, s.reply
}
func (s stubFocusFacts) directCommentMention(string) bool { return s.directMention }

// failingFocusFacts proves all mode never consults (and so never resolves) facts.
type failingFocusFacts struct{ t *testing.T }

func (f failingFocusFacts) fail() { f.t.Helper(); f.t.Fatal("all mode consulted focus facts") }
func (f failingFocusFacts) targetAssignee() (string, string) {
	f.fail()
	return "", ""
}
func (f failingFocusFacts) newStatus() string         { f.fail(); return "" }
func (f failingFocusFacts) actorType() string         { f.fail(); return "" }
func (f failingFocusFacts) commentAuthorType() string { f.fail(); return "" }
func (f failingFocusFacts) threadRootAuthor() (string, string, bool) {
	f.fail()
	return "", "", false
}
func (f failingFocusFacts) directCommentMention(string) bool { f.fail(); return false }

func TestShouldDeliverInboxAllModeMatchesIsNotifMuted(t *testing.T) {
	types := []string{"reaction_added", "quick_create_done", "children_done"}
	for notifType := range notifTypeToGroup {
		types = append(types, notifType)
	}
	prefSets := []map[string]string{
		nil,
		{},
		{"inbox_mode": "all"},
		{"inbox_mode": "unexpected"},
		{"comments": "muted", "status_changes": "muted"},
		{"assignments": "muted", "mentions": "muted", "updates": "muted", "agent_activity": "muted"},
		{
			"inbox_mode": "all", "focused_assigned": "off", "focused_blocked": "off",
			"focused_mentioned": "off", "focused_thread_reply": "off", "focused_agent_activity": "off",
		},
	}
	for i, prefs := range prefSets {
		for _, notifType := range types {
			want := !isNotifMuted(prefs, notifType)
			if got := shouldDeliverInbox(prefs, notifType, focusRecipient, failingFocusFacts{t}); got != want {
				t.Fatalf("prefs[%d]=%v type=%s: got %v, want %v", i, prefs, notifType, got, want)
			}
		}
	}
}

func TestShouldDeliverInboxFocusedRules(t *testing.T) {
	const other = "22222222-2222-2222-2222-222222222222"
	focused := func(extra ...string) map[string]string {
		prefs := map[string]string{"inbox_mode": "focused"}
		for i := 0; i+1 < len(extra); i += 2 {
			prefs[extra[i]] = extra[i+1]
		}
		return prefs
	}
	mine := stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient}

	cases := []struct {
		name      string
		prefs     map[string]string
		notifType string
		facts     stubFocusFacts
		want      bool
	}{
		// issue_assigned
		{"assigned delivers", focused(), "issue_assigned", stubFocusFacts{}, true},
		{"assigned ignores group mute", focused("assignments", "muted"), "issue_assigned", stubFocusFacts{}, true},
		{"assigned off drops", focused("focused_assigned", "off"), "issue_assigned", stubFocusFacts{}, false},
		{"unassigned drops", focused(), "unassigned", mine, false},
		{"assignee_changed drops", focused(), "assignee_changed", mine, false},

		// mentioned
		{"direct comment mention delivers", focused(), "mentioned", stubFocusFacts{directMention: true}, true},
		{"mention ignores group mute", focused("mentions", "muted"), "mentioned", stubFocusFacts{directMention: true}, true},
		{"indirect or description mention drops", focused(), "mentioned", stubFocusFacts{}, false},
		{"mentioned off drops", focused("focused_mentioned", "off"), "mentioned", stubFocusFacts{directMention: true}, false},

		// status_changed
		{"blocked on my issue delivers", focused(), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient, status: "blocked", actor: "member"}, true},
		{"blocked ignores group mute", focused("status_changes", "muted"), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient, status: "blocked", actor: "member"}, true},
		{"blocked off drops", focused("focused_blocked", "off"), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient, status: "blocked", actor: "member"}, false},
		{"blocked on someone else's issue drops", focused(), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: other, status: "blocked", actor: "agent"}, false},
		{"blocked on agent-assigned issue drops", focused(), "status_changed",
			stubFocusFacts{assigneeType: "agent", assigneeID: focusRecipient, status: "blocked", actor: "agent"}, false},
		{"member status change on my issue drops", focused(), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient, status: "in_review", actor: "member"}, false},
		{"agent status change on my issue delivers", focused(), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient, status: "in_review", actor: "agent"}, true},
		{"agent activity off drops agent status change", focused("focused_agent_activity", "off"), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient, status: "in_review", actor: "agent"}, false},
		{"agent blocked still delivers with agent activity off", focused("focused_agent_activity", "off"), "status_changed",
			stubFocusFacts{assigneeType: "member", assigneeID: focusRecipient, status: "blocked", actor: "agent"}, true},

		// new_comment
		{"reply in my thread delivers", focused(), "new_comment",
			stubFocusFacts{commentAuthor: "member", reply: true, rootAuthorType: "member", rootAuthorID: focusRecipient}, true},
		{"reply ignores group mute", focused("comments", "muted"), "new_comment",
			stubFocusFacts{commentAuthor: "member", reply: true, rootAuthorType: "member", rootAuthorID: focusRecipient}, true},
		{"thread reply off drops", focused("focused_thread_reply", "off"), "new_comment",
			stubFocusFacts{commentAuthor: "member", reply: true, rootAuthorType: "member", rootAuthorID: focusRecipient}, false},
		{"reply in someone else's thread drops", focused(), "new_comment",
			stubFocusFacts{commentAuthor: "member", reply: true, rootAuthorType: "member", rootAuthorID: other}, false},
		{"top-level member comment on my issue drops", focused(), "new_comment",
			stubFocusFacts{commentAuthor: "member", assigneeType: "member", assigneeID: focusRecipient}, false},
		{"agent comment on my issue delivers", focused(), "new_comment",
			stubFocusFacts{commentAuthor: "agent", assigneeType: "member", assigneeID: focusRecipient}, true},
		{"agent comment on someone else's issue drops", focused(), "new_comment",
			stubFocusFacts{commentAuthor: "agent", assigneeType: "member", assigneeID: other}, false},
		{"agent activity off drops agent comment", focused("focused_agent_activity", "off"), "new_comment",
			stubFocusFacts{commentAuthor: "agent", assigneeType: "member", assigneeID: focusRecipient}, false},
		{"agent reply in my thread delivers with agent activity off", focused("focused_agent_activity", "off"), "new_comment",
			stubFocusFacts{commentAuthor: "agent", reply: true, rootAuthorType: "member", rootAuthorID: focusRecipient}, true},

		// types outside every rule
		{"priority change drops", focused(), "priority_changed", mine, false},
		{"due date change drops", focused(), "due_date_changed", mine, false},
		{"task_failed drops", focused(), "task_failed", mine, false},
		{"reaction drops", focused(), "reaction_added", mine, false},
		{"unfiltered type keeps delivering", focused(), "quick_create_done", stubFocusFacts{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldDeliverInbox(tc.prefs, tc.notifType, focusRecipient, tc.facts); got != tc.want {
				t.Fatalf("shouldDeliverInbox(%v, %q) = %v, want %v", tc.prefs, tc.notifType, got, tc.want)
			}
		})
	}
}

// ---- DB-backed acceptance tests ----------------------------------------------

const allFocusedRulesOff = `{"inbox_mode":"all","focused_assigned":"off","focused_blocked":"off",` +
	`"focused_mentioned":"off","focused_thread_reply":"off","focused_agent_activity":"off"}`

// focusMember creates a workspace member with the given stored preferences
// ("" for no preference row).
func focusMember(t *testing.T, fx *testutil.Fixture, label, prefs string) string {
	t.Helper()
	id := fx.User(t, "Focused "+label, fmt.Sprintf("focused-%s@multica.ai", label))
	fx.Member(t, testWorkspaceID, id, "member")
	if prefs != "" {
		fx.Insert(t, "notification_preference", testutil.Cols{
			"workspace_id": testWorkspaceID,
			"user_id":      id,
			"preferences":  prefs,
		})
	}
	return id
}

// focusIssue creates an issue (optionally member-assigned) whose inbox and
// subscriber rows are removed before the issue itself.
func focusIssue(t *testing.T, fx *testutil.Fixture, assigneeID string) string {
	t.Helper()
	cols := testutil.Cols{}
	if assigneeID != "" {
		cols = testutil.Cols{"assignee_type": "member", "assignee_id": assigneeID}
	}
	issueID := fx.Issue(t, "focused inbox issue", cols)
	fx.Cleanup(t, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
	fx.Cleanup(t, `DELETE FROM issue_subscriber WHERE issue_id = $1`, issueID)
	return issueID
}

func focusAgent(t *testing.T, fx *testutil.Fixture) string {
	t.Helper()
	return fx.Agent(t, "Focused inbox agent", fx.Runtime(t, "Focused inbox runtime"))
}

func countInbox(t *testing.T, recipientID, issueID, notifType string) int {
	t.Helper()
	return workspaceFixture(t).Count(t, `
		SELECT count(*) FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1 AND issue_id = $2 AND type = $3
	`, recipientID, issueID, notifType)
}

func publishMemberComment(bus *events.Bus, actorID, issueID, commentID, content string, parentID *string) {
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     actorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID: commentID, IssueID: issueID, AuthorType: "member", AuthorID: actorID,
				Content: content, Type: "comment", ParentID: parentID,
			},
			"issue_title":  "focused inbox issue",
			"issue_status": "todo",
		},
	})
}

// publishAgentComment mirrors TaskService.createAgentComment's map payload.
func publishAgentComment(bus *events.Bus, agentID, issueID, commentID, content string, parentID *string) {
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload: map[string]any{
			"comment": map[string]any{
				"id": commentID, "issue_id": issueID, "author_type": "agent", "author_id": agentID,
				"content": content, "type": "comment", "parent_id": parentID,
			},
			"issue_title":  "focused inbox issue",
			"issue_status": "todo",
		},
	})
}

func publishFocusStatusChange(bus *events.Bus, actorType, actorID, issueID, assigneeID, newStatus string) {
	issue := handler.IssueResponse{
		ID: issueID, WorkspaceID: testWorkspaceID, Title: "focused inbox issue",
		Status: newStatus, Priority: "none", CreatorType: "member", CreatorID: testUserID,
	}
	if assigneeID != "" {
		assigneeType := "member"
		issue.AssigneeType, issue.AssigneeID = &assigneeType, &assigneeID
	}
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload: map[string]any{
			"issue":          issue,
			"status_changed": true,
			"prev_status":    "todo",
		},
	})
}

func publishAssigned(bus *events.Bus, issueID, assigneeID string) {
	assigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID: issueID, WorkspaceID: testWorkspaceID, Title: "focused inbox issue",
				Status: "todo", Priority: "none", CreatorType: "member", CreatorID: testUserID,
				AssigneeType: &assigneeType, AssigneeID: &assigneeID,
			},
			"assignee_changed": true,
		},
	})
}

// The focused_* keys mean nothing in all mode: every existing group decision
// is unchanged.
func TestFocusedInbox_AllModeIgnoresFocusedRules(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)

	r := focusMember(t, fx, "allmode-r", allFocusedRulesOff)
	issueID := focusIssue(t, fx, "")
	addTestSubscriber(t, issueID, "member", r, "manual")

	commentID := fx.Comment(t, issueID, "plain comment")
	publishMemberComment(bus, testUserID, issueID, commentID, "plain comment", nil)
	publishFocusStatusChange(bus, "member", testUserID, issueID, "", "in_progress")

	if n := countInbox(t, r, issueID, "new_comment"); n != 1 {
		t.Fatalf("new_comment rows = %d, want 1", n)
	}
	if n := countInbox(t, r, issueID, "status_changed"); n != 1 {
		t.Fatalf("status_changed rows = %d, want 1", n)
	}
}

func TestFocusedInbox_MemberCommentSkipsPlainSubscriber(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)

	r := focusMember(t, fx, "subscriber-r", `{"inbox_mode":"focused"}`)
	control := focusMember(t, fx, "subscriber-control", "")
	issueID := focusIssue(t, fx, "")
	addTestSubscriber(t, issueID, "member", r, "manual")
	addTestSubscriber(t, issueID, "member", control, "manual")

	commentID := fx.Comment(t, issueID, "member comment")
	publishMemberComment(bus, testUserID, issueID, commentID, "member comment", nil)

	if n := countInbox(t, r, issueID, "new_comment"); n != 0 {
		t.Fatalf("focused subscriber got %d new_comment rows, want 0", n)
	}
	if n := countInbox(t, control, issueID, "new_comment"); n != 1 {
		t.Fatalf("all-mode subscriber got %d new_comment rows, want 1", n)
	}
}

func TestFocusedInbox_AgentCommentOnMyIssue(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)
	agentID := focusAgent(t, fx)

	r := focusMember(t, fx, "agentcomment-r", `{"inbox_mode":"focused"}`)
	issueID := focusIssue(t, fx, r)
	addTestSubscriber(t, issueID, "member", r, "assignee")

	commentID := fx.Comment(t, issueID, "agent result", testutil.Cols{"author_type": "agent", "author_id": agentID})
	publishAgentComment(bus, agentID, issueID, commentID, "agent result", nil)

	if n := countInbox(t, r, issueID, "new_comment"); n != 1 {
		t.Fatalf("assignee got %d new_comment rows, want 1", n)
	}
}

// Project watchers go through the same focused rules as issue subscribers:
// new issues and other members' comments are dropped, an agent comment on the
// watcher's own issue still arrives.
func TestFocusedInbox_ProjectWatcher(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)
	agentID := focusAgent(t, fx)

	r := focusMember(t, fx, "project-r", `{"inbox_mode":"focused"}`)
	control := focusMember(t, fx, "project-control", "")
	projectID := createTestProject(t)
	addProjectSubscribers(t, projectID, r, control)
	issueID := focusIssue(t, fx, r)
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID); err != nil {
		t.Fatalf("move issue into project: %v", err)
	}

	assigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{"issue": handler.IssueResponse{
			ID: issueID, WorkspaceID: testWorkspaceID, Title: "focused inbox issue",
			Status: "todo", Priority: "none", CreatorType: "member", CreatorID: testUserID,
			AssigneeType: &assigneeType, AssigneeID: &r, ProjectID: &projectID,
		}},
	})
	memberCommentID := fx.Comment(t, issueID, "member comment")
	publishMemberComment(bus, testUserID, issueID, memberCommentID, "member comment", nil)
	agentCommentID := fx.Comment(t, issueID, "agent result", testutil.Cols{"author_type": "agent", "author_id": agentID})
	publishAgentComment(bus, agentID, issueID, agentCommentID, "agent result", nil)

	if n := countInbox(t, control, issueID, "issue_created"); n != 1 {
		t.Fatalf("all-mode watcher got %d issue_created rows, want 1", n)
	}
	if n := countInbox(t, control, issueID, "new_comment"); n != 2 {
		t.Fatalf("all-mode watcher got %d new_comment rows, want 2", n)
	}
	if n := countInbox(t, r, issueID, "issue_created"); n != 0 {
		t.Fatalf("focused watcher got %d issue_created rows, want 0", n)
	}
	if n := countInbox(t, r, issueID, "new_comment"); n != 1 {
		t.Fatalf("focused watcher got %d new_comment rows, want 1 (the agent comment)", n)
	}
}

func TestFocusedInbox_StatusChangesOnMyIssue(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)
	agentID := focusAgent(t, fx)

	r := focusMember(t, fx, "status-r", `{"inbox_mode":"focused"}`)

	t.Run("member moves it to blocked", func(t *testing.T) {
		issueID := focusIssue(t, fx, r)
		addTestSubscriber(t, issueID, "member", r, "assignee")
		publishFocusStatusChange(bus, "member", testUserID, issueID, r, "blocked")
		if n := countInbox(t, r, issueID, "status_changed"); n != 1 {
			t.Fatalf("status_changed rows = %d, want 1", n)
		}
	})
	t.Run("member moves it elsewhere", func(t *testing.T) {
		issueID := focusIssue(t, fx, r)
		addTestSubscriber(t, issueID, "member", r, "assignee")
		publishFocusStatusChange(bus, "member", testUserID, issueID, r, "in_review")
		if n := countInbox(t, r, issueID, "status_changed"); n != 0 {
			t.Fatalf("status_changed rows = %d, want 0", n)
		}
	})
	t.Run("agent changes the status", func(t *testing.T) {
		issueID := focusIssue(t, fx, r)
		addTestSubscriber(t, issueID, "member", r, "assignee")
		publishFocusStatusChange(bus, "agent", agentID, issueID, r, "in_review")
		if n := countInbox(t, r, issueID, "status_changed"); n != 1 {
			t.Fatalf("status_changed rows = %d, want 1", n)
		}
	})
}

func TestFocusedInbox_ReplyInMyThread(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)

	r := focusMember(t, fx, "thread-r", `{"inbox_mode":"focused"}`)
	other := focusMember(t, fx, "thread-other", `{"inbox_mode":"focused"}`)
	issueID := focusIssue(t, fx, "")
	addTestSubscriber(t, issueID, "member", r, "commenter")
	addTestSubscriber(t, issueID, "member", other, "commenter")

	rootID := fx.Comment(t, issueID, "my question", testutil.Cols{"author_id": r})
	firstReply := fx.Comment(t, issueID, "first reply", testutil.Cols{"author_id": other, "parent_id": rootID})
	// A reply to a reply still belongs to R's thread.
	replyID := fx.Comment(t, issueID, "nested reply", testutil.Cols{"parent_id": firstReply})
	publishMemberComment(bus, testUserID, issueID, replyID, "nested reply", &firstReply)

	if n := countInbox(t, r, issueID, "new_comment"); n != 1 {
		t.Fatalf("thread root author got %d new_comment rows, want 1", n)
	}
	if n := countInbox(t, other, issueID, "new_comment"); n != 0 {
		t.Fatalf("non-root participant got %d new_comment rows, want 0", n)
	}
}

func TestFocusedInbox_DirectMentionOnly(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)

	direct := focusMember(t, fx, "mention-direct", `{"inbox_mode":"focused"}`)
	viaAll := focusMember(t, fx, "mention-all", `{"inbox_mode":"focused"}`)
	allModeMember := focusMember(t, fx, "mention-allmode", "")
	issueID := focusIssue(t, fx, "")

	content := "[@Direct](mention://member/" + direct + ") [@All](mention://all/all)"
	commentID := fx.Comment(t, issueID, content)
	publishMemberComment(bus, testUserID, issueID, commentID, content, nil)

	if n := countInbox(t, direct, issueID, "mentioned"); n != 1 {
		t.Fatalf("direct mention rows = %d, want 1", n)
	}
	if n := countInbox(t, viaAll, issueID, "mentioned"); n != 0 {
		t.Fatalf("@all-only focused member rows = %d, want 0", n)
	}
	if n := countInbox(t, allModeMember, issueID, "mentioned"); n != 1 {
		t.Fatalf("@all all-mode member rows = %d, want 1", n)
	}

	// A description mention is not a comment mention.
	descIssue := focusIssue(t, fx, "")
	description := "[@Direct](mention://member/" + direct + ")"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{"issue": handler.IssueResponse{
			ID: descIssue, WorkspaceID: testWorkspaceID, Title: "focused inbox issue",
			Status: "todo", Priority: "none", CreatorType: "member", CreatorID: testUserID,
			Description: &description,
		}},
	})
	if n := countInbox(t, direct, descIssue, "mentioned"); n != 0 {
		t.Fatalf("description mention rows = %d, want 0", n)
	}
}

func TestFocusedInbox_Assigned(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)

	on := focusMember(t, fx, "assigned-on", `{"inbox_mode":"focused","assignments":"muted"}`)
	off := focusMember(t, fx, "assigned-off", `{"inbox_mode":"focused","focused_assigned":"off"}`)

	onIssue := focusIssue(t, fx, "")
	publishAssigned(bus, onIssue, on)
	if n := countInbox(t, on, onIssue, "issue_assigned"); n != 1 {
		t.Fatalf("issue_assigned rows = %d, want 1", n)
	}

	offIssue := focusIssue(t, fx, "")
	publishAssigned(bus, offIssue, off)
	if n := countInbox(t, off, offIssue, "issue_assigned"); n != 0 {
		t.Fatalf("issue_assigned rows with focused_assigned=off = %d, want 0", n)
	}
}

// Switching modes filters new rows only; rows created earlier stay as they are.
func TestFocusedInbox_ModeChangeKeepsExistingRows(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)

	r := focusMember(t, fx, "modechange-r", `{"inbox_mode":"all"}`)
	issueID := focusIssue(t, fx, "")
	addTestSubscriber(t, issueID, "member", r, "manual")

	first := fx.Comment(t, issueID, "before the switch")
	publishMemberComment(bus, testUserID, issueID, first, "before the switch", nil)
	before := inboxItemsForRecipient(t, queries, r)
	if len(before) != 1 {
		t.Fatalf("rows before switch = %d, want 1", len(before))
	}

	fx.Exec(t, `
		UPDATE notification_preference SET preferences = '{"inbox_mode":"focused"}'
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, r)

	second := fx.Comment(t, issueID, "after the switch")
	publishMemberComment(bus, testUserID, issueID, second, "after the switch", nil)

	after := inboxItemsForRecipient(t, queries, r)
	if len(after) != 1 || util.UUIDToString(after[0].ID) != util.UUIDToString(before[0].ID) {
		t.Fatalf("rows after switch = %d, want the original row only", len(after))
	}
	if after[0].Read || after[0].Archived {
		t.Fatalf("existing row changed: read=%v archived=%v", after[0].Read, after[0].Archived)
	}
	if n := countInbox(t, r, issueID, "new_comment"); n != 1 {
		t.Fatalf("new_comment rows = %d, want 1", n)
	}
}

// Focused mode reads the thread root only for replies and the issue only when
// a rule could still match.
func TestLazyFocusFactsSkipsReadsForTopLevelComment(t *testing.T) {
	facts := newLazyFocusFacts(context.Background(), nil, events.Event{
		Type: protocol.EventCommentCreated,
		Payload: map[string]any{"comment": handler.CommentResponse{
			ID: "33333333-3333-3333-3333-333333333333", AuthorType: "member",
		}},
	}, "44444444-4444-4444-4444-444444444444", "todo")
	// nil queries: any read would panic.
	if shouldDeliverInbox(map[string]string{"inbox_mode": "focused"}, "new_comment", focusRecipient, facts) {
		t.Fatal("top-level member comment delivered in focused mode")
	}
}
