package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// Flat table facets must be evaluated before LIMIT/OFFSET and COUNT. This
// exercises the same multi-value/nullable filters the table query sends.
func TestListIssues_TableFacetsAreServerSide(t *testing.T) {
	ctx := context.Background()
	token := fmt.Sprintf("table-filter-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"table_filter_test":%q}`, token)

	createProject := func(title string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
		`, testWorkspaceID, title).Scan(&id); err != nil {
			t.Fatalf("create project: %v", err)
		}
		return id
	}
	projectA := createProject(token + " A")
	projectB := createProject(token + " B")

	var labelA, labelB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue_label (workspace_id, name, color)
		VALUES ($1, $2, '#ef4444') RETURNING id
	`, testWorkspaceID, token+" A").Scan(&labelA); err != nil {
		t.Fatalf("create label A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue_label (workspace_id, name, color)
		VALUES ($1, $2, '#22c55e') RETURNING id
	`, testWorkspaceID, token+" B").Scan(&labelB); err != nil {
		t.Fatalf("create label B: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id IN ($1, $2)`, labelA, labelB)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id IN ($1, $2)`, projectA, projectB)
	})

	nextNumber := func() int {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		return number
	}

	insertIssue := func(title, status, priority string, assigned bool, projectID, parentID *string) string {
		var assigneeType *string
		var assigneeID *string
		if assigned {
			member := "member"
			assigneeType = &member
			assigneeID = &testUserID
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, assignee_type, assignee_id,
				creator_type, creator_id, parent_issue_id, position, number,
				project_id, metadata
			) VALUES ($1, $2, $3, $4, $5, $6, 'member', $7, $8, 0, $9, $10, $11::jsonb)
			RETURNING id
		`, testWorkspaceID, title, status, priority, assigneeType, assigneeID,
			testUserID, parentID, nextNumber(), projectID, metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	issueA := insertIssue(token+" todo", "todo", "high", false, nil, nil)
	issueB := insertIssue(token+" progress", "in_progress", "low", true, &projectA, nil)
	issueC := insertIssue(token+" child", "done", "high", true, &projectB, &issueB)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1, $2), ($3, $4), ($5, $2)
	`, issueA, labelA, issueB, labelB, issueC); err != nil {
		t.Fatalf("attach labels: %v", err)
	}

	list := func(query string) ([]string, int64) {
		t.Helper()
		path := fmt.Sprintf(
			"/api/issues?workspace_id=%s&limit=100&metadata=%s%s",
			testWorkspaceID,
			url.QueryEscape(metadata),
			query,
		)
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var response struct {
			Issues []IssueResponse `json:"issues"`
			Total  int64           `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		ids := make([]string, 0, len(response.Issues))
		for _, issue := range response.Issues {
			ids = append(ids, issue.ID)
		}
		sort.Strings(ids)
		return ids, response.Total
	}

	assertList := func(query string, want ...string) {
		t.Helper()
		got, total := list(query)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("query %q ids = %v, want %v", query, got, want)
		}
		if total != int64(len(want)) {
			t.Fatalf("query %q total = %d, want %d", query, total, len(want))
		}
	}

	assertList("&statuses=todo,in_progress", issueA, issueB)
	assertList("&priorities=high", issueA, issueC)
	assertList("&assignee_filters="+url.QueryEscape("member:"+testUserID), issueB, issueC)
	assertList("&project_ids="+projectA+"&include_no_project=true", issueA, issueB)
	assertList("&label_ids="+labelA, issueA, issueC)
	assertList("&top_level_only=true", issueA, issueB)
	assertList("&q="+url.QueryEscape("progress "+token), issueB)
	assertList("&q="+url.QueryEscape("progress")+"&statuses=in_progress", issueB)
	var issueBNumber int
	if err := testPool.QueryRow(ctx, `SELECT number FROM issue WHERE id = $1`, issueB).Scan(&issueBNumber); err != nil {
		t.Fatalf("read issue number: %v", err)
	}
	assertList("&q="+url.QueryEscape(fmt.Sprintf("MUL-%d", issueBNumber)), issueB)
	assertList(
		"&statuses=todo&priorities=high&include_no_assignee=true&label_ids="+labelA+"&top_level_only=true",
		issueA,
	)

	// The ids facet restricts the window to an explicit id set (the table's
	// agents-working filter). It must compose with other facets, and a
	// PRESENT-but-EMPTY list must yield an empty window — the "nothing is
	// running" state — not fall back to the unrestricted one.
	assertList("&ids="+issueA+","+issueC, issueA, issueC)
	assertList("&ids="+issueA+","+issueC+"&statuses=done", issueC)
	assertList("&ids=")
}

