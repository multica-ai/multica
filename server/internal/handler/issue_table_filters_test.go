package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
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
}
