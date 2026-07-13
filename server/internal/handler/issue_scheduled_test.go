package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Backs the Project Gantt view: only issues with at least one of
// start_date / due_date should come back when scheduled=true, regardless of
// status or assignee. The unfiltered call must keep returning everything.
func TestListIssues_ScheduledFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Seed three issues in a fresh project — one with start_date only, one
	// with due_date only, and one with neither. Using a dedicated project so
	// the assertion isn't polluted by other issues seeded by parallel tests.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Gantt Scheduled %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	insertIssue := func(title string, startDate, dueDate *time.Time) string {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id, start_date, due_date)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5, $6, $7) RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID, startDate, dueDate).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	start := time.Now().UTC().Truncate(24 * time.Hour)
	due := start.Add(72 * time.Hour)
	withStart := insertIssue(fmt.Sprintf("with-start-%d", suffix), &start, nil)
	withDue := insertIssue(fmt.Sprintf("with-due-%d", suffix), nil, &due)
	withBoth := insertIssue(fmt.Sprintf("with-both-%d", suffix), &start, &due)
	noDates := insertIssue(fmt.Sprintf("no-dates-%d", suffix), nil, nil)

	list := func(query string) (ids []string, total int64) {
		path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&limit=500%s",
			testWorkspaceID, projectID, query)
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Issues []IssueResponse `json:"issues"`
			Total  int64           `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		for _, iss := range resp.Issues {
			ids = append(ids, iss.ID)
		}
		return ids, resp.Total
	}

	// Without the filter every project issue comes back.
	allIDs, allTotal := list("")
	for _, want := range []string{withStart, withDue, withBoth, noDates} {
		if !containsIssueID(allIDs, want) {
			t.Fatalf("baseline list missing %s — all=%v", want, allIDs)
		}
	}
	if allTotal != 4 {
		t.Fatalf("baseline total: want 4, got %d", allTotal)
	}

	// With scheduled=true only the three dated issues should surface, and
	// CountIssues must agree so the frontend pagination logic stays sane.
	scheduledIDs, scheduledTotal := list("&scheduled=true")
	for _, want := range []string{withStart, withDue, withBoth} {
		if !containsIssueID(scheduledIDs, want) {
			t.Fatalf("scheduled list missing %s — got %v", want, scheduledIDs)
		}
	}
	if containsIssueID(scheduledIDs, noDates) {
		t.Fatalf("scheduled list unexpectedly includes undated issue %s", noDates)
	}
	if scheduledTotal != 3 {
		t.Fatalf("scheduled total: want 3, got %d", scheduledTotal)
	}
}

func TestListIssuesReturnsStage(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Stage List %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id, stage)
		VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5, 2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("stage-list-%d", suffix), testUserID, number, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&limit=500", testWorkspaceID, projectID)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if resp.Total != 1 || len(resp.Issues) != 1 {
		t.Fatalf("expected one staged issue, total=%d len=%d", resp.Total, len(resp.Issues))
	}
	if resp.Issues[0].ID != issueID {
		t.Fatalf("returned issue id = %s, want %s", resp.Issues[0].ID, issueID)
	}
	if resp.Issues[0].Stage == nil || *resp.Issues[0].Stage != 2 {
		t.Fatalf("stage = %#v, want 2", resp.Issues[0].Stage)
	}
}

func TestIssueStageConsistentAcrossListGetAndChildren(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Stage Consistency %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

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

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
		VALUES ($1, $2, 'in_progress', 'none', 'member', $3, 0, $4, $5)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("stage-parent-%d", suffix), testUserID, nextNumber(), projectID).Scan(&parentID); err != nil {
		t.Fatalf("create parent issue: %v", err)
	}

	var childID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, parent_issue_id, position, number, project_id, stage)
		VALUES ($1, $2, 'todo', 'none', 'member', $3, $4, 0, $5, $6, 2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("stage-child-%d", suffix), testUserID, parentID, nextNumber(), projectID).Scan(&childID); err != nil {
		t.Fatalf("create child issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, childID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parentID)
	})

	findIssue := func(issues []IssueResponse, id string) IssueResponse {
		t.Helper()
		for _, issue := range issues {
			if issue.ID == id {
				return issue
			}
		}
		t.Fatalf("issue %s not found in response", id)
		return IssueResponse{}
	}

	wantStage := int32(2)

	listReq := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&limit=500", testWorkspaceID, projectID)
	listW := httptest.NewRecorder()
	testHandler.ListIssues(listW, newRequest("GET", listReq, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	listIssue := findIssue(listResp.Issues, childID)
	if listIssue.Stage == nil || *listIssue.Stage != wantStage {
		t.Fatalf("list stage = %#v, want %d", listIssue.Stage, wantStage)
	}

	getW := httptest.NewRecorder()
	testHandler.GetIssue(getW, withURLParam(newRequest("GET", "/api/issues/"+childID, nil), "id", childID))
	if getW.Code != http.StatusOK {
		t.Fatalf("GetIssue: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	var getResp IssueResponse
	if err := json.NewDecoder(getW.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.Stage == nil || *getResp.Stage != wantStage {
		t.Fatalf("get stage = %#v, want %d", getResp.Stage, wantStage)
	}

	childrenW := httptest.NewRecorder()
	testHandler.ListChildIssues(childrenW, withURLParam(newRequest("GET", "/api/issues/"+parentID+"/children", nil), "id", parentID))
	if childrenW.Code != http.StatusOK {
		t.Fatalf("ListChildIssues: expected 200, got %d: %s", childrenW.Code, childrenW.Body.String())
	}
	var childrenResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(childrenW.Body).Decode(&childrenResp); err != nil {
		t.Fatalf("decode children response: %v", err)
	}
	childFromChildren := findIssue(childrenResp.Issues, childID)
	if childFromChildren.Stage == nil || *childFromChildren.Stage != wantStage {
		t.Fatalf("children stage = %#v, want %d", childFromChildren.Stage, wantStage)
	}

	if listIssue.Stage == nil || getResp.Stage == nil || childFromChildren.Stage == nil {
		t.Fatal("expected all three endpoints to include stage")
	}
	if *listIssue.Stage != *getResp.Stage || *getResp.Stage != *childFromChildren.Stage {
		t.Fatalf("stage mismatch: list=%d get=%d children=%d", *listIssue.Stage, *getResp.Stage, *childFromChildren.Stage)
	}
}
