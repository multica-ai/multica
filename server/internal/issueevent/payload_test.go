package issueevent

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uid(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

// The realtime client (cmd/server/listeners.go) marshals Event.Payload directly, so
// the typed payload MUST serialize to the exact wire the single-update handler
// emitted as a map before this refactor. Installed desktop clients parse these keys.
// This pins the wire against the historical key/value set (key order is immaterial
// to a JSON object, so the comparison is on parsed content).
func TestIssueUpdatedPayloadWireMatchesLegacyMap(t *testing.T) {
	before := db.Issue{
		ID:           uid(1),
		WorkspaceID:  uid(9),
		Status:       "todo",
		Priority:     "high",
		Title:        "old title",
		AssigneeType: text("member"),
		AssigneeID:   uid(2),
		Description:  text("old body"),
		CreatorType:  "member",
		CreatorID:    uid(3),
	}
	after := before
	after.Status = "done"
	after.Title = "new title"

	// The producer's client representation of the issue is emitted unchanged; use a
	// small stand-in so the test pins the envelope, not issueToResponse.
	issue := map[string]any{"id": "issue-json"}

	p := Build(before, after, issue, true)

	// The exact key/value set the pre-refactor single-update map produced.
	prevAssigneeType := "member"
	prevAssigneeID := "02020202-0202-0202-0202-020202020202"
	prevDescription := "old body"
	legacy := map[string]any{
		"issue":               issue,
		"assignee_changed":    false,
		"status_changed":      true,
		"priority_changed":    false,
		"project_changed":     false,
		"start_date_changed":  false,
		"due_date_changed":    false,
		"description_changed": false,
		"title_changed":       true,
		"prev_title":          "old title",
		"prev_assignee_type":  &prevAssigneeType,
		"prev_assignee_id":    &prevAssigneeID,
		"prev_status":         "todo",
		"prev_priority":       "high",
		"prev_start_date":     (*string)(nil),
		"prev_due_date":       (*string)(nil),
		"prev_description":    &prevDescription,
		"creator_type":        "member",
		"creator_id":          "03030303-0303-0303-0303-030303030303",
	}

	if !jsonEqual(t, p, legacy) {
		gp, _ := json.MarshalIndent(p, "", "  ")
		gl, _ := json.MarshalIndent(legacy, "", "  ")
		t.Errorf("wire drift:\n typed=%s\n legacy=%s", gp, gl)
	}
}

// Source is off the wire unless a producer sets it, so a producer that does not
// (single / batch / task) keeps its exact prior payload.
func TestSourceOmittedByDefault(t *testing.T) {
	p := Build(db.Issue{}, db.Issue{}, nil, true)
	var m map[string]any
	raw, _ := json.Marshal(p)
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, present := m["source"]; present {
		t.Error("source must be omitted when unset")
	}
	p.Source = "github_pr_merged"
	raw, _ = json.Marshal(p)
	m = nil
	json.Unmarshal(raw, &m)
	if m["source"] != "github_pr_merged" {
		t.Errorf("source = %v, want github_pr_merged", m["source"])
	}
}

// The internal fields never reach the wire.
func TestInternalFieldsNotSerialized(t *testing.T) {
	p := Build(db.Issue{ID: uid(1), Status: "x"}, db.Issue{ID: uid(1), Status: "x"}, nil, true)
	raw, _ := json.Marshal(p)
	var m map[string]any
	json.Unmarshal(raw, &m)
	for _, k := range []string{"Snapshot", "snapshot", "TriggerSideEffects", "trigger_side_effects"} {
		if _, present := m[k]; present {
			t.Errorf("internal field %q leaked onto the wire", k)
		}
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	ra, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var ma, mb any
	if err := json.Unmarshal(ra, &ma); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rb, &mb); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(ma, mb)
}
