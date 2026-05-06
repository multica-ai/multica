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

// setUserChannelPrefs replaces preferences.notifications with the
// channel-first shape (one map per channel + optional channels transport).
// Used by the resolveChannelChoice / resolveChannelTransport tests.
func setUserChannelPrefs(t *testing.T, userID string, channels map[string]map[string]string, transport map[string]map[string]any) {
	t.Helper()
	notif := map[string]any{}
	for ch, m := range channels {
		nm := map[string]any{}
		for k, v := range m {
			nm[k] = v
		}
		notif[ch] = nm
	}
	if len(transport) > 0 {
		ch := map[string]any{}
		for k, v := range transport {
			ch[k] = v
		}
		notif["channels"] = ch
	}
	blob := map[string]any{"notifications": notif}
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

// setUserNotifPrefs replaces the user's preferences.notifications block
// using the legacy single-route input shape (inbox/notifications/off per key)
// translated into the channel-first storage format. Tests written before
// channel-first stays readable; the translation is exactly what migration
// 9011 does for live data:
//   "inbox"         -> inbox.<key>=on,  notifications.<key>=off
//   "notifications" -> inbox.<key>=off, notifications.<key>=on
//   "off"           -> inbox.<key>=off, notifications.<key>=off
//
// Mobile/desktop/mail are left to the resolver's default tables. Tests that
// need to touch those channels should use setUserChannelPrefs instead.
func setUserNotifPrefs(t *testing.T, userID string, notif map[string]string) {
	t.Helper()
	channels := map[string]map[string]string{
		channelInbox:         {},
		channelNotifications: {},
	}
	for k, v := range notif {
		switch v {
		case "inbox":
			channels[channelInbox][k] = "on"
			channels[channelNotifications][k] = "off"
		case "notifications":
			channels[channelInbox][k] = "off"
			channels[channelNotifications][k] = "on"
		case "off":
			channels[channelInbox][k] = "off"
			channels[channelNotifications][k] = "off"
		}
		// Unrecognised values fall through, leaving the resolver's default.
	}
	setUserChannelPrefs(t, userID, channels, nil)
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

// ----------- push payload truncation tests ------------

func TestTruncateForPush(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"empty", "", 200, ""},
		{"under limit", "Hello world", 200, "Hello world"},
		{"exact limit", "abcdef", 6, "abcdef"},
		{"over limit appends ellipsis", "abcdefghij", 5, "abcd…"},
		{"unicode-safe", "æøåabcdef", 5, "æøåa…"},
		{"zero max returns empty", "anything", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateForPush(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("truncateForPush(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

// ----------- channel-first resolver tests ------------

func TestResolveChannelChoice_DefaultsWhenNoPrefs(t *testing.T) {
	queries := db.New(testPool)
	clearUserPrefs(t, testUserID)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	cases := []struct {
		channel string
		key     string
		want    bool
	}{
		// Inbox defaults: strong personal signals on, low-noise off.
		{channelInbox, "issue_assigned", true},
		{channelInbox, "mentioned", true},
		{channelInbox, "new_comment", false},
		// Notifications defaults: low-noise on, strong signals off.
		{channelNotifications, "new_comment", true},
		{channelNotifications, "issue_assigned", false},
		// Mobile defaults: only conservative pushes.
		{channelMobile, "mentioned", true},
		{channelMobile, "task_failed", false},
		{channelMobile, "new_comment", false},
		// Desktop defaults: more permissive than mobile but still focused.
		{channelDesktop, "task_failed", true},
		{channelDesktop, "new_comment", false},
		// Mail: nothing by default until transport is built.
		{channelMail, "issue_assigned", false},
	}
	for _, tc := range cases {
		got := resolveChannelChoice(context.Background(), queries, "member", testUserID, tc.channel, tc.key)
		if got != tc.want {
			t.Errorf("resolveChannelChoice(%q, %q) = %v, want %v", tc.channel, tc.key, got, tc.want)
		}
	}
}

func TestResolveChannelChoice_HonoursOverride(t *testing.T) {
	queries := db.New(testPool)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	// User flips mobile.new_comment on (default is off) and inbox.issue_assigned
	// off (default is on).
	setUserChannelPrefs(t, testUserID, map[string]map[string]string{
		channelMobile: {"new_comment": "on"},
		channelInbox:  {"issue_assigned": "off"},
	}, nil)

	if got := resolveChannelChoice(context.Background(), queries, "member", testUserID, channelMobile, "new_comment"); !got {
		t.Errorf("override mobile.new_comment=on: got false, want true")
	}
	if got := resolveChannelChoice(context.Background(), queries, "member", testUserID, channelInbox, "issue_assigned"); got {
		t.Errorf("override inbox.issue_assigned=off: got true, want false")
	}
	// Untouched channel/key still falls back to default.
	if got := resolveChannelChoice(context.Background(), queries, "member", testUserID, channelInbox, "mentioned"); !got {
		t.Errorf("untouched key fall-through: got false, want true (default)")
	}
}

func TestResolveChannelChoice_AgentsOnlyInbox(t *testing.T) {
	queries := db.New(testPool)

	// Agents have no preferences UI; they should only see inbox channel as on,
	// regardless of the routing key or default tables.
	if got := resolveChannelChoice(context.Background(), queries, "agent", testUserID, channelInbox, "new_comment"); !got {
		t.Errorf("agent channelInbox.new_comment: got false, want true")
	}
	for _, ch := range []string{channelNotifications, channelMobile, channelDesktop, channelMail} {
		if got := resolveChannelChoice(context.Background(), queries, "agent", testUserID, ch, "issue_assigned"); got {
			t.Errorf("agent channel %q.issue_assigned: got true, want false", ch)
		}
	}
}

func TestResolveChannelChoice_InvalidValueFallsBackToDefault(t *testing.T) {
	queries := db.New(testPool)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	setUserChannelPrefs(t, testUserID, map[string]map[string]string{
		channelMobile: {"mentioned": "garbage"},
	}, nil)

	got := resolveChannelChoice(context.Background(), queries, "member", testUserID, channelMobile, "mentioned")
	if !got {
		t.Errorf("invalid mobile.mentioned override should fall back to default (true), got false")
	}
}

func TestResolveChannelTransport_DefaultsWhenNoPrefs(t *testing.T) {
	queries := db.New(testPool)
	clearUserPrefs(t, testUserID)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	mob := resolveChannelTransport(context.Background(), queries, "member", testUserID, channelMobile)
	if !mob.Badge || !mob.Sound {
		t.Errorf("mobile default transport: got %+v, want Badge+Sound on", mob)
	}

	desk := resolveChannelTransport(context.Background(), queries, "member", testUserID, channelDesktop)
	if !desk.Badge || !desk.Banner || desk.Sound {
		t.Errorf("desktop default transport: got %+v, want Badge+Banner on, Sound off", desk)
	}
}

func TestResolveChannelTransport_HonoursOverride(t *testing.T) {
	queries := db.New(testPool)
	t.Cleanup(func() { clearUserPrefs(t, testUserID) })

	setUserChannelPrefs(t, testUserID, nil, map[string]map[string]any{
		channelMobile:  {"badge": false, "sound": false},
		channelDesktop: {"banner": false},
	})

	mob := resolveChannelTransport(context.Background(), queries, "member", testUserID, channelMobile)
	if mob.Badge || mob.Sound {
		t.Errorf("mobile override: got %+v, want Badge+Sound off", mob)
	}

	desk := resolveChannelTransport(context.Background(), queries, "member", testUserID, channelDesktop)
	if desk.Banner {
		t.Errorf("desktop banner override: got banner=true, want false")
	}
	// Badge wasn't overridden — default still applies.
	if !desk.Badge {
		t.Errorf("desktop badge default leak: got badge=false, want true")
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
