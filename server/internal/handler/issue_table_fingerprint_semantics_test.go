package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueTableCursorSurvivesDroppedEmptyAdvancedValues(t *testing.T) {
	prefix := "cursor-empty-" + uuid.NewString()
	firstID := dbfx.Issue(t, prefix+" first", testutil.Cols{"priority": "urgent", "position": 1})
	secondID := dbfx.Issue(t, prefix+" second", testutil.Cols{"priority": "urgent", "position": 2})
	condition := func(values ...any) issueTableFilterNode {
		return issueTableFilterNode{Field: "priorities", Values: rawValues(values...)}
	}
	base := issueTableFilterNode{Match: "any", Children: []issueTableFilterNode{condition("urgent")}}
	variants := map[string]issueTableFilterNode{
		"empty value": {Match: "any", Children: []issueTableFilterNode{condition("urgent", "  ")}},
		"empty condition": {Match: "any", Children: []issueTableFilterNode{
			condition("urgent"), condition("  "),
		}},
		"empty group": {Match: "any", Children: []issueTableFilterNode{
			condition("urgent"), {Match: "all", Children: []issueTableFilterNode{condition("  ")}},
		}},
		"padded value": {Match: "any", Children: []issueTableFilterNode{condition(" urgent ")}},
	}
	rows := func(t *testing.T, advanced *issueTableFilterNode, cursor *string) issueTableRowsResponse {
		t.Helper()
		var response issueTableRowsResponse
		testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope:   issueTableScope{Kind: "workspace"},
				Search:  prefix,
				Filters: issueTableFiltersRequest{Advanced: advanced},
				Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 1, Cursor: cursor},
		})).Want(http.StatusOK).JSON(&response)
		return response
	}
	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				from, to := &base, &variant
				if reverse {
					from, to = to, from
				}
				first := rows(t, from, nil)
				if len(first.Rows) != 1 || first.Rows[0].Issue.ID != firstID || first.NextCursor == nil {
					t.Fatalf("reverse=%v: unexpected first page: %+v", reverse, first)
				}
				second := rows(t, to, first.NextCursor)
				if len(second.Rows) != 1 || second.Rows[0].Issue.ID != secondID || second.NextCursor != nil {
					t.Fatalf("reverse=%v: unexpected second page: %+v", reverse, second)
				}
				if first.QueryFingerprint != second.QueryFingerprint {
					t.Fatalf("reverse=%v: equivalent filters have different fingerprints", reverse)
				}
			}
		})
	}
}

func TestIssueTableCursorRejectsChangedAdvancedPropertyValues(t *testing.T) {
	definition := uuid.NewString()
	issue := func(title, value string, position int) string {
		return dbfx.Issue(t, title, testutil.Cols{
			"properties": testutil.Raw(fmt.Sprintf(`'{"%s": %q}'::jsonb`, definition, value)),
			"position":   position,
		})
	}
	plainFirst := issue("plain first", "alpha", 10)
	plainSecond := issue("plain second", "alpha", 30)
	paddedFirst := issue("padded first", " alpha ", 5)
	paddedSecond := issue("padded second", " alpha ", 20)
	request := func(value string, cursor *string) issueTableRowsRequest {
		return issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope: issueTableScope{Kind: "workspace"},
				Filters: issueTableFiltersRequest{Advanced: &issueTableFilterNode{
					Match:    "all",
					Children: []issueTableFilterNode{{Field: "property:" + definition, Values: rawValues(value)}},
				}},
				Sort: issueTableSortRequest{Field: "position", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 1, Cursor: cursor},
		}
	}
	for _, tc := range []struct {
		value, otherValue string
		firstID, secondID string
	}{
		{"alpha", " alpha ", plainFirst, plainSecond},
		{" alpha ", "alpha", paddedFirst, paddedSecond},
	} {
		t.Run(tc.value, func(t *testing.T) {
			var first, second issueTableRowsResponse
			testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", request(tc.value, nil))).Want(http.StatusOK).JSON(&first)
			if len(first.Rows) != 1 || first.Rows[0].Issue.ID != tc.firstID || first.NextCursor == nil {
				t.Fatalf("unexpected first page: %+v", first)
			}
			testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", request(tc.value, first.NextCursor))).Want(http.StatusOK).JSON(&second)
			if len(second.Rows) != 1 || second.Rows[0].Issue.ID != tc.secondID || second.NextCursor != nil {
				t.Fatalf("unexpected second page: %+v", second)
			}
			var rejected struct {
				Error string `json:"error"`
			}
			testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", request(tc.otherValue, first.NextCursor))).Want(http.StatusConflict).JSON(&rejected)
			if rejected.Error != "cursor_query_mismatch" {
				t.Fatalf("unexpected cursor rejection: %s", rejected.Error)
			}
		})
	}
}

func TestIssueTableFingerprintPreservesAdvancedPropertyValues(t *testing.T) {
	field := "property:" + uuid.NewString()
	fingerprint := func(t *testing.T, values ...any) string {
		t.Helper()
		value, err := canonicalIssueTableFingerprint(testWorkspaceID, issueTableQuerySpec{
			Scope: issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{Advanced: &issueTableFilterNode{
				Match:    "all",
				Children: []issueTableFilterNode{{Field: field, Values: rawValues(values...)}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, tc := range []struct {
		name        string
		left, right []any
	}{
		{"padded string", []any{"alpha"}, []any{" alpha "}},
		{"whitespace-only string", []any{"  "}, nil},
		{"padded unset marker", []any{noPropertyValue}, []any{" " + noPropertyValue + " "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if fingerprint(t, tc.left...) == fingerprint(t, tc.right...) {
				t.Fatal("different property predicates share a fingerprint")
			}
		})
	}
	op := map[string]string{"op": "contains", "value": " alpha "}
	if fingerprint(t, "alpha", op, "alpha") != fingerprint(t, op, "alpha") {
		t.Fatal("property value order or duplicates changed the fingerprint")
	}
}
