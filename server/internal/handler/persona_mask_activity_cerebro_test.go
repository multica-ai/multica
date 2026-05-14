// CEREBRO-PATCH(persona-mask-activity-cerebro-test): JEH-1190 — net-new
// cerebro test file.
//
// JEH-1190: unit tests for the timeline mask helpers.
//
// Stubs PersonaMaskInvoker / PersonaMaskAuditWriter the same way as the
// JEH-1173 resource tests so the test stays in-package and never spins
// up persona / postgres.

package handler

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --------------------------------------------------------------- helpers

func uuidFor(b byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{b}, Valid: true}
}

// --------------------------------------------------------- activity rows

func TestMaskTimelineActivitiesForCaller_TitleChangedOnPiiIssue_RedactsFromTo(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"title"}, DecisionID: "d-1"},
		}},
		PersonaMaskAudit: audit,
	}
	issue := db.Issue{ID: uuidFor(1), WorkspaceID: uuidFor(9), Classification: "pii"}
	activities := []db.ActivityLog{
		{ID: uuidFor(10), Action: "title_changed", Details: json.RawMessage(`{"from":"old secret","to":"new secret"}`)},
	}
	entries := []TimelineEntry{
		{Type: "activity", ID: uuidToString(activities[0].ID), Details: activities[0].Details},
	}
	out := h.maskTimelineActivitiesForCaller(newReqWithUser("u1"), issue, activities, entries)

	if len(out) != 1 {
		t.Fatalf("activity rows must be preserved, got %d", len(out))
	}
	var parsed map[string]any
	if err := json.Unmarshal(out[0].Details, &parsed); err != nil {
		t.Fatalf("details should be valid JSON: %v", err)
	}
	if parsed["from"] != nil {
		t.Errorf("details.from should be null after redact, got %v", parsed["from"])
	}
	if parsed["to"] != nil {
		t.Errorf("details.to should be null after redact, got %v", parsed["to"])
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "mask" {
		t.Errorf("expected one mask audit row, got %+v", audit.events)
	}
	if audit.events[0].ResourceKind != personaKindIssue {
		t.Errorf("audit should reference the issue kind, got %q", audit.events[0].ResourceKind)
	}
}

func TestMaskTimelineActivitiesForCaller_TitleChanged_WithPiiGrant_PassesThrough(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		// Allowed + no masked fields = caller has pii grant; row is untouched.
		PersonaMask:      &stubMaskInvoker{decisions: []PersonaMaskDecision{{Allowed: true}}},
		PersonaMaskAudit: audit,
	}
	issue := db.Issue{ID: uuidFor(1), WorkspaceID: uuidFor(9), Classification: "pii"}
	activities := []db.ActivityLog{
		{ID: uuidFor(10), Action: "title_changed", Details: json.RawMessage(`{"from":"old","to":"new"}`)},
	}
	entries := []TimelineEntry{
		{Type: "activity", ID: uuidToString(activities[0].ID), Details: activities[0].Details},
	}
	out := h.maskTimelineActivitiesForCaller(newReqWithUser("u1"), issue, activities, entries)

	var parsed map[string]any
	_ = json.Unmarshal(out[0].Details, &parsed)
	if parsed["from"] != "old" || parsed["to"] != "new" {
		t.Errorf("pii-grant actor must see historical values, got %+v", parsed)
	}
	if len(audit.events) != 0 {
		t.Errorf("allow-no-mask must NOT write audit, got %+v", audit.events)
	}
}

func TestMaskTimelineActivitiesForCaller_StatusChanged_NeverRedacts(t *testing.T) {
	audit := &stubAuditWriter{}
	// Deliberately surface a mask decision; the helper must not consult
	// persona at all for non-PII actions, so the audit row count stays 0.
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"title"}},
		}},
		PersonaMaskAudit: audit,
	}
	issue := db.Issue{ID: uuidFor(1), WorkspaceID: uuidFor(9), Classification: "pii"}
	activities := []db.ActivityLog{
		{ID: uuidFor(11), Action: "status_changed", Details: json.RawMessage(`{"from":"todo","to":"in_progress"}`)},
		{ID: uuidFor(12), Action: "priority_changed", Details: json.RawMessage(`{"from":"low","to":"high"}`)},
		{ID: uuidFor(13), Action: "assignee_changed", Details: json.RawMessage(`{"from_id":"u1","to_id":"u2"}`)},
	}
	entries := []TimelineEntry{
		{Type: "activity", ID: uuidToString(activities[0].ID), Details: activities[0].Details},
		{Type: "activity", ID: uuidToString(activities[1].ID), Details: activities[1].Details},
		{Type: "activity", ID: uuidToString(activities[2].ID), Details: activities[2].Details},
	}
	out := h.maskTimelineActivitiesForCaller(newReqWithUser("u1"), issue, activities, entries)

	for i, want := range []string{
		`{"from":"todo","to":"in_progress"}`,
		`{"from":"low","to":"high"}`,
		`{"from_id":"u1","to_id":"u2"}`,
	} {
		if string(out[i].Details) != want {
			t.Errorf("row %d should be unchanged, want %s got %s", i, want, out[i].Details)
		}
	}
	if len(audit.events) != 0 {
		t.Errorf("non-PII actions must not call persona / audit, got %+v", audit.events)
	}
}

