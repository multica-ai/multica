package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Helper to build a pgtype.UUID from a string.
func testUUID(s string) pgtype.UUID {
	return parseUUID(s)
}

// Helper to build a pgtype.Text.
func testText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

const (
	agentAssigneeID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	otherAgentID    = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	memberID        = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	otherMemberID   = "dddddddd-dddd-dddd-dddd-dddddddddddd"
)

func issueWithAgentAssignee() db.Issue {
	return db.Issue{
		AssigneeType: testText("agent"),
		AssigneeID:   testUUID(agentAssigneeID),
	}
}

func issueNoAssignee() db.Issue {
	return db.Issue{}
}

// -------------------------------------------------------------------
// commentMentionsOthersButNotAssignee
// -------------------------------------------------------------------

func TestCommentMentionsOthersButNotAssignee(t *testing.T) {
	h := &Handler{} // nil handler — method doesn't use h

	issue := issueWithAgentAssignee()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no mentions → allow trigger",
			content: "just a plain comment",
			want:    false,
		},
		{
			name:    "mentions assignee → allow trigger",
			content: fmt.Sprintf("[@Agent](mention://agent/%s) please fix", agentAssigneeID),
			want:    false,
		},
		{
			name:    "mentions other agent only → suppress",
			content: fmt.Sprintf("[@Other](mention://agent/%s) what do you think?", otherAgentID),
			want:    true,
		},
		{
			name:    "mentions other member only → suppress",
			content: fmt.Sprintf("[@Bob](mention://member/%s) take a look", memberID),
			want:    true,
		},
		{
			name:    "mentions both assignee and other → allow trigger",
			content: fmt.Sprintf("[@Agent](mention://agent/%s) and [@Other](mention://agent/%s)", agentAssigneeID, otherAgentID),
			want:    false,
		},
		{
			name:    "@all mention → suppress (broadcast, not directed at agent)",
			content: "[@All](mention://all/all) heads up everyone",
			want:    true,
		},
		{
			name:    "@all with assignee mention → suppress (@all takes precedence)",
			content: fmt.Sprintf("[@All](mention://all/all) [@Agent](mention://agent/%s) fyi", agentAssigneeID),
			want:    true,
		},
		{
			name:    "issue mention only → allow trigger (cross-reference, not @person)",
			content: "[PAN-1](mention://issue/44c266e7-f6dd-4be3-9140-5ac40233f79c) is related",
			want:    false,
		},
		{
			name:    "issue mention + other agent → suppress (agent mention matters)",
			content: fmt.Sprintf("[PAN-1](mention://issue/44c266e7-f6dd-4be3-9140-5ac40233f79c) cc [@Other](mention://agent/%s)", otherAgentID),
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.commentMentionsOthersButNotAssignee(tt.content, issue)
			if got != tt.want {
				t.Errorf("commentMentionsOthersButNotAssignee() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommentMentionsOthersButNotAssignee_NoAssignee(t *testing.T) {
	h := &Handler{}
	issue := issueNoAssignee()

	// Any mention on an unassigned issue → suppress
	content := fmt.Sprintf("[@Agent](mention://agent/%s) help", otherAgentID)
	if got := h.commentMentionsOthersButNotAssignee(content, issue); !got {
		t.Errorf("expected true for mentions on unassigned issue, got false")
	}
}

// -------------------------------------------------------------------
// isReplyToMemberThread
// -------------------------------------------------------------------

func TestIsReplyToMemberThread(t *testing.T) {
	h := &Handler{}
	issue := issueWithAgentAssignee()

	memberParent := &db.Comment{AuthorType: "member", AuthorID: testUUID(memberID), Content: "plain thread starter"}
	agentParent := &db.Comment{AuthorType: "agent", AuthorID: testUUID(agentAssigneeID), Content: "agent thread starter"}
	// Member-started thread root that @mentions the assignee agent.
	memberParentMentioningAssignee := &db.Comment{
		AuthorType: "member",
		AuthorID:   testUUID(memberID),
		Content:    fmt.Sprintf("[@Agent](mention://agent/%s) can you look at this?", agentAssigneeID),
	}
	// Member-started thread root that @mentions a non-assignee agent.
	memberParentMentioningOther := &db.Comment{
		AuthorType: "member",
		AuthorID:   testUUID(memberID),
		Content:    fmt.Sprintf("[@Other](mention://agent/%s) what do you think?", otherAgentID),
	}

	tests := []struct {
		name    string
		parent  *db.Comment
		content string
		want    bool
	}{
		{
			name:    "top-level comment (nil parent) → allow",
			parent:  nil,
			content: "a comment",
			want:    false,
		},
		{
			name:    "reply to agent thread, no mentions → allow",
			parent:  agentParent,
			content: "sounds good",
			want:    false,
		},
		{
			name:    "reply to agent thread, mention other member → allow (handled by other check)",
			parent:  agentParent,
			content: fmt.Sprintf("[@Bob](mention://member/%s) thoughts?", memberID),
			want:    false, // isReplyToMemberThread only checks member threads
		},
		{
			name:    "reply to member thread, no mentions → suppress",
			parent:  memberParent,
			content: "I agree with you",
			want:    true,
		},
		{
			name:    "reply to member thread, mention other member → suppress",
			parent:  memberParent,
			content: fmt.Sprintf("[@Alice](mention://member/%s) what about this?", otherMemberID),
			want:    true,
		},
		{
			name:    "reply to member thread, mention assignee agent → allow",
			parent:  memberParent,
			content: fmt.Sprintf("[@Agent](mention://agent/%s) can you help?", agentAssigneeID),
			want:    false,
		},
		{
			name:    "reply to member thread, mention other agent (not assignee) → suppress",
			parent:  memberParent,
			content: fmt.Sprintf("[@Other](mention://agent/%s) take a look", otherAgentID),
			want:    true,
		},
		{
			name:    "reply to member thread that @mentioned assignee, no re-mention → allow",
			parent:  memberParentMentioningAssignee,
			content: "here is more context for you",
			want:    false,
		},
		{
			name:    "reply to member thread that @mentioned other agent, no re-mention → suppress",
			parent:  memberParentMentioningOther,
			content: "here is more context",
			want:    true, // parent mentioned other agent, not assignee — still suppress on_comment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.isReplyToMemberThread(context.Background(), tt.parent, tt.content, issue)
			if got != tt.want {
				t.Errorf("isReplyToMemberThread() = %v, want %v", got, tt.want)
			}
		})
	}
}

// -------------------------------------------------------------------
// Combined trigger decision (simulates the full on_comment check)
// -------------------------------------------------------------------

func TestOnCommentTriggerDecision(t *testing.T) {
	h := &Handler{}
	issue := issueWithAgentAssignee()

	memberParent := &db.Comment{AuthorType: "member", AuthorID: testUUID(memberID), Content: "plain thread starter"}
	agentParent := &db.Comment{AuthorType: "agent", AuthorID: testUUID(agentAssigneeID), Content: "agent thread starter"}
	memberParentMentioningAssignee := &db.Comment{
		AuthorType: "member",
		AuthorID:   testUUID(memberID),
		Content:    fmt.Sprintf("[@Agent](mention://agent/%s) help me", agentAssigneeID),
	}

	// Simulates the combined check from CreateComment:
	//   !commentMentionsOthersButNotAssignee && !isReplyToMemberThread
	shouldTrigger := func(parent *db.Comment, content string) bool {
		return !h.commentMentionsOthersButNotAssignee(content, issue) &&
			!h.isReplyToMemberThread(context.Background(), parent, content, issue)
	}

	tests := []struct {
		name    string
		parent  *db.Comment
		content string
		want    bool
	}{
		{"top-level, no mention", nil, "hello agent", true},
		{"top-level, mention assignee", nil, fmt.Sprintf("[@Agent](mention://agent/%s) fix this", agentAssigneeID), true},
		{"top-level, mention other only", nil, fmt.Sprintf("[@Other](mention://agent/%s) look", otherAgentID), false},
		{"reply agent thread, no mention", agentParent, "got it", true},
		{"reply agent thread, mention other member", agentParent, fmt.Sprintf("[@Bob](mention://member/%s) ?", memberID), false},
		{"reply agent thread, mention assignee", agentParent, fmt.Sprintf("[@Agent](mention://agent/%s) yes", agentAssigneeID), true},
		{"reply member thread, no mention", memberParent, "agreed", false},
		{"reply member thread, mention other member", memberParent, fmt.Sprintf("[@Bob](mention://member/%s) ok", memberID), false},
		{"reply member thread, mention assignee", memberParent, fmt.Sprintf("[@Agent](mention://agent/%s) help", agentAssigneeID), true},
		{"reply member thread that @mentioned assignee, no re-mention", memberParentMentioningAssignee, "here is more info", true},
		{"top-level, @all broadcast", nil, "[@All](mention://all/all) heads up team", false},
		{"reply agent thread, @all broadcast", agentParent, "[@All](mention://all/all) update for everyone", false},
		{"reply member thread, @all broadcast", memberParent, "[@All](mention://all/all) fyi", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldTrigger(tt.parent, tt.content)
			if got != tt.want {
				t.Errorf("shouldTrigger() = %v, want %v", got, tt.want)
			}
		})
	}
}

