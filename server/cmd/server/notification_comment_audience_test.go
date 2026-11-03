package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestNotification_CommentRoutedByAudience covers MUL-8541: a comment lands in
// Needs action for a subscriber it @mentions or when it @mentions nobody, and
// in Activity for a subscriber it does not @mention while it @mentions someone
// else. Who wrote the comment does not matter, so every case is posted by an
// agent, the author whose replies used to default to Activity.
func TestNotification_CommentRoutedByAudience(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load agent: %v", err)
	}

	suffix := uuid.NewString()[:8]
	member := func(name string) string {
		email := fmt.Sprintf("notif-audience-%s-%s@multica.ai", name, suffix)
		id := createNotificationMember(t, email)
		t.Cleanup(func() { cleanupTestUser(t, email) })
		return id
	}
	target := member("target")
	bystander := member("bystander")
	optedIn := member("opted-in")
	if _, err := testPool.Exec(ctx, `
		INSERT INTO notification_preference (workspace_id, user_id, preferences)
		VALUES ($1, $2, '{"comments_mentioning_others":"action"}'::jsonb)
	`, testWorkspaceID, optedIn); err != nil {
		t.Fatalf("seed preference: %v", err)
	}

	// A squad with target as its only human member.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4) RETURNING id
	`, testWorkspaceID, "Audience squad "+suffix, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'member', $2), ($1, 'agent', $3)
	`, squadID, target, agentID); err != nil {
		t.Fatalf("add squad members: %v", err)
	}

	mentionMember := func(id string) string { return fmt.Sprintf("[@M](mention://member/%s)", id) }
	cases := []struct {
		name    string
		content string
		// want is the new_comment section per subscriber.
		want map[string]string
		// mentioned lists who also gets a "mentioned" row.
		mentioned []string
	}{
		{
			name:    "nobody mentioned",
			content: "Done, the PR is up.",
			want:    map[string]string{target: "action", bystander: "action", optedIn: "action"},
		},
		{
			name:    "issue link is not a mention",
			content: "Related to [MUL-1](mention://issue/" + uuid.NewString() + ").",
			want:    map[string]string{target: "action", bystander: "action", optedIn: "action"},
		},
		{
			name:      "member mentioned",
			content:   mentionMember(target) + " can you confirm?",
			want:      map[string]string{target: "action", bystander: "activity", optedIn: "action"},
			mentioned: []string{target},
		},
		{
			// The mention parser accepts these forms; they must still reach
			// the member they name.
			name:      "member mentioned in uppercase",
			content:   fmt.Sprintf("[@M](mention://member/%s) can you confirm?", strings.ToUpper(target)),
			want:      map[string]string{target: "action", bystander: "activity", optedIn: "action"},
			mentioned: []string{target},
		},
		{
			name:      "member mentioned without hyphens",
			content:   fmt.Sprintf("[@M](mention://member/%s) can you confirm?", strings.ReplaceAll(target, "-", "")),
			want:      map[string]string{target: "action", bystander: "activity", optedIn: "action"},
			mentioned: []string{target},
		},
		{
			name:    "only an agent mentioned",
			content: fmt.Sprintf("[@A](mention://agent/%s) please take this", agentID),
			want:    map[string]string{target: "activity", bystander: "activity", optedIn: "action"},
		},
		{
			name:      "recipient and others mentioned",
			content:   mentionMember(target) + fmt.Sprintf(" and [@A](mention://agent/%s)", agentID),
			want:      map[string]string{target: "action", bystander: "activity", optedIn: "action"},
			mentioned: []string{target},
		},
		{
			name:      "squad reaches its human members",
			content:   fmt.Sprintf("[@S](mention://squad/%s) over to you", squadID),
			want:      map[string]string{target: "action", bystander: "activity", optedIn: "action"},
			mentioned: []string{target},
		},
		{
			name:      "@all reaches everyone",
			content:   "[@all](mention://all/all) heads up",
			want:      map[string]string{target: "action", bystander: "action", optedIn: "action"},
			mentioned: []string{target, bystander, optedIn},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issueID := createTestIssue(t, testWorkspaceID, testUserID)
			t.Cleanup(func() {
				cleanupInboxForIssue(t, issueID)
				cleanupTestIssue(t, issueID)
			})
			for _, id := range []string{target, bystander, optedIn} {
				addTestSubscriber(t, issueID, "member", id, "commenter")
			}

			bus.Publish(events.Event{
				Type:        protocol.EventCommentCreated,
				WorkspaceID: testWorkspaceID,
				ActorType:   "agent",
				ActorID:     agentID,
				Payload: map[string]any{
					"comment": handler.CommentResponse{
						ID:         uuid.NewString(),
						IssueID:    issueID,
						AuthorType: "agent",
						AuthorID:   agentID,
						Content:    c.content,
						Type:       "comment",
					},
					"issue_title":  "audience test issue",
					"issue_status": "todo",
				},
			})

			mentioned := map[string]bool{}
			for _, id := range c.mentioned {
				mentioned[id] = true
			}
			for id, want := range c.want {
				var comment, mention int
				for _, item := range inboxItemsForRecipient(t, queries, id) {
					if util.UUIDToString(item.IssueID) != issueID {
						continue
					}
					switch item.Type {
					case "new_comment":
						comment++
						if !item.Section.Valid || item.Section.String != want {
							t.Errorf("%s: new_comment section = %+v, want %q", id, item.Section, want)
						}
					case "mentioned":
						mention++
					}
				}
				if comment != 1 {
					t.Errorf("%s: new_comment rows = %d, want 1", id, comment)
				}
				if wantMention := map[bool]int{true: 1}[mentioned[id]]; mention != wantMention {
					t.Errorf("%s: mentioned rows = %d, want %d", id, mention, wantMention)
				}
			}
		})
	}
}

// TestCommentAudienceCanonicalizesMemberIDs guards the comparison behind
// commentAudience: subscriber IDs are canonical (lowercase, hyphenated), and a
// member mention written in another accepted form must match them, so the
// comment follows the member's mentions choice instead of landing in
// Activity as if it were written to someone else.
func TestCommentAudienceCanonicalizesMemberIDs(t *testing.T) {
	const member = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	for _, id := range []string{member, strings.ToUpper(member), strings.ReplaceAll(member, "-", "")} {
		mentioned := expandMentionedMembers(context.Background(), nil, "",
			parseMentions(fmt.Sprintf("[@M](mention://member/%s) please look", id)))
		if got := mentioned.commentAudience(member); got != inbox.CommentToRecipient {
			t.Errorf("mention of %q: audience = %v, want CommentToRecipient", id, got)
		}
		section, ok := inbox.RouteComment(map[string]string{"mentions": "muted"}, mentioned.commentAudience(member))
		if ok {
			t.Errorf("mention of %q with mentions muted: route = (%q, delivered), want muted", id, section)
		}
	}
}
