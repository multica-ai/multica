package handler

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// An issue reaches clients rendered two different ways: the HTTP handler
// marshals IssueResponse, while events published outside it (autopilot and the
// channel engine's /issue command on issue:created, the background stuck-issue
// status reset on issue:updated) marshal service.IssueToMap.
// Clients type both as a complete Issue and insert the
// object straight into the list cache without runtime validation, so a key
// present in one rendering and absent from the other is a field that reads
// back undefined depending on which entry point created the issue — a project
// that never lands in its project view, a custom-property column that shows
// empty until the next refetch.
//
// Adding a field to IssueResponse without adding it to IssueToMap must fail
// here rather than in the UI.
func TestIssueToMap_KeysMatchIssueResponse(t *testing.T) {
	const prefix = "MUL"
	issue := fullyPopulatedIssue(t)

	responseKeys := jsonKeys(t, issueToResponse(issue, prefix))
	mapKeys := jsonKeys(t, service.IssueToMap(issue, prefix))

	for _, k := range responseKeys {
		if !hasKey(mapKeys, k) {
			t.Errorf("service.IssueToMap is missing %q, which handler.IssueResponse emits; "+
				"clients treat both as a complete Issue", k)
		}
	}
	for _, k := range mapKeys {
		if !hasKey(responseKeys, k) {
			t.Errorf("service.IssueToMap emits %q, which handler.IssueResponse does not; "+
				"the two renderings must describe the same issue", k)
		}
	}
}

// The two renderings must also agree on values and JSON types, not just on
// which keys exist — a number sent as a string, or a date formatted
// differently, breaks clients just as quietly as a missing field.
func TestIssueToMap_ValuesMatchIssueResponse(t *testing.T) {
	const prefix = "MUL"
	issue := fullyPopulatedIssue(t)

	response, err := json.Marshal(issueToResponse(issue, prefix))
	if err != nil {
		t.Fatalf("marshal IssueResponse: %v", err)
	}
	fromMap, err := json.Marshal(service.IssueToMap(issue, prefix))
	if err != nil {
		t.Fatalf("marshal IssueToMap: %v", err)
	}

	var want, got map[string]any
	if err := json.Unmarshal(response, &want); err != nil {
		t.Fatalf("unmarshal IssueResponse: %v", err)
	}
	if err := json.Unmarshal(fromMap, &got); err != nil {
		t.Fatalf("unmarshal IssueToMap: %v", err)
	}

	for k, wantValue := range want {
		gotValue, ok := got[k]
		if !ok {
			continue // reported by the key test
		}
		wantJSON, _ := json.Marshal(wantValue)
		gotJSON, _ := json.Marshal(gotValue)
		if string(wantJSON) != string(gotJSON) {
			t.Errorf("issue[%q]: IssueToMap has %s, IssueResponse has %s", k, gotJSON, wantJSON)
		}
	}
}

// metadata and properties are documented as always present so clients can
// index them without a nil guard. A row that never had either bag written
// must still broadcast {}, never null.
func TestIssueToMap_UnsetJSONBagsAreEmptyObjects(t *testing.T) {
	issue := db.Issue{Number: 42}

	raw, err := json.Marshal(service.IssueToMap(issue, "MUL"))
	if err != nil {
		t.Fatalf("marshal IssueToMap: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal IssueToMap: %v", err)
	}

	for _, key := range []string{"metadata", "properties"} {
		if got := string(payload[key]); got != "{}" {
			t.Errorf("issue[%q] = %s; want {} so clients can index it without a nil guard", key, got)
		}
	}
}

// A workspace lookup can fail on the chat path, which degrades the prefix to
// "". The identifier is then rendered from the number alone rather than as a
// stray "-42", and the chat reply and the broadcast payload must not disagree
// about it (both call service.IssueIdentifier).
func TestIssueIdentifier_DegradesWithoutPrefix(t *testing.T) {
	if got := service.IssueIdentifier("MUL", 42); got != "MUL-42" {
		t.Errorf("IssueIdentifier(\"MUL\", 42) = %q; want MUL-42", got)
	}
	if got := service.IssueIdentifier("", 42); got != "#42" {
		t.Errorf("IssueIdentifier(\"\", 42) = %q; want #42", got)
	}
}

// fullyPopulatedIssue returns a row with every nullable column set, so a
// rendering that drops a field cannot pass by coincidentally emitting the
// zero value both ways.
func fullyPopulatedIssue(t *testing.T) db.Issue {
	t.Helper()
	utcDate := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return db.Issue{
		ID:             parseUUID("11111111-1111-1111-1111-111111111111"),
		WorkspaceID:    parseUUID("22222222-2222-2222-2222-222222222222"),
		Number:         42,
		Title:          "Fix login",
		Description:    pgtype.Text{String: "details", Valid: true},
		Status:         "todo",
		Priority:       "high",
		AssigneeType:   pgtype.Text{String: "agent", Valid: true},
		AssigneeID:     parseUUID("33333333-3333-3333-3333-333333333333"),
		CreatorType:    "member",
		CreatorID:      parseUUID("44444444-4444-4444-4444-444444444444"),
		ParentIssueID:  parseUUID("55555555-5555-5555-5555-555555555555"),
		ProjectID:      parseUUID("66666666-6666-6666-6666-666666666666"),
		Position:       1024.5,
		Stage:          pgtype.Int4{Int32: 2, Valid: true},
		StartDate:      pgtype.Date{Time: utcDate(2026, time.January, 1), Valid: true},
		DueDate:        pgtype.Date{Time: utcDate(2026, time.February, 1), Valid: true},
		CreatedAt:      pgtype.Timestamptz{Time: utcDate(2026, time.January, 1), Valid: true},
		UpdatedAt:      pgtype.Timestamptz{Time: utcDate(2026, time.January, 2), Valid: true},
		LastActivityAt: pgtype.Timestamptz{Time: utcDate(2026, time.January, 3), Valid: true},
		Metadata:       []byte(`{"pr_url":"https://example.test/pr/1"}`),
		Properties:     []byte(`{"77777777-7777-7777-7777-777777777777":"done"}`),
	}
}

// jsonKeys marshals a value and returns its top-level JSON keys. Fields tagged
// omitempty (reactions, attachments, labels) are absent from both renderings
// here by construction: they are optional decorations the client already
// nil-guards, not part of the core issue shape this test pins.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal %T: %v", v, err)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func hasKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