// POST /api/issues/query is the body-transport twin of GET /api/issues for
// filter sets too large for a request line (the agents-working ids facet can
// carry hundreds of UUIDs; proxies cap request lines around 8 KB). It must
// return exactly what the GET returns for the same parameters, including at
// id-list sizes that would overflow a GET.
func TestQueryIssues_PostTwinMatchesGet(t *testing.T) {
	ctx := context.Background()
	token := fmt.Sprintf("post-query-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"post_query_test":%q}`, token)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
	})

	nextNumber := func() int {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		return number
	}
	insertIssue := func(title string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				position, number, metadata
			) VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5::jsonb)
			RETURNING id
		`, testWorkspaceID, title, testUserID, nextNumber(), metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}
	issueA := insertIssue(token + " A")
	issueB := insertIssue(token + " B")
	insertIssue(token + " C")

	decode := func(w *httptest.ResponseRecorder, transport string) ([]string, int64) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", transport, w.Code, w.Body.String())
		}
		var response struct {
			Issues []IssueResponse `json:"issues"`
			Total  int64           `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("%s decode: %v", transport, err)
		}
		ids := make([]string, 0, len(response.Issues))
		for _, issue := range response.Issues {
			ids = append(ids, issue.ID)
		}
		sort.Strings(ids)
		return ids, response.Total
	}

	// Pad the target ids with 300 extra UUIDs — a body far beyond any GET
	// request-line budget. The extras match nothing; the result must still be
	// exactly A and B.
	padded := []string{issueA, issueB}
	for i := 0; i < 300; i++ {
		var extra string
		if err := testPool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&extra); err != nil {
			t.Fatalf("generate uuid: %v", err)
		}
		padded = append(padded, extra)
	}

	postBody := map[string]string{
		"workspace_id": testWorkspaceID,
		"metadata":     metadata,
		"limit":        "100",
		"ids":          strings.Join(padded, ","),
	}
	postRecorder := httptest.NewRecorder()
	testHandler.QueryIssues(postRecorder, newRequest("POST", "/api/issues/query", postBody))
	postIDs, postTotal := decode(postRecorder, "POST")

	wantIDs := []string{issueA, issueB}
	sort.Strings(wantIDs)
	if fmt.Sprint(postIDs) != fmt.Sprint(wantIDs) || postTotal != 2 {
		t.Fatalf("POST ids = %v total = %d, want %v total 2", postIDs, postTotal, wantIDs)
	}

	getPath := fmt.Sprintf(
		"/api/issues?workspace_id=%s&limit=100&metadata=%s&ids=%s",
		testWorkspaceID, url.QueryEscape(metadata),
		url.QueryEscape(issueA+","+issueB),
	)
	getRecorder := httptest.NewRecorder()
	testHandler.ListIssues(getRecorder, newRequest("GET", getPath, nil))
	getIDs, getTotal := decode(getRecorder, "GET")

	if fmt.Sprint(postIDs) != fmt.Sprint(getIDs) || postTotal != getTotal {
		t.Fatalf("transport mismatch: POST %v/%d vs GET %v/%d", postIDs, postTotal, getIDs, getTotal)
	}

	// Malformed body fails closed.
	badRecorder := httptest.NewRecorder()
	badRequest := httptest.NewRequest("POST", "/api/issues/query", strings.NewReader("not json"))
	badRequest.Header.Set("X-User-ID", testUserID)
	badRequest.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.QueryIssues(badRecorder, badRequest)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: expected 400, got %d", badRecorder.Code)
	}
}

// Offset pages are only stable when the full ORDER BY is deterministic. All
// rows here share status, priority, AND created_at, so ordering falls
// entirely to the unique id tie-break — without it the database may reorder
// ties between two LIMIT/OFFSET requests, duplicating or dropping rows at
// page boundaries.
func TestListIssues_OffsetPaginationStableOnCreatedAtTies(t *testing.T) {
	ctx := context.Background()
	token := fmt.Sprintf("tie-page-%d", time.Now().UnixNano())
	metadata := fmt.Sprintf(`{"tie_page_test":%q}`, token)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE metadata @> $1::jsonb`, metadata)
	})

	nextNumber := func() int {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		return number
	}

	const totalIssues = 5
	want := make(map[string]bool, totalIssues)
	for i := 0; i < totalIssues; i++ {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				position, number, metadata, created_at, updated_at
			) VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5::jsonb,
				'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
			RETURNING id
		`, testWorkspaceID, fmt.Sprintf("%s %d", token, i), testUserID,
			nextNumber(), metadata).Scan(&id); err != nil {
			t.Fatalf("create issue %d: %v", i, err)
		}
		want[id] = true
	}

	page := func(offset int) []string {
		t.Helper()
		path := fmt.Sprintf(
			"/api/issues?workspace_id=%s&metadata=%s&sort=status&direction=asc&limit=2&offset=%d",
			testWorkspaceID, url.QueryEscape(metadata), offset,
		)
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssues offset=%d: expected 200, got %d: %s", offset, w.Code, w.Body.String())
		}
		var response struct {
			Issues []IssueResponse `json:"issues"`
			Total  int64           `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if response.Total != totalIssues {
			t.Fatalf("offset=%d total = %d, want %d", offset, response.Total, totalIssues)
		}
		ids := make([]string, 0, len(response.Issues))
		for _, issue := range response.Issues {
			ids = append(ids, issue.ID)
		}
		return ids
	}

	seen := make(map[string]int, totalIssues)
	var walked []string
	for offset := 0; offset < totalIssues; offset += 2 {
		for _, id := range page(offset) {
			seen[id]++
			walked = append(walked, id)
		}
	}
	if len(walked) != totalIssues {
		t.Fatalf("walked %d rows across pages, want %d (%v)", len(walked), totalIssues, walked)
	}
	for id := range want {
		if seen[id] != 1 {
			t.Fatalf("issue %s appeared %d times across pages, want exactly once", id, seen[id])
		}
	}
}
