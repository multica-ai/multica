package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A triage decision dispatches nobody (MUL-7189 §2.5).
//
// §2.3 deliberately lets a member name an agent by hand even inside Triage —
// naming is a conversation, not an executor. A decision explaining itself
// ("@Ana already filed this") would ride straight through that allowance, so
// the decision is a comment TYPE the trigger path refuses outright.
//
// The comment is constructed in memory rather than inserted: the `comment.type`
// CHECK does not admit the value yet (MUL-7216 widens it together with the
// actions that write it), and the rule under test reads the type off the row it
// is handed, which is exactly what the writer will hand it.
func TestTriageDecisionCommentDispatchesNobody(t *testing.T) {
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, "", "triage decision dispatch")
	issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	body := "duplicate of what [Agent](mention://agent/" + f.agentID + ") is already on"
	comment := func(commentType string) db.Comment {
		id := dbfx.Comment(t, issueID, body, testutil.Cols{
			"author_type": "member",
			"author_id":   testUserID,
		})
		return db.Comment{
			ID:         parseUUID(id),
			IssueID:    issue.ID,
			AuthorType: "member",
			AuthorID:   parseUUID(testUserID),
			Content:    body,
			Type:       commentType,
		}
	}

	decision := comment(CommentTypeTriageDecision)
	if outcomes := testHandler.triggerTasksForComment(t.Context(), issue, decision, nil,
		"member", testUserID, testUserID, nil); len(outcomes) != 0 {
		t.Fatalf("a triage decision produced %d trigger outcomes, want 0: %+v", len(outcomes), outcomes)
	}
	if tasks := tasksOn(t, issueID); tasks != 0 {
		t.Fatalf("a triage decision enqueued %d tasks, want 0", tasks)
	}

	// The control: the same body, on the same ordinary issue, as an ordinary
	// comment. Without it a broken mention parser would pass the case above.
	ordinary := comment("comment")
	if outcomes := testHandler.triggerTasksForComment(t.Context(), issue, ordinary, nil,
		"member", testUserID, testUserID, nil); len(outcomes) == 0 {
		t.Fatal("the same body as an ordinary comment dispatched nobody — the suppression above proves nothing")
	}
}
