package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
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

func TestHasAgentOrSquadMention(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"plain", "just a plain comment", false},
		{"agent", fmt.Sprintf("[@Agent](mention://agent/%s) please fix", agentAssigneeID), true},
		{"squad", "[@Squad](mention://squad/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) please coordinate", true},
		{"member only", fmt.Sprintf("[@Bob](mention://member/%s) take a look", memberID), false},
		{"issue only", "[PAN-1](mention://issue/44c266e7-f6dd-4be3-9140-5ac40233f79c) is related", false},
		{"all only", "[@all](mention://all/all) heads up", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAgentOrSquadMention(parseMentionsForTest(tt.content))
			if got != tt.want {
				t.Errorf("hasAgentOrSquadMention() = %v, want %v", got, tt.want)
			}
		})
	}
}

func parseMentionsForTest(content string) []util.Mention {
	return util.ParseMentions(content)
}

// -------------------------------------------------------------------
// isNoteComment — the /note opt-out prefix
// -------------------------------------------------------------------

func TestIsNoteComment(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"plain comment triggers", "just a plain comment", false},
		{"note prefix skips", "/note check the API expiry", true},
		{"bare note skips", "/note", true},
		{"uppercase note skips (case-insensitive)", "/NOTE shout", true},
		{"mixed case note skips", "/Note mixed", true},
		{"leading whitespace tolerated", "   /note leading space", true},
		{"note followed by newline skips", "/note\nmultiline body", true},
		{"plural notes does not match (word boundary)", "/notes are plural", false},
		{"noteworthy does not match", "/noteworthy idea", false},
		{"slash space note does not match", "/ note has a space", false},
		{"mid-sentence note does not match", "see foo/note here", false},
		{"note as second token does not match", "fyi /note", false},
		{"empty content does not match", "", false},
		{"whitespace-only content does not match", "   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNoteComment(tt.content); got != tt.want {
				t.Errorf("isNoteComment(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

// TestTriggerTasksForComment_NoteShortCircuits proves a /note comment returns
// before the cascade tries to resolve mentions, parent ownership, or assignee
// fallback. A nil-Queries Handler would panic if the /note guard were missing or
// moved below those branches.
func TestTriggerTasksForComment_NoteShortCircuits(t *testing.T) {
	h := &Handler{} // nil Queries / TaskService on purpose
	issue := issueWithAgentAssignee()
	comment := db.Comment{
		Content: fmt.Sprintf("/note cc [@Other](mention://agent/%s) just an fyi", otherAgentID),
	}

	// Must not panic — the guard short-circuits before any DB access.
	h.triggerTasksForComment(context.Background(), issue, comment, nil, "member", memberID, memberID, nil)
}
