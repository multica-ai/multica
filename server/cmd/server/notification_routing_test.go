package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// setUserNotifPrefs replaces the user's preferences.notifications block
// wholesale. Each test should clear via t.Cleanup so prefs don't leak.
func setUserNotifPrefs(t *testing.T, userID string, notif map[string]string) {
	t.Helper()
	blob := map[string]any{}
	if len(notif) > 0 {
		nm := map[string]any{}
		for k, v := range notif {
			nm[k] = v
		}
		blob["notifications"] = nm
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal prefs: %v", err)
	}
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE "user" SET preferences = $1::jsonb WHERE id = $2`,
		raw, userID,
	); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
}

func clearUserPrefs(t *testing.T, userID string) {
	t.Helper()
	testPool.Exec(
		context.Background(),
		`UPDATE "user" SET preferences = '{}'::jsonb WHERE id = $1`,
		userID,
	)
}

// inboxItemsByRoute returns all non-archived inbox items for a recipient
// matching the given route. Uses raw SQL because the generated query
// ListInboxItems intentionally doesn't filter by route.
func inboxItemsByRoute(t *testing.T, recipientID, route string) []struct {
	Type  string
	Route string
} {
	t.Helper()
	rows, err := testPool.Query(
		context.Background(),
		`SELECT type, route FROM inbox_item
		 WHERE workspace_id = $1 AND recipient_type = 'member'
		   AND recipient_id = $2 AND archived = false AND route = $3
		 ORDER BY created_at`,
		testWorkspaceID, recipientID, route,
	)
	if err != nil {
		t.Fatalf("query inbox by route: %v", err)
	}
	defer rows.Close()
	var out []struct{ Type, Route string }
	for rows.Next() {
		var typ, rt string
		if err := rows.Scan(&typ, &rt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, struct{ Type, Route string }{typ, rt})
	}
	return out
}

// ----------- resolveRoute unit tests ------------

func TestResolveRoute_DefaultsForNonSplitTypes(t *testing.T) {
	queries := db.New(testPool)
	clearUserPrefs(t, testUserID)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	cases := []struct {
		notifType string
		want      string
	}{
		{"issue_assigned", routeInbox},
		{"mentioned", routeInbox},
		{"task_failed", routeInbox},
		{"unassigned", routeNotifications},
		{"reaction_added", routeNotifications},
		{"new_comment", routeNotifications},
		{"assignee_changed", routeNotifications},
		{"status_changed", routeNotifications},
	}
	for _, tc := range cases {
		got := resolveRoute(context.Background(), queries, "member", testUserID, tc.notifType, false)
		if got != tc.want {
			t.Errorf("resolveRoute(%q) = %q, want %q", tc.notifType, got, tc.want)
		}
	}
}

func TestResolveRoute_DefaultsForSplitTypes(t *testing.T) {
	queries := db.New(testPool)
	clearUserPrefs(t, testUserID)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	cases := []struct {
		notifType  string
		isAssignee bool
		want       string
	}{
		{"due_date_changed", true, routeInbox},
		{"due_date_changed", false, routeNotifications},
		{"priority_changed", true, routeInbox},
		{"priority_changed", false, routeNotifications},
	}
	for _, tc := range cases {
		got := resolveRoute(context.Background(), queries, "member", testUserID, tc.notifType, tc.isAssignee)
		if got != tc.want {
			t.Errorf("resolveRoute(%q, isAssignee=%v) = %q, want %q",
				tc.notifType, tc.isAssignee, got, tc.want)
		}
	}
}

func TestResolveRoute_HonoursUserOverride(t *testing.T) {
	queries := db.New(testPool)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	setUserNotifPrefs(t, testUserID, map[string]string{
		"reaction_added":            "off",
		"new_comment":               "inbox",
		"due_date_changed.assignee": "notifications",
		"due_date_changed.follower": "off",
	})

	cases := []struct {
		notifType  string
		isAssignee bool
		want       string
	}{
		{"reaction_added", false, routeOff},
		{"new_comment", false, routeInbox},
		{"due_date_changed", true, routeNotifications},
		{"due_date_changed", false, routeOff},
		// Untouched type still falls back to default.
		{"status_changed", false, routeNotifications},
	}
	for _, tc := range cases {
		got := resolveRoute(context.Background(), queries, "member", testUserID, tc.notifType, tc.isAssignee)
		if got != tc.want {
			t.Errorf("resolveRoute(%q, isAssignee=%v) = %q, want %q",
				tc.notifType, tc.isAssignee, got, tc.want)
		}
	}
}

func TestResolveRoute_AgentsAlwaysInbox(t *testing.T) {
	queries := db.New(testPool)
	clearUserPrefs(t, testUserID)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	// Even types whose default is "notifications" should land in inbox for
	// agent recipients — agents have no preferences UI and inbox is the only
	// surface they read from.
	for _, notifType := range []string{"new_comment", "status_changed", "issue_assigned"} {
		got := resolveRoute(context.Background(), queries, "agent", testUserID, notifType, false)
		if got != routeInbox {
			t.Errorf("agent route for %q = %q, want %q", notifType, got, routeInbox)
		}
	}
}

func TestResolveRoute_InvalidOverrideFallsBackToDefault(t *testing.T) {
	queries := db.New(testPool)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	setUserNotifPrefs(t, testUserID, map[string]string{
		"new_comment": "garbage-value",
	})

	got := resolveRoute(context.Background(), queries, "member", testUserID, "new_comment", false)
	if got != routeNotifications {
		t.Errorf("invalid override fall-through: got %q, want %q (default)", got, routeNotifications)
	}
}

// ----------- listener integration: preferences shape inbox_item.route ------------

// TestRouting_OffPreventsItemCreation verifies that a user routing
// 'reaction_added' to 'off' gets zero inbox items when the event fires.
func TestRouting_OffPreventsItemCreation(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	reactorEmail := "notif-route-reactor@multica.ai"
	reactorID := createTestUser(t, reactorEmail)
	t.Cleanup(func() { cleanupTestUser(t, reactorEmail) })

	creatorEmail := "notif-route-creator@multica.ai"
	creatorID := createTestUser(t, creatorEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, creatorEmail)
		clearUserPrefs(t, creatorID)
	})

	setUserNotifPrefs(t, creatorID, map[string]string{
		"reaction_added": "off",
	})

	issueID := createTestIssue(t, testWorkspaceID, creatorID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventIssueReactionAdded,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     reactorID,
		Payload: map[string]any{
			"reaction":     handler.IssueReactionResponse{Emoji: "👍"},
			"creator_type": "member",
			"creator_id":   creatorID,
			"issue_id":     issueID,
			"issue_title":  "off-test",
			"issue_status": "todo",
		},
	})

	items := inboxItemsForRecipient(t, queries, creatorID)
	if len(items) != 0 {
		t.Fatalf("expected 0 items (route=off), got %d", len(items))
	}
}

// TestRouting_StatusChangedDefaultsToNotifications verifies that the default
// route for a non-overridden subscriber lands on the 'notifications' page.
func TestRouting_StatusChangedDefaultsToNotifications(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	subEmail := "notif-route-sub@multica.ai"
	subID := createTestUser(t, subEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, subEmail)
		clearUserPrefs(t, subID)
	})

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", subID, "creator")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "default-route",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"status_changed": true,
			"prev_status":    "todo",
		},
	})

	notifs := inboxItemsByRoute(t, subID, routeNotifications)
	if len(notifs) != 1 || notifs[0].Type != "status_changed" {
		t.Fatalf("expected 1 notifications-routed status_changed, got %+v", notifs)
	}
	inbox := inboxItemsByRoute(t, subID, routeInbox)
	if len(inbox) != 0 {
		t.Fatalf("expected 0 inbox-routed items, got %+v", inbox)
	}
}

// TestRouting_PreferenceLandsInInbox verifies that a user who routed
// 'status_changed' to 'inbox' receives the item in the inbox.
func TestRouting_PreferenceLandsInInbox(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	subEmail := "notif-route-pref-inbox@multica.ai"
	subID := createTestUser(t, subEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, subEmail)
		clearUserPrefs(t, subID)
	})

	setUserNotifPrefs(t, subID, map[string]string{
		"status_changed": "inbox",
	})

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", subID, "creator")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "pref-inbox",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"status_changed": true,
			"prev_status":    "todo",
		},
	})

	inbox := inboxItemsByRoute(t, subID, routeInbox)
	if len(inbox) != 1 || inbox[0].Type != "status_changed" {
		t.Fatalf("expected 1 inbox-routed status_changed, got %+v", inbox)
	}
	notifs := inboxItemsByRoute(t, subID, routeNotifications)
	if len(notifs) != 0 {
		t.Fatalf("expected 0 notifications-routed items, got %+v", notifs)
	}
}

// TestRouting_PriorityChangedSplitsAssigneeAndFollower verifies that the
// assignee gets the .assignee key (default inbox) and a non-assignee
// subscriber gets the .follower key (default notifications) — even when
// both are subscribers of the same issue and neither has an override.
func TestRouting_PriorityChangedSplitsAssigneeAndFollower(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	assigneeEmail := "notif-route-assignee@multica.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, assigneeEmail)
		clearUserPrefs(t, assigneeID)
	})

	followerEmail := "notif-route-follower@multica.ai"
	followerID := createTestUser(t, followerEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, followerEmail)
		clearUserPrefs(t, followerID)
	})

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Set the issue's assignee to assigneeID so the listener computes
	// assigneeMemberID correctly.
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE issue SET assignee_type = 'member', assignee_id = $1 WHERE id = $2`,
		assigneeID, issueID,
	); err != nil {
		t.Fatalf("set assignee: %v", err)
	}

	addTestSubscriber(t, issueID, "member", assigneeID, "assignee")
	addTestSubscriber(t, issueID, "member", followerID, "creator")

	assigneeIDStr := assigneeID
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID, // actor is testUserID, not the assignee/follower
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "split-test",
				Status:       "todo",
				Priority:     "urgent",
				AssigneeType: stringPtr("member"),
				AssigneeID:   &assigneeIDStr,
				CreatorType:  "member",
				CreatorID:    testUserID,
			},
			"priority_changed": true,
			"prev_priority":    "medium",
		},
	})

	// Assignee gets it on the inbox route (.assignee default).
	asInbox := inboxItemsByRoute(t, assigneeID, routeInbox)
	if len(asInbox) != 1 || asInbox[0].Type != "priority_changed" {
		t.Fatalf("assignee: expected 1 inbox priority_changed, got %+v", asInbox)
	}
	asNotif := inboxItemsByRoute(t, assigneeID, routeNotifications)
	if len(asNotif) != 0 {
		t.Fatalf("assignee: expected 0 notifications items, got %+v", asNotif)
	}

	// Follower gets it on the notifications route (.follower default).
	foNotif := inboxItemsByRoute(t, followerID, routeNotifications)
	if len(foNotif) != 1 || foNotif[0].Type != "priority_changed" {
		t.Fatalf("follower: expected 1 notifications priority_changed, got %+v", foNotif)
	}
	foInbox := inboxItemsByRoute(t, followerID, routeInbox)
	if len(foInbox) != 0 {
		t.Fatalf("follower: expected 0 inbox items, got %+v", foInbox)
	}
}