func TestMaskTimelineActivitiesForCaller_InternalIssue_FastPath(t *testing.T) {
	audit := &stubAuditWriter{}
	calls := &stubMaskInvoker{}
	h := &Handler{PersonaMask: calls, PersonaMaskAudit: audit}
	issue := db.Issue{ID: uuidFor(1), WorkspaceID: uuidFor(9), Classification: "internal"}
	activities := []db.ActivityLog{
		{ID: uuidFor(10), Action: "title_changed", Details: json.RawMessage(`{"from":"a","to":"b"}`)},
	}
	entries := []TimelineEntry{
		{Type: "activity", ID: uuidToString(activities[0].ID), Details: activities[0].Details},
	}
	out := h.maskTimelineActivitiesForCaller(newReqWithUser("u1"), issue, activities, entries)
	if string(out[0].Details) != `{"from":"a","to":"b"}` {
		t.Errorf("internal-classified issue should not redact, got %s", out[0].Details)
	}
	if calls.calls != 0 {
		t.Errorf("internal classification should skip persona call, got %d calls", calls.calls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit should be empty, got %+v", audit.events)
	}
}

func TestMaskTimelineActivitiesForCaller_DenyRedactsButPreservesRow(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask:      &stubMaskInvoker{decisions: []PersonaMaskDecision{{Allowed: false}}},
		PersonaMaskAudit: audit,
	}
	issue := db.Issue{ID: uuidFor(1), WorkspaceID: uuidFor(9), Classification: "pii"}
	activities := []db.ActivityLog{
		{ID: uuidFor(10), Action: "title_changed", Details: json.RawMessage(`{"from":"old","to":"new"}`)},
	}
	entries := []TimelineEntry{
		{Type: "activity", ID: uuidToString(activities[0].ID), Details: activities[0].Details},
	}
	out := h.maskTimelineActivitiesForCaller(newReqWithUser("u1"), issue, activities, entries)
	if len(out) != 1 {
		t.Fatalf("deny on activity must not drop the row, got %d", len(out))
	}
	var parsed map[string]any
	_ = json.Unmarshal(out[0].Details, &parsed)
	if parsed["from"] != nil || parsed["to"] != nil {
		t.Errorf("deny should null historical fields, got %+v", parsed)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "deny" {
		t.Errorf("deny should write one audit row, got %+v", audit.events)
	}
}

func TestActivityKindAndField_OnlyTitleAndDescriptionAreRedactable(t *testing.T) {
	tests := []struct {
		action   string
		wantRef  activityFieldRef
	}{
		{"title_changed", activityFieldRef{Kind: personaKindIssue, Field: personaFieldTitle}},
		{"description_updated", activityFieldRef{Kind: personaKindIssue, Field: personaFieldDescription}},
		{"status_changed", activityFieldRef{}},
		{"priority_changed", activityFieldRef{}},
		{"assignee_changed", activityFieldRef{}},
		{"due_date_changed", activityFieldRef{}},
		{"created", activityFieldRef{}},
		{"task_completed", activityFieldRef{}},
		{"task_failed", activityFieldRef{}},
		{"unknown_action", activityFieldRef{}},
	}
	for _, tc := range tests {
		got := activityKindAndField(tc.action)
		if got != tc.wantRef {
			t.Errorf("activityKindAndField(%q) = %+v, want %+v", tc.action, got, tc.wantRef)
		}
	}
}

func TestRedactActivityDetails_NullsKnownKeysOnly(t *testing.T) {
	in := json.RawMessage(`{"from":"x","to":"y","field":"email","keep":"me"}`)
	out := redactActivityDetails(in)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["from"] != nil || parsed["to"] != nil {
		t.Errorf("from/to should be null, got %+v", parsed)
	}
	if parsed["field"] != "email" {
		t.Errorf("field should be preserved, got %v", parsed["field"])
	}
	if parsed["keep"] != "me" {
		t.Errorf("unrelated keys should be preserved, got %v", parsed["keep"])
	}
}

func TestRedactActivityDetails_OldValueNewValueAliasesSupported(t *testing.T) {
	in := json.RawMessage(`{"field":"email","old_value":"a@x.com","new_value":"b@x.com"}`)
	out := redactActivityDetails(in)
	var parsed map[string]any
	_ = json.Unmarshal(out, &parsed)
	if parsed["old_value"] != nil || parsed["new_value"] != nil {
		t.Errorf("old_value/new_value should be null, got %+v", parsed)
	}
}

func TestRedactActivityDetails_EmptyDetailsPassThrough(t *testing.T) {
	if got := redactActivityDetails(nil); got != nil {
		t.Errorf("nil details should be returned as-is, got %v", got)
	}
	in := json.RawMessage(`{}`)
	if got := redactActivityDetails(in); string(got) != "{}" {
		t.Errorf("empty object should pass through, got %s", got)
	}
}

func TestRedactActivityDetails_InvalidJSONPassThrough(t *testing.T) {
	in := json.RawMessage(`not json`)
	if got := redactActivityDetails(in); string(got) != "not json" {
		t.Errorf("invalid JSON should pass through unchanged, got %s", got)
	}
}

// --------------------------------------------------------- comment rows

func TestMaskTimelineCommentsForCaller_DropsDeniedBlanksMasked(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: false},                                   // c1 — denied → dropped
			{Allowed: true, MaskedFields: []string{"content"}}, // c2 — blank content
			{Allowed: true},                                    // c3 — passthrough
		}},
		PersonaMaskAudit: audit,
	}
	ws := uuidFor(9)
	comments := []db.Comment{
		{ID: uuidFor(1), WorkspaceID: ws, Classification: "pii"},
		{ID: uuidFor(2), WorkspaceID: ws, Classification: "pii"},
		{ID: uuidFor(3), WorkspaceID: ws, Classification: "internal"},
	}
	c1, c2, c3 := "C1", "C2", "C3"
	entries := []TimelineEntry{
		{Type: "comment", ID: uuidToString(comments[0].ID), Content: &c1},
		{Type: "comment", ID: uuidToString(comments[1].ID), Content: &c2},
		{Type: "comment", ID: uuidToString(comments[2].ID), Content: &c3},
		// An activity entry mixed into the slice — must be passed through
		// untouched even though it's part of the timeline pipeline.
		{Type: "activity", ID: uuidToString(uuidFor(99)), Action: textPtr("status_changed")},
	}
	issue := db.Issue{WorkspaceID: ws}
	out := h.maskTimelineCommentsForCaller(newReqWithUser("u1"), issue, comments, entries)

	if len(out) != 3 {
		t.Fatalf("deny should drop one comment, got %d remaining", len(out))
	}
	if out[0].Type != "comment" || out[0].ID != uuidToString(comments[1].ID) || *out[0].Content != "" {
		t.Errorf("masked comment should have empty content, got %+v", out[0])
	}
	if out[1].Type != "comment" || out[1].ID != uuidToString(comments[2].ID) || *out[1].Content != "C3" {
		t.Errorf("passthrough comment should keep content, got %+v", out[1])
	}
	if out[2].Type != "activity" {
		t.Errorf("activity entries must survive the comment pass, got %+v", out[2])
	}
	if len(audit.events) != 2 {
		t.Errorf("expected one deny + one mask audit row, got %+v", audit.events)
	}
}

