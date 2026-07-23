package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestAudienceSplitMigrationCopiesExistingEffectiveChoices(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var userID string
	legacyPreferences := `{
		"notifications": {
			"inbox": {"status_changed": "on"},
			"mobile": {"new_comment": "off"},
			"desktop": {"agent_comment_no_tag": "invalid"}
		}
	}`
	if err := tx.QueryRow(ctx,
		`INSERT INTO "user" (name, email, preferences)
		 VALUES ('Notification split migration', 'notif-split-migration@multica.ai', $1::jsonb)
		 RETURNING id::text`,
		legacyPreferences,
	).Scan(&userID); err != nil {
		t.Fatalf("create migration fixture: %v", err)
	}

	migration, err := os.ReadFile("../../migrations/9154_cerebro_notification_audience_split.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := tx.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("run migration: %v", err)
	}
	if _, err := tx.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("re-run migration: %v", err)
	}

	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT preferences FROM "user" WHERE id = $1`, userID).Scan(&raw); err != nil {
		t.Fatalf("read migrated preferences: %v", err)
	}
	var preferences map[string]any
	if err := json.Unmarshal(raw, &preferences); err != nil {
		t.Fatalf("decode migrated preferences: %v", err)
	}
	notifications := preferences["notifications"].(map[string]any)
	choice := func(channel, key string) string {
		t.Helper()
		return notifications[channel].(map[string]any)[key].(string)
	}

	for notificationType := range cerebroAudienceSplitTypes {
		for _, channel := range allChannels {
			want := "off"
			if channelDefault(channel, notificationType) {
				want = "on"
			}
			if channel == channelInbox && notificationType == "status_changed" {
				want = "on"
			}
			if channel == channelMobile && notificationType == "new_comment" {
				want = "off"
			}
			for _, audience := range []string{"assignee", "follower"} {
				key := notificationType + "." + audience
				if got := choice(channel, key); got != want {
					t.Errorf("migrated %s.%s = %q, want effective legacy choice %q", channel, key, got, want)
				}
			}
		}
	}
	if got := choice(channelMobile, "new_comment"); got != "off" {
		t.Errorf("legacy base choice changed to %q", got)
	}

	downMigration, err := os.ReadFile("../../migrations/9154_cerebro_notification_audience_split.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if _, err := tx.Exec(ctx, string(downMigration)); err != nil {
		t.Fatalf("run down migration: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT preferences FROM "user" WHERE id = $1`, userID).Scan(&raw); err != nil {
		t.Fatalf("read rolled-back preferences: %v", err)
	}
	preferences = nil
	if err := json.Unmarshal(raw, &preferences); err != nil {
		t.Fatalf("decode rolled-back preferences: %v", err)
	}
	mobile := preferences["notifications"].(map[string]any)[channelMobile].(map[string]any)
	if got := mobile["new_comment"]; got != "off" {
		t.Errorf("rolled-back legacy base choice = %v, want off", got)
	}
	if _, exists := mobile["new_comment.assignee"]; exists {
		t.Errorf("rolled-back preferences still contain new_comment.assignee")
	}
}

func TestCerebroAudienceSplitRouteKeys(t *testing.T) {
	for _, notificationType := range []string{
		"new_comment",
		"status_changed",
		"agent_comment_no_tag",
		"agent_comment_member_tag",
		"agent_comment_agent_tag",
	} {
		t.Run(notificationType, func(t *testing.T) {
			if got, want := routeKey(notificationType, true), fmt.Sprintf("%s.assignee", notificationType); got != want {
				t.Fatalf("assignee route key = %q, want %q", got, want)
			}
			if got, want := routeKey(notificationType, false), fmt.Sprintf("%s.follower", notificationType); got != want {
				t.Fatalf("follower route key = %q, want %q", got, want)
			}
		})
	}
}

func TestCerebroAudienceSplitDefaults(t *testing.T) {
	for notificationType := range cerebroAudienceSplitTypes {
		t.Run(notificationType, func(t *testing.T) {
			for _, channel := range allChannels {
				assigneeKey := notificationType + ".assignee"
				if got, want := channelDefault(channel, assigneeKey), channelDefault(channel, notificationType); got != want {
					t.Errorf("%s assignee default = %v, want legacy default %v", channel, got, want)
				}
			}

			if !channelDefault(channelInbox, notificationType+".follower") {
				t.Errorf("follower inbox default = off, want on")
			}
			for _, channel := range []string{channelMobile, channelDesktop} {
				if channelDefault(channel, notificationType+".follower") {
					t.Errorf("follower %s default = on, want off", channel)
				}
			}
			for _, channel := range []string{channelNotifications, channelMail} {
				if got, want := channelDefault(channel, notificationType+".follower"), channelDefault(channel, notificationType); got != want {
					t.Errorf("follower %s default = %v, want legacy default %v", channel, got, want)
				}
			}

			if got, want := primaryChannelForKey(notificationType+".assignee"), primaryChannelForKey(notificationType); got != want {
				t.Errorf("assignee primary channel = %q, want legacy channel %q", got, want)
			}
			if got := primaryChannelForKey(notificationType + ".follower"); got != channelInbox {
				t.Errorf("follower primary channel = %q, want %q", got, channelInbox)
			}
		})
	}
}

func TestCommentPushUsesAssigneeAndFollowerPreferences(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	commenterEmail := "notif-split-commenter@multica.ai"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	assigneeEmail := "notif-split-assignee@multica.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, assigneeEmail)
		clearUserPrefs(t, assigneeID)
	})

	followerEmail := "notif-split-follower@multica.ai"
	followerID := createTestUser(t, followerEmail)
	t.Cleanup(func() {
		cleanupTestUser(t, followerEmail)
		clearUserPrefs(t, followerID)
	})

	issueID := createTestIssue(t, testWorkspaceID, commenterID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE issue SET assignee_type = 'member', assignee_id = $1 WHERE id = $2`,
		assigneeID, issueID,
	); err != nil {
		t.Fatalf("set assignee: %v", err)
	}

	addTestSubscriber(t, issueID, "member", assigneeID, "assignee")
	addTestSubscriber(t, issueID, "member", followerID, "creator")
	setUserChannelPrefs(t, assigneeID, map[string]map[string]string{
		channelInbox:         {"new_comment.assignee": "on"},
		channelNotifications: {"new_comment.assignee": "off"},
		channelMobile:        {"new_comment.assignee": "on"},
	}, nil)
	setUserChannelPrefs(t, followerID, map[string]map[string]string{
		channelInbox:         {"new_comment.follower": "on"},
		channelNotifications: {"new_comment.follower": "off"},
		channelMobile:        {"new_comment.follower": "off"},
	}, nil)

	pushCapture := &capturePushNotifier{}
	previousPushNotifier := pushNotifier
	pushNotifier = pushCapture
	t.Cleanup(func() { pushNotifier = previousPushNotifier })

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     commenterID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000003650",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   commenterID,
				Content:    stringPtr("split push test"),
				Type:       "comment",
			},
			"issue_title":  "audience split",
			"issue_status": "todo",
		},
	})

	userIDs, pushes := pushCapture.snapshot()
	if len(pushes) != 1 {
		t.Fatalf("mobile pushes = %d, want only the assignee push: %+v", len(pushes), pushes)
	}
	if userIDs[0] != assigneeID || pushes[0].Type != "new_comment" {
		t.Fatalf("mobile push = user %q type %q, want assignee %q new_comment", userIDs[0], pushes[0].Type, assigneeID)
	}
}
