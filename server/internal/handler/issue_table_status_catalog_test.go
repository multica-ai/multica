package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MUL-4809 — the Table surface must speak the status catalog, not just the 7
// legacy tokens.
//
// The Table query subsystem originally validated filters.statuses against
// validIssueStatuses and filtered on i.status. The status picker stores catalog
// ids (for built-ins too), so sending a selection as `statuses` 400'd the whole
// Table request — a status filter broke the view outright, not just for custom
// statuses. filters.status_ids selects exact catalog rows instead.

// tableGroupsByStatusIDs runs the groups endpoint filtered by catalog ids.
func tableGroupsByStatusIDs(t *testing.T, statusIDs []string) (issueTableGroupsResponse, int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListIssueTableGroups(w, newRequest("POST", "/api/issues/table/groups", issueTableGroupsRequest{
		Query: issueTableQuerySpec{
			Scope:   issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{StatusIds: statusIDs},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: issueTableGroupSpec{Kind: "status"},
		Page:  issueTablePageRequest{Limit: 100},
	}))
	var resp issueTableGroupsResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code, w.Body.String()
}

// A custom status is selectable on the Table, and two custom statuses sharing a
// Category stay distinguishable — the whole point of filtering on catalog ids.
func TestTableFiltersByCustomStatusID(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	alpha, code, body := createStatus(t, map[string]any{
		"name": "Table Alpha", "category": "in_progress", "icon": "in_progress", "color": "warning",
	})
	if code != http.StatusCreated {
		t.Fatalf("create alpha: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, alpha.ID, "") })
	beta, code, body := createStatus(t, map[string]any{
		"name": "Table Beta", "category": "in_progress", "icon": "in_review", "color": "info",
	})
	if code != http.StatusCreated {
		t.Fatalf("create beta: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, beta.ID, "") })

	inAlpha, _, _ := createIssueWithStatusFields(t, map[string]any{"title": "table alpha row", "status_id": alpha.ID})
	inBeta, _, _ := createIssueWithStatusFields(t, map[string]any{"title": "table beta row", "status_id": beta.ID})
	t.Cleanup(func() { deleteTestIssue(t, inAlpha.ID); deleteTestIssue(t, inBeta.ID) })

	// Filtering by a catalog id must succeed (it used to 400) and select only it,
	// even though alpha and beta share the in_progress Category / legacy token.
	resp, code, body := tableGroupsByStatusIDs(t, []string{alpha.ID})
	if code != http.StatusOK {
		t.Fatalf("filter by custom status_id: expected 200, got %d %s", code, body)
	}
	if resp.Total != 1 {
		t.Fatalf("total = %d, want 1 (only the alpha issue)", resp.Total)
	}

	// Both ids OR together.
	resp, code, body = tableGroupsByStatusIDs(t, []string{alpha.ID, beta.ID})
	if code != http.StatusOK {
		t.Fatalf("filter by both ids: %d %s", code, body)
	}
	if resp.Total != 2 {
		t.Fatalf("total = %d, want 2", resp.Total)
	}
}

// A malformed id is rejected rather than silently dropped.
func TestTableRejectsMalformedStatusID(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	if _, code, body := tableGroupsByStatusIDs(t, []string{"not-a-uuid"}); code != http.StatusBadRequest {
		t.Fatalf("malformed status_ids: expected 400, got %d %s", code, body)
	}
}

// Table rows must echo the resolved catalog status, so a custom status renders
// with its own name/icon/color instead of the legacy token it projects to.
func TestTableRowsCarryStatusDetail(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Table Detail", "category": "todo", "icon": "todo", "color": "info",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	created, _, _ := createIssueWithStatusFields(t, map[string]any{"title": "table detail row", "status_id": custom.ID})
	t.Cleanup(func() { deleteTestIssue(t, created.ID) })

	w := httptest.NewRecorder()
	testHandler.ListIssueTableRows(w, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope:   issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{StatusIds: []string{custom.ID}},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: issueTableGroupSpec{Kind: "none"},
		Page:  issueTablePageRequest{Limit: 50},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("table rows: expected 200, got %d %s", w.Code, w.Body.String())
	}
	var resp issueTableRowsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(resp.Rows))
	}
	got := resp.Rows[0].Issue
	if got.StatusID == nil || *got.StatusID != custom.ID {
		t.Fatalf("row status_id = %v, want %s", got.StatusID, custom.ID)
	}
	if got.StatusDetail == nil || got.StatusDetail.Name != "Table Detail" {
		t.Fatalf("row status_detail = %v, want the custom status (Table rows rendered the legacy token before)", got.StatusDetail)
	}
}
