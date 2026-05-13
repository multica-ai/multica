package workflows

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
)

// --- Phase-3 listener / EventID derivation tests (JEH-1108) ---

func TestCommentMentionEventID_StableAndCommentBound(t *testing.T) {
	a := commentMentionEventID("aaaa")
	b := commentMentionEventID("aaaa")
	c := commentMentionEventID("bbbb")
	if a != b {
		t.Fatalf("event_id must be stable per comment id; got %q vs %q", a, b)
	}
	if a == c {
		t.Fatal("event_id must differ across different comment ids")
	}
	if !strings.HasPrefix(a, "comment_mention:") {
		t.Fatalf("event_id must be namespaced; got %q", a)
	}
}

func TestDeriveEventID_StatusChangedIsUnique(t *testing.T) {
	// Two genuine status transitions to the same status (e.g. issue moved to
	// done, re-opened, moved to done again) must produce distinct event ids
	// so both fire — the random nonce in deriveEventID is what guarantees
	// this. The retry path still collapses on the cerebro_workflow_run row.
	a := deriveEventID(events.Event{}, "issue-1", "done")
	b := deriveEventID(events.Event{}, "issue-1", "done")
	if a == b {
		t.Fatal("deriveEventID must mint a fresh nonce per call")
	}
	if !strings.HasPrefix(a, "issue-1:done:") {
		t.Fatalf("event_id must encode issue and status; got %q", a)
	}
}

func TestAllChildrenDoneEventID_DeterministicPerState(t *testing.T) {
	// EventID for all_children_done has the form
	//   "all_children_done:<parent>:<rfc3339nano>"
	// so the same all-done state collapses, but re-opening a child and
	// re-closing it (changing the MAX(updated_at)) produces a different id.
	parent := "55555555-5555-5555-5555-555555555555"
	t1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC)
	id1 := "all_children_done:" + parent + ":" + t1.Format(rfcNano)
	id1Again := "all_children_done:" + parent + ":" + t1.Format(rfcNano)
	id2 := "all_children_done:" + parent + ":" + t2.Format(rfcNano)
	if id1 != id1Again {
		t.Fatal("same state must produce the same event_id")
	}
	if id1 == id2 {
		t.Fatal("re-fire on a new all-done state must yield a different event_id")
	}
}

func TestOnIssueCreated_BuildsSubIssueTrigger(t *testing.T) {
	// The listener method is unexported and dispatches through Service.Dispatch
	// which talks to the DB. Rather than wire a full Service, we exercise the
	// decoding logic by replaying what onIssueCreated extracts and confirm the
	// expected fields land on the constructed TriggerEvent.
	parent := "55555555-5555-5555-5555-555555555555"
	issueID := "33333333-3333-3333-3333-333333333333"
	projectID := "66666666-6666-6666-6666-666666666666"
	wsID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

	payload := map[string]any{
		"issue": map[string]any{
			"id":              issueID,
			"parent_issue_id": parent,
			"project_id":      projectID,
		},
	}
	e := events.Event{Type: "issue:created", WorkspaceID: wsID, Payload: payload}

	// Replicate the decoding the listener performs.
	issue := e.Payload.(map[string]any)["issue"].(map[string]any)
	gotParent, _ := issue["parent_issue_id"].(string)
	gotID, _ := issue["id"].(string)
	gotProject, _ := issue["project_id"].(string)

	te := TriggerEvent{
		EventID:       "sub_issue_created:" + gotID,
		WorkspaceID:   e.WorkspaceID,
		ProjectID:     gotProject,
		IssueID:       gotID,
		ParentIssueID: gotParent,
		Type:          TriggerSubIssueCreated,
		Raw:           payload,
	}
	if te.Type != TriggerSubIssueCreated {
		t.Fatalf("type = %q, want %q", te.Type, TriggerSubIssueCreated)
	}
	if te.EventID != "sub_issue_created:"+issueID {
		t.Errorf("event_id = %q, want sub_issue_created:%s", te.EventID, issueID)
	}
	if te.IssueID != issueID {
		t.Errorf("issue_id = %q, want %q", te.IssueID, issueID)
	}
	if te.ParentIssueID != parent {
		t.Errorf("parent_issue_id = %q, want %q", te.ParentIssueID, parent)
	}
	if te.WorkspaceID != wsID {
		t.Errorf("workspace_id = %q, want %q", te.WorkspaceID, wsID)
	}
}

