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

// CEREBRO-PATCH(reply-triggers-assignee): FIR-2553 — our fork never suppresses
// the assignee's on_comment trigger on a member-thread reply (upstream did).
// isReplyToMemberThread therefore always returns false; a plain reply routes to
// the issue assignee, and involving anyone else requires an explicit @mention.
func TestIsReplyToMemberThread(t *testing.T) {
	h := &Handler{}
	issue := issueWithAgentAssignee()

	memberParent := &db.Comment{AuthorType: "member", AuthorID: testUUID(memberID), Content: "plain thread starter"}
	agentParent := &db.Comment{AuthorType: "agent", AuthorID: testUUID(agentAssigneeID), Content: "agent thread starter"}
	// Member-started thread root that @mentions a non-assignee agent — the exact
	// shape from the FIR-2553 bug report (issue assigned to A, root tagged B).
	memberParentMentioningOther := &db.Comment{
		AuthorType: "member",
		AuthorID:   testUUID(memberID),
		Content:    fmt.Sprintf("[@Other](mention://agent/%s) what do you think?", otherAgentID),
	}

	tests := []struct {
		name    string
		parent  *db.Comment
		content string
	}{
		{name: "top-level comment (nil parent)", parent: nil, content: "a comment"},
		{name: "reply to agent thread", parent: agentParent, content: "sounds good"},
		{name: "plain reply to member thread", parent: memberParent, content: "I agree with you"},
		{
			name:    "plain reply to member thread that tagged a non-assignee agent (FIR-2553)",
			parent:  memberParentMentioningOther,
			content: "here is more context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.isReplyToMemberThread(context.Background(), tt.parent, tt.content, issue); got {
				t.Errorf("isReplyToMemberThread() = true, want false (fork never suppresses)")
			}
		})
	}
}

// -------------------------------------------------------------------
// shouldInheritParentMentions
// -------------------------------------------------------------------

// CEREBRO-PATCH(reply-triggers-assignee): FIR-2553 — thread-root mention
// inheritance is disabled in our fork, so shouldInheritParentMentions always
// returns false. A plain reply routes to the issue assignee, not to whichever
// agent was tagged at the thread root.
func TestShouldInheritParentMentions(t *testing.T) {
	memberParent := &db.Comment{AuthorType: "member", AuthorID: testUUID(memberID), Content: "thread starter"}
	agentParent := &db.Comment{AuthorType: "agent", AuthorID: testUUID(agentAssigneeID), Content: "agent thread starter"}
	someMention := []util.Mention{{Type: "agent", ID: otherAgentID}}

	tests := []struct {
		name            string
		parent          *db.Comment
		replyMentions   []util.Mention
		replyAuthorType string
	}{
		{"nil parent", nil, nil, "member"},
		{"reply has explicit mentions", memberParent, someMention, "member"},
		{"agent-authored reply, member parent", memberParent, nil, "agent"},
		{"member reply, agent parent", agentParent, nil, "member"},
		{"member reply, member parent, no mentions (was the inheriting case)", memberParent, nil, "member"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldInheritParentMentions(tt.parent, tt.replyMentions, tt.replyAuthorType); got {
				t.Errorf("shouldInheritParentMentions() = true, want false (inheritance disabled)")
			}
		})
	}
}

// Regression for the case from MUL-1535: J posts a PR completion comment
// that @mentions GPT-Boy for review; later a member posts a plain follow-up
// reply asking the assignee a question. GPT-Boy must NOT be re-triggered.
func TestShouldInheritParentMentions_AgentReviewDelegationDoesNotLeak(t *testing.T) {
	jPRCompletion := &db.Comment{
		AuthorType: "agent",
		AuthorID:   testUUID(agentAssigneeID),
		Content:    fmt.Sprintf("PR ready. [@GPT-Boy](mention://agent/%s) please review this.", otherAgentID),
	}
	if got := shouldInheritParentMentions(jPRCompletion, nil, "member"); got {
		t.Fatal("member follow-up to an agent's PR-review delegation must not inherit the @reviewer mention")
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
		// FIR-2553: a plain reply in a member thread now wakes the issue assignee.
		{"reply member thread, no mention", memberParent, "agreed", true},
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
// before any of the three enqueue paths run. shouldEnqueueOnComment,
// shouldEnqueueSquadLeaderOnComment, and enqueueMentionedAgentTasks all
// dereference h.Queries, so a nil-Queries Handler would panic if the /note
// guard were missing or moved below them. The comment also @mentions an agent
// to exercise the mention path specifically.
func TestTriggerTasksForComment_NoteShortCircuits(t *testing.T) {
	h := &Handler{} // nil Queries / TaskService on purpose
	issue := issueWithAgentAssignee()
	comment := db.Comment{
		Content: fmt.Sprintf("/note cc [@Other](mention://agent/%s) just an fyi", otherAgentID),
	}

	// Must not panic — the guard short-circuits before any DB access.
	h.triggerTasksForComment(context.Background(), issue, comment, nil, "member", memberID)
}
