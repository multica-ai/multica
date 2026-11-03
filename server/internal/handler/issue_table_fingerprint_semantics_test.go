package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rowsRequestWithCursor posts one table-rows query and returns the response.
func rowsRequestWithCursor(t *testing.T, advanced map[string]any, cursor string) issueTableRowsResponse {
	t.Helper()
	filters := map[string]any{}
	if advanced != nil {
		filters["advanced"] = advanced
	}
	body := map[string]any{
		"query": map[string]any{
			"scope":   map[string]any{"kind": "workspace"},
			"filters": filters,
			"sort":    map[string]any{"field": "position", "direction": "asc"},
		},
		"group": map[string]any{"kind": "none"},
		"page": func() any {
			if cursor == "" {
				return map[string]any{"limit": 1}
			}
			return map[string]any{"limit": 1, "cursor": cursor}
		}(),
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/table/rows", body)
	testHandler.ListIssueTableRows(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("table rows: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp issueTableRowsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode rows response: %v", err)
	}
	return resp
}

// TestIssueTableCursorSurvivesDroppedEmptyAdvancedValues pins the
// fingerprint/compile parity for whitespace-only advanced values (#9766
// follow-up): the compile path drops values that trim to nothing, so
// ["urgent"] and ["urgent", "  "] are the same query and a cursor captured
// under one spelling must survive the other. Before the fix the two
// spellings fingerprinted differently and the second page died with 409
// cursor_query_mismatch.
func TestIssueTableCursorSurvivesDroppedEmptyAdvancedValues(t *testing.T) {
	urgentA := dbfxIssueWithPriority(t, "urgent", "one")
	urgentB := dbfxIssueWithPriority(t, "urgent", "two")
	t.Cleanup(func() {
		deleteIssueForTableTest(t, urgentA)
		deleteIssueForTableTest(t, urgentB)
	})

	advanced := func(values ...string) map[string]any {
		children := make([]map[string]any, 0, len(values))
		for _, v := range values {
			children = append(children, map[string]any{"field": "priorities", "values": []string{v}})
		}
		return map[string]any{"match": "any", "children": children}
	}

	// Page 1 under spelling A captures a cursor.
	first := rowsRequestWithCursor(t, advanced("urgent"), "")
	if first.NextCursor == nil || *first.NextCursor == "" {
		t.Fatalf("expected a next cursor with limit=1 and two urgent issues")
	}

	// Page 2 under spelling B (same query, extra whitespace-only value).
	second := rowsRequestWithCursor(t, advanced("urgent", "  "), *first.NextCursor)
	if len(second.Rows) == 0 {
		t.Fatalf("page 2 returned no rows")
	}
	for _, row := range second.Rows {
		if row.Issue.ID == urgentA || row.Issue.ID == urgentB {
			continue
		}
		t.Fatalf("page 2 leaked non-fixture row %s", row.Issue.ID)
	}

	// And the reverse order: cursor captured under B survives A.
	firstB := rowsRequestWithCursor(t, advanced("urgent", "  "), "")
	if firstB.NextCursor == nil || *firstB.NextCursor == "" {
		t.Fatalf("expected a next cursor under spelling B")
	}
	secondA := rowsRequestWithCursor(t, advanced("urgent"), *firstB.NextCursor)
	if len(secondA.Rows) == 0 {
		t.Fatalf("page 2 (reverse order) returned no rows")
	}
	_ = fmt.Sprint
}

// dbfxIssueWithPriority creates one issue with the given priority and returns
// its id.
func dbfxIssueWithPriority(t *testing.T, priority, tag string) string {
	t.Helper()
	created := createIssueForTest(t, map[string]any{"title": "fingerprint parity " + priority + " " + tag, "priority": priority})
	return created.ID
}

func deleteIssueForTableTest(t *testing.T, id string) {
	t.Helper()
	r := withURLParam(newRequest("DELETE", "/api/issues/"+id, nil), "id", id)
	testHandler.DeleteIssue(httptest.NewRecorder(), r)
}