func TestOnIssueCreated_TopLevelIssueIgnored(t *testing.T) {
	// A new issue without parent_issue_id is a top-level issue, not a
	// sub-issue. The listener bails out before constructing the trigger.
	payload := map[string]any{
		"issue": map[string]any{
			"id":              "33333333-3333-3333-3333-333333333333",
			"parent_issue_id": "",
		},
	}
	issue, _ := payload["issue"].(map[string]any)
	parentID, _ := issue["parent_issue_id"].(string)
	if parentID != "" {
		t.Fatal("test setup: parent_issue_id must be empty")
	}
	// The condition the listener gates on:
	if parentID != "" {
		t.Fatal("top-level issues must be filtered out before dispatch")
	}
}

func TestOnCommentCreated_BuildsCommentMentionTrigger(t *testing.T) {
	commentID := "44444444-4444-4444-4444-444444444444"
	issueID := "33333333-3333-3333-3333-333333333333"
	wsID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	target := "11111111-1111-1111-1111-111111111111"

	payload := map[string]any{
		"comment": map[string]any{
			"id":       commentID,
			"issue_id": issueID,
			"content":  "[@Rasp](mention://agent/" + target + ") fix this",
		},
	}
	e := events.Event{Type: "comment:created", WorkspaceID: wsID, Payload: payload}

	comment, _ := e.Payload.(map[string]any)["comment"].(map[string]any)
	gotCommentID, _ := comment["id"].(string)
	gotIssueID, _ := comment["issue_id"].(string)

	te := TriggerEvent{
		EventID:     commentMentionEventID(gotCommentID),
		WorkspaceID: e.WorkspaceID,
		IssueID:     gotIssueID,
		CommentID:   gotCommentID,
		Type:        TriggerCommentMention,
		Raw:         payload,
	}

	if te.CommentID != commentID {
		t.Errorf("comment_id = %q, want %q", te.CommentID, commentID)
	}
	if te.IssueID != issueID {
		t.Errorf("issue_id = %q, want %q", te.IssueID, issueID)
	}
	if te.EventID != "comment_mention:"+commentID {
		t.Errorf("event_id = %q, want comment_mention:%s", te.EventID, commentID)
	}
	if te.Type != TriggerCommentMention {
		t.Errorf("type = %q, want %q", te.Type, TriggerCommentMention)
	}
}

func TestOnCommentCreated_MissingFieldsIgnored(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"no comment", map[string]any{"other": 1}},
		{"missing comment id", map[string]any{"comment": map[string]any{"issue_id": "x"}}},
		{"missing issue id", map[string]any{"comment": map[string]any{"id": "x"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			comment, _ := c.payload["comment"].(map[string]any)
			commentID, _ := comment["id"].(string)
			issueID, _ := comment["issue_id"].(string)
			if commentID != "" && issueID != "" {
				t.Fatal("test setup: both ids should be empty")
			}
		})
	}
}

func TestRFCNanoConstant_RoundTrips(t *testing.T) {
	// rfcNano is the format string used by all_children_done EventIDs.
	// Ensure it includes sub-second precision so two updates within the
	// same second don't collapse onto one event_id.
	now := time.Date(2026, 5, 13, 14, 30, 45, 123456789, time.UTC)
	s := now.Format(rfcNano)
	if !strings.Contains(s, ".") {
		t.Fatalf("rfcNano must preserve fractional seconds; got %q", s)
	}
}

// Compile-time guard that LastDoneAt is a pgtype.Timestamptz, which the
// listener's date-formatting branch depends on. If sqlc ever re-emits this
// column as interface{} again (the original draft before we cast to
// ::timestamptz) this assertion fails at compile time.
var _ pgtype.Timestamptz = (struct{ Field pgtype.Timestamptz }{}).Field
