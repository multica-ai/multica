package handler

import (
	"context"
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

// Two custom statuses sharing a Category must be SEPARATE columns when grouping
// by status — the whole point of keying groups on the catalog id (MUL-4809).
// Grouping used to key on i.status, which merged them into one in_progress column.
func TestTableGroupsByCatalogStatus(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	alpha, code, body := createStatus(t, map[string]any{
		"name": "Group Alpha", "category": "in_progress", "icon": "in_progress", "color": "warning",
	})
	if code != http.StatusCreated {
		t.Fatalf("create alpha: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, alpha.ID, "") })
	beta, code, body := createStatus(t, map[string]any{
		"name": "Group Beta", "category": "in_progress", "icon": "in_review", "color": "info",
	})
	if code != http.StatusCreated {
		t.Fatalf("create beta: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, beta.ID, "") })

	inAlpha, _, _ := createIssueWithStatusFields(t, map[string]any{"title": "group alpha row", "status_id": alpha.ID})
	inBeta, _, _ := createIssueWithStatusFields(t, map[string]any{"title": "group beta row", "status_id": beta.ID})
	t.Cleanup(func() { deleteTestIssue(t, inAlpha.ID); deleteTestIssue(t, inBeta.ID) })

	resp, code, body := tableGroupsByStatusIDs(t, []string{alpha.ID, beta.ID})
	if code != http.StatusOK {
		t.Fatalf("groups: %d %s", code, body)
	}
	byID := map[string]issueTableGroupDescriptorResponse{}
	for _, g := range resp.Groups {
		byID[g.Value.StatusID] = g
	}
	ga, okA := byID[alpha.ID]
	gb, okB := byID[beta.ID]
	if !okA || !okB {
		t.Fatalf("expected a separate column per custom status; got %+v", resp.Groups)
	}
	if ga.Count != 1 || gb.Count != 1 {
		t.Fatalf("counts alpha=%d beta=%d, want 1/1 (they must not merge)", ga.Count, gb.Count)
	}
	if ga.Key != "status:"+alpha.ID {
		t.Fatalf("group key = %q, want status:%s", ga.Key, alpha.ID)
	}
	// The column still names itself for older clients and for display.
	if ga.Value.Status != "in_progress" {
		t.Fatalf("legacy token = %q, want in_progress", ga.Value.Status)
	}
	if ga.Value.StatusName != "Group Alpha" {
		t.Fatalf("status_name = %q, want Group Alpha", ga.Value.StatusName)
	}
}

// P0 compat: a workspace that upgraded before any backfill has issues with
// status_id IS NULL. Filtering by a built-in catalog id must still return them
// (matched by legacy token), and they must group into that built-in's column —
// otherwise every pre-existing issue disappears the moment a filter is used.
func TestLegacyNullStatusIDStillFiltersAndGroups(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	catalog := getStatusCatalog(t, false)
	todoID := catalog.CategoryDefaults["todo"]
	if todoID == "" {
		t.Fatal("todo category default missing")
	}

	id := createTestIssue(t, "legacy null status_id row", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, id) })
	// Simulate a pre-catalog row: legacy token only, no status_id.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET status_id = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("null out status_id: %v", err)
	}

	resp, code, body := tableGroupsByStatusIDs(t, []string{todoID})
	if code != http.StatusOK {
		t.Fatalf("filter by built-in id: %d %s", code, body)
	}
	if resp.Total < 1 {
		t.Fatalf("legacy status_id=NULL issue vanished under a catalog filter (total=%d)", resp.Total)
	}
	var found bool
	for _, g := range resp.Groups {
		if g.Value.StatusID == todoID {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy row did not fold into the Todo column; groups=%+v", resp.Groups)
	}
}

// statusIDForSystemKey resolves the catalog id of the built-in that owns a legacy
// token, for tests that assert catalog-keyed group keys (MUL-4809).
func statusIDForSystemKey(t *testing.T, systemKey string) string {
	t.Helper()
	ensureTestWorkspaceStatuses(t)
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM issue_status WHERE workspace_id = $1 AND system_key = $2`,
		testWorkspaceID, systemKey).Scan(&id); err != nil {
		t.Fatalf("resolve status id for %q: %v", systemKey, err)
	}
	return id
}

// TestCategoryLaneReturnsRowsForCustomStatusSelection is the cross-layer
// counterexample for the empty-list P0 (MUL-4809 review).
//
// The List projects a selected CUSTOM status onto its Category lane, so it asks
// for `group_key=status:in_progress` while narrowing with
// `filters.status_ids=[<custom id>]`. The lane predicate used to resolve the
// token to the BUILT-IN In Progress catalog id; a custom status's effective
// group value is its OWN id, so the two conditions AND'd to nothing and the
// request — correctly formed, correctly routed — still returned zero rows.
//
// The lane is the legacy-token bucket, so it must contain the custom statuses
// that project to that token.
func TestCategoryLaneReturnsRowsForCustomStatusSelection(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Lane Needs QA", "category": "in_progress", "icon": "in_review", "color": "warning",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	created, code, body := createIssueWithStatusFields(t, map[string]any{
		"title": "lane custom row", "status_id": custom.ID, "priority": "none",
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", code, body)
	}
	t.Cleanup(func() { deleteTestIssue(t, created.ID) })

	// Exactly what the List sends for a custom-status selection.
	w := httptest.NewRecorder()
	testHandler.ListIssueTableRows(w, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope:   issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{StatusIds: []string{custom.ID}},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group:    issueTableGroupSpec{Kind: "status"},
		GroupKey: strPtr("status:in_progress"),
		Page:     issueTablePageRequest{Limit: 50},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("category lane rows: expected 200, got %d %s", w.Code, w.Body.String())
	}
	var resp issueTableRowsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	var found bool
	for _, row := range resp.Rows {
		if row.Issue.ID == created.ID {
			found = true
			if row.Issue.StatusDetail == nil || row.Issue.StatusDetail.Name != "Lane Needs QA" {
				t.Fatalf("row lost its catalog status: %v", row.Issue.StatusDetail)
			}
		}
	}
	if !found {
		t.Fatalf("custom-status issue missing from its Category lane: %d rows returned — this is the empty-list regression", len(resp.Rows))
	}

	// The lane must still be the legacy-token bucket, not a Category free-for-all:
	// in_review is its own lane and must not be swept into in_progress.
	inReview, code, body := createIssueWithStatusFields(t, map[string]any{
		"title": "lane in review row", "status": "in_review", "priority": "none",
	})
	if code != http.StatusCreated {
		t.Fatalf("create in_review issue: %d %s", code, body)
	}
	t.Cleanup(func() { deleteTestIssue(t, inReview.ID) })

	w2 := httptest.NewRecorder()
	testHandler.ListIssueTableRows(w2, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope:   issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{},
			Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group:    issueTableGroupSpec{Kind: "status"},
		GroupKey: strPtr("status:in_progress"),
		Page:     issueTablePageRequest{Limit: 100},
	}))
	var resp2 issueTableRowsResponse
	json.NewDecoder(w2.Body).Decode(&resp2)
	for _, row := range resp2.Rows {
		if row.Issue.ID == inReview.ID {
			t.Fatal("in_review issue leaked into the in_progress lane; the lane is the token bucket, not the Category")
		}
	}
}