func TestMaskTimelineForCaller_NoInvokerNoOp(t *testing.T) {
	h := &Handler{} // PersonaMask == nil
	original := "C1"
	in := []TimelineEntry{
		{Type: "comment", ID: uuidToString(uuidFor(1)), Content: &original},
		{Type: "activity", ID: uuidToString(uuidFor(2)), Details: json.RawMessage(`{"from":"x","to":"y"}`)},
	}
	out := h.maskTimelineForCaller(
		newReqWithUser("u1"),
		db.Issue{Classification: "pii"},
		[]db.Comment{{ID: uuidFor(1), Classification: "pii"}},
		[]db.ActivityLog{{ID: uuidFor(2), Action: "title_changed", Details: in[1].Details}},
		in,
	)
	if len(out) != 2 {
		t.Fatalf("no invoker must pass everything through, got %d", len(out))
	}
	if *out[0].Content != "C1" || string(out[1].Details) != `{"from":"x","to":"y"}` {
		t.Errorf("entries should be untouched, got %+v", out)
	}
}

func TestApplyTimelineCommentMask_BlanksContentPointer(t *testing.T) {
	c := "secret"
	e := TimelineEntry{Type: "comment", Content: &c}
	applyTimelineCommentMask(&e, []string{"content"})
	if e.Content == nil || *e.Content != "" {
		t.Errorf("content should be blanked, got %v", e.Content)
	}
}

func TestApplyTimelineCommentMask_UnknownFieldNoop(t *testing.T) {
	c := "secret"
	e := TimelineEntry{Type: "comment", Content: &c}
	applyTimelineCommentMask(&e, []string{"definitely_not_a_field"})
	if e.Content == nil || *e.Content != "secret" {
		t.Errorf("unknown field should be no-op, got %v", e.Content)
	}
}