// TestRouting_MentionBumping verifies that a comment with @-mention emits
// TWO inbox items for the mentioned user — one new_comment (route by
// new_comment pref) and one mentioned (route by mentioned pref) — so the
// "@-mention bumps to inbox" UX works regardless of new_comment's route.
func TestRouting_MentionBumping(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	commenterEmail := "notif-route-commenter@multica.ai"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	mentionedEmail := "notif-route-mentioned@multica.ai"
	mentionedID := createTestUser(t, mentionedEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, mentionedEmail)
		clearUserPrefs(t, mentionedID)
	})

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Mentioned user is subscribed to the issue (so new_comment fires for them
	// alongside the mention).
	addTestSubscriber(t, issueID, "member", mentionedID, "creator")

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     commenterID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000099",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   commenterID,
				Content:    "hey [@user](mention://member/" + mentionedID + ") take a look",
				Type:       "comment",
			},
			"issue_title":  "mention-bump",
			"issue_status": "todo",
		},
	})

	// Default routing: new_comment → notifications, mentioned → inbox. Both
	// items should exist on their respective routes.
	notifs := inboxItemsByRoute(t, mentionedID, routeNotifications)
	inbox := inboxItemsByRoute(t, mentionedID, routeInbox)

	hasType := func(items []struct{ Type, Route string }, typ string) bool {
		for _, i := range items {
			if i.Type == typ {
				return true
			}
		}
		return false
	}

	if !hasType(notifs, "new_comment") {
		t.Errorf("expected new_comment on notifications route, got: notifs=%+v", notifs)
	}
	if !hasType(inbox, "mentioned") {
		t.Errorf("expected mentioned on inbox route, got: inbox=%+v", inbox)
	}
}

// stringPtr is a tiny convenience for *string literals in test payloads.
func stringPtr(s string) *string { return &s }

// guard: ensure the routing key shape stays in sync with what the routing
// helper expects — catching any future rename of split keys.
func TestRouteKey_FormatStaysStable(t *testing.T) {
	if k := routeKey("status_changed", true); k != "status_changed" {
		t.Errorf("non-split should ignore isAssignee, got %q", k)
	}
	if k := routeKey("due_date_changed", true); !strings.HasSuffix(k, ".assignee") {
		t.Errorf("split assignee suffix changed, got %q", k)
	}
	if k := routeKey("due_date_changed", false); !strings.HasSuffix(k, ".follower") {
		t.Errorf("split follower suffix changed, got %q", k)
	}
}
