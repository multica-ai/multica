package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBatchCreateIssuesValidMemberAssignedBatchCreatesIssue(t *testing.T) {
	projectID := createBatchCreateProject(t, testWorkspaceID, "Batch Create Valid Project")
	title := uniqueBatchCreateTitle("valid-member")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"confirm_batch_create": true,
		"issues": []map[string]any{{
			"title":         "  " + title + "  ",
			"description":   "Markdown\nbody",
			"status":        "todo",
			"assignee_type": "member",
			"assignee_id":   testUserID,
			"project_id":    projectID,
		}},
	})
	testHandler.BatchCreateIssues(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeBatchCreateIssuesResponse(t, w)
	if !resp.Valid || resp.Created != 1 || resp.RowCount != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(resp.Issues) != 1 {
		t.Fatalf("expected one created issue, got %d", len(resp.Issues))
	}
	created := resp.Issues[0]
	t.Cleanup(func() { deleteBatchCreatedIssue(t, created.ID) })
	if created.Title != title {
		t.Fatalf("expected trimmed title %q, got %q", title, created.Title)
	}
	if created.Status != "todo" {
		t.Fatalf("expected status todo, got %q", created.Status)
	}
	if created.AssigneeID == nil || *created.AssigneeID != testUserID {
		t.Fatalf("expected assignee_id user id %q, got %+v", testUserID, created.AssigneeID)
	}
	if created.ProjectID == nil || *created.ProjectID != projectID {
		t.Fatalf("expected project_id %q, got %+v", projectID, created.ProjectID)
	}
	if countIssuesWithExactTitle(t, title) != 1 {
		t.Fatalf("expected exactly one persisted issue with title %q", title)
	}
}

func TestBatchCreateIssuesMultiRowDeterministicNumbers(t *testing.T) {
	prefix := uniqueBatchCreateTitle("deterministic")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"confirm_batch_create": true,
		"issues": []map[string]any{
			{"title": prefix + " A"},
			{"title": prefix + " B"},
			{"title": prefix + " C"},
		},
	})
	testHandler.BatchCreateIssues(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBatchCreateIssuesResponse(t, w)
	if len(resp.Issues) != 3 {
		t.Fatalf("expected 3 issues, got %d", len(resp.Issues))
	}
	for _, issue := range resp.Issues {
		t.Cleanup(func(id string) func() { return func() { deleteBatchCreatedIssue(t, id) } }(issue.ID))
	}
	for i, issue := range resp.Issues {
		wantTitle := fmt.Sprintf("%s %c", prefix, 'A'+rune(i))
		if issue.Title != wantTitle {
			t.Fatalf("row %d title order changed: got %q want %q", i+1, issue.Title, wantTitle)
		}
		if i > 0 && issue.Number != resp.Issues[i-1].Number+1 {
			t.Fatalf("issue numbers are not contiguous in request order: %+v", resp.Issues)
		}
		if !strings.HasPrefix(issue.Identifier, "HAN-") {
			t.Fatalf("expected handler-test issue prefix in identifier, got %q", issue.Identifier)
		}
	}
}

func TestBatchCreateIssuesBlankStatusDefaultsAndValidateOnlyWritesNothing(t *testing.T) {
	title := uniqueBatchCreateTitle("validate-only")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"validate_only": true,
		"issues": []map[string]any{{
			"title":  title,
			"status": " ",
		}},
	})
	testHandler.BatchCreateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBatchCreateIssuesResponse(t, w)
	if !resp.Valid || resp.RowCount != 1 || len(resp.Rows) != 1 {
		t.Fatalf("unexpected validation response: %+v", resp)
	}
	if resp.Rows[0].Status != "todo" {
		t.Fatalf("expected blank status to default to todo, got %q", resp.Rows[0].Status)
	}
	if countIssuesWithExactTitle(t, title) != 0 {
		t.Fatalf("validate_only created an issue with title %q", title)
	}
}

func TestBatchCreateIssuesValidationErrorsCreateZeroIssues(t *testing.T) {
	cases := []struct {
		name      string
		row       map[string]any
		wantField string
		wantCode  string
	}{
		{
			name:      "invalid_status",
			row:       map[string]any{"title": "bad status", "status": "triage"},
			wantField: "status",
			wantCode:  "invalid_status",
		},
		{
			name:      "missing_title",
			row:       map[string]any{"description": "no title"},
			wantField: "title",
			wantCode:  "required",
		},
		{
			name:      "assignee_type_without_id",
			row:       map[string]any{"title": "only type", "assignee_type": "member"},
			wantField: "assignee_id",
			wantCode:  "assignee_pair_required",
		},
		{
			name:      "assignee_id_without_type",
			row:       map[string]any{"title": "only id", "assignee_id": testUserID},
			wantField: "assignee_type",
			wantCode:  "assignee_pair_required",
		},
		{
			name:      "unsupported_field",
			row:       map[string]any{"title": "priority unsupported", "priority": "high"},
			wantField: "priority",
			wantCode:  "unsupported_field",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefix := uniqueBatchCreateTitle(tc.name)
			validTitle := prefix + " valid row"
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues/batch-create", map[string]any{
				"confirm_batch_create": true,
				"issues": []map[string]any{
					{"title": validTitle},
					tc.row,
				},
			})
			before := countIssuesWithTitlePrefix(t, prefix)
			testHandler.BatchCreateIssues(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			resp := decodeBatchCreateIssuesResponse(t, w)
			assertBatchCreateError(t, resp, 2, tc.wantField, tc.wantCode)
			after := countIssuesWithTitlePrefix(t, prefix)
			if after != before {
				t.Fatalf("validation failure created issues: before=%d after=%d", before, after)
			}
		})
	}
}

func TestBatchCreateIssuesMemberAssigneeUsesUserIDNotMemberID(t *testing.T) {
	memberID := handlerTestMemberID(t)
	title := uniqueBatchCreateTitle("member-id")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"confirm_batch_create": true,
		"issues": []map[string]any{{
			"title":         title,
			"assignee_type": "member",
			"assignee_id":   memberID,
		}},
	})
	testHandler.BatchCreateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBatchCreateIssuesResponse(t, w)
	assertBatchCreateError(t, resp, 1, "assignee_id", "assignee_not_found")
	if countIssuesWithExactTitle(t, title) != 0 {
		t.Fatalf("member.id assignee request created issue")
	}
}

func TestBatchCreateIssuesRejectsArchivedAgent(t *testing.T) {
	agentID := createArchivedBatchCreateAgent(t)
	title := uniqueBatchCreateTitle("archived-agent")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"validate_only": true,
		"issues": []map[string]any{{
			"title":         title,
			"assignee_type": "agent",
			"assignee_id":   agentID,
		}},
	})
	testHandler.BatchCreateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBatchCreateIssuesResponse(t, w)
	assertBatchCreateError(t, resp, 1, "assignee_id", "assignee_archived")
}

func TestBatchCreateIssuesRejectsForeignWorkspaceProject(t *testing.T) {
	projectID := createForeignBatchCreateProject(t)
	title := uniqueBatchCreateTitle("foreign-project")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"confirm_batch_create": true,
		"issues": []map[string]any{{
			"title":      title,
			"project_id": projectID,
		}},
	})
	testHandler.BatchCreateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBatchCreateIssuesResponse(t, w)
	assertBatchCreateError(t, resp, 1, "project_id", "project_not_found")
	if countIssuesWithExactTitle(t, title) != 0 {
		t.Fatalf("foreign project validation failure created issue")
	}
}

func TestBatchCreateIssuesRowLimit(t *testing.T) {
	cases := []struct {
		name      string
		envValue  string
		rows      int
		wantCode  int
		wantLimit int
	}{
		{name: "default", envValue: "", rows: 1001, wantCode: http.StatusBadRequest, wantLimit: 1000},
		{name: "override", envValue: "1", rows: 2, wantCode: http.StatusBadRequest, wantLimit: 1},
		{name: "invalid_env_fallback", envValue: "nope", rows: 1001, wantCode: http.StatusBadRequest, wantLimit: 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(batchIssueCreateLimitEnv, tc.envValue)
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues/batch-create", map[string]any{
				"validate_only": true,
				"issues":        makeBatchCreateRows(tc.rows, uniqueBatchCreateTitle(tc.name)),
			})
			testHandler.BatchCreateIssues(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
			resp := decodeBatchCreateIssuesResponse(t, w)
			if resp.Limit != tc.wantLimit {
				t.Fatalf("expected limit %d, got %d", tc.wantLimit, resp.Limit)
			}
			assertBatchCreateError(t, resp, 0, "issues", "row_limit_exceeded")
		})
	}
}

func TestBatchCreateIssuesRequiresConfirmationForCreate(t *testing.T) {
	title := uniqueBatchCreateTitle("confirmation")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"issues": []map[string]any{{"title": title}},
	})
	testHandler.BatchCreateIssues(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Error          string `json:"error"`
		RowCount       int    `json:"row_count"`
		AgentTaskCount int    `json:"agent_task_count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "confirmation_required" || resp.RowCount != 1 {
		t.Fatalf("unexpected confirmation response: %+v", resp)
	}
	if countIssuesWithExactTitle(t, title) != 0 {
		t.Fatalf("unconfirmed create request created issue")
	}
}

func TestBatchCreateIssuesAgentTaskSummaryAndEnqueue(t *testing.T) {
	agentID := handlerTestAgentID(t)
	backlogTitle := uniqueBatchCreateTitle("agent-backlog")
	todoTitle := uniqueBatchCreateTitle("agent-todo")
	payloadRows := []map[string]any{
		{
			"title":         backlogTitle,
			"status":        "backlog",
			"assignee_type": "agent",
			"assignee_id":   agentID,
		},
		{
			"title":         todoTitle,
			"status":        "todo",
			"assignee_type": "agent",
			"assignee_id":   agentID,
		},
	}

	validateW := httptest.NewRecorder()
	validateReq := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"validate_only": true,
		"issues":        payloadRows,
	})
	testHandler.BatchCreateIssues(validateW, validateReq)
	if validateW.Code != http.StatusOK {
		t.Fatalf("expected validate 200, got %d: %s", validateW.Code, validateW.Body.String())
	}
	validateResp := decodeBatchCreateIssuesResponse(t, validateW)
	if validateResp.AgentTaskCount != 1 {
		t.Fatalf("expected one agent task in summary, got %d", validateResp.AgentTaskCount)
	}
	if validateResp.Rows[0].WillEnqueueAgentTask {
		t.Fatalf("backlog agent row should not enqueue")
	}
	if !validateResp.Rows[1].WillEnqueueAgentTask {
		t.Fatalf("todo agent row should enqueue")
	}

	createW := httptest.NewRecorder()
	createReq := newRequest("POST", "/api/issues/batch-create", map[string]any{
		"confirm_batch_create": true,
		"issues":               payloadRows,
	})
	testHandler.BatchCreateIssues(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createW.Code, createW.Body.String())
	}
	createResp := decodeBatchCreateIssuesResponse(t, createW)
	if createResp.AgentTaskCount != 1 || createResp.Created != 2 {
		t.Fatalf("unexpected create response: %+v", createResp)
	}
	for _, issue := range createResp.Issues {
		t.Cleanup(func(id string) func() { return func() { deleteBatchCreatedIssue(t, id) } }(issue.ID))
	}
	if countTasksForIssue(t, createResp.Issues[0].ID) != 0 {
		t.Fatalf("backlog agent issue enqueued a task")
	}
	if countTasksForIssue(t, createResp.Issues[1].ID) != 1 {
		t.Fatalf("todo agent issue did not enqueue exactly one task")
	}
}

func uniqueBatchCreateTitle(prefix string) string {
	return fmt.Sprintf("BatchCreate %s %d", prefix, time.Now().UnixNano())
}

func decodeBatchCreateIssuesResponse(t *testing.T, w *httptest.ResponseRecorder) BatchCreateIssuesResponse {
	t.Helper()
	var resp BatchCreateIssuesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode batch create response: %v", err)
	}
	return resp
}

func assertBatchCreateError(t *testing.T, resp BatchCreateIssuesResponse, row int, field string, code string) {
	t.Helper()
	for _, err := range resp.Errors {
		if err.Row == row && err.Field == field && err.Code == code {
			return
		}
	}
	t.Fatalf("missing batch error row=%d field=%s code=%s in %+v", row, field, code, resp.Errors)
}

func makeBatchCreateRows(count int, titlePrefix string) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, map[string]any{"title": fmt.Sprintf("%s row %d", titlePrefix, i+1)})
	}
	return rows
}

func countIssuesWithExactTitle(t *testing.T, title string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID,
		title,
	).Scan(&count); err != nil {
		t.Fatalf("count issues by exact title: %v", err)
	}
	return count
}

func countIssuesWithTitlePrefix(t *testing.T, titlePrefix string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title LIKE $2`,
		testWorkspaceID,
		titlePrefix+"%",
	).Scan(&count); err != nil {
		t.Fatalf("count issues by title prefix: %v", err)
	}
	return count
}

func countTasksForIssue(t *testing.T, issueID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`,
		issueID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks for issue: %v", err)
	}
	return count
}

func deleteBatchCreatedIssue(t *testing.T, issueID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete batch-created issue: %v", err)
	}
}

func createBatchCreateProject(t *testing.T, workspaceID string, title string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO project (workspace_id, title, description, icon, status)
		 VALUES ($1, $2, '', 'circle', 'planned')
		 RETURNING id`,
		workspaceID,
		title,
	).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func createForeignBatchCreateProject(t *testing.T) string {
	t.Helper()
	suffix := time.Now().UnixNano()
	var workspaceID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO workspace (name, slug, description, issue_prefix)
		 VALUES ($1, $2, '', 'FOR')
		 RETURNING id`,
		"Batch Create Foreign",
		fmt.Sprintf("batch-create-foreign-%d", suffix),
	).Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	return createBatchCreateProject(t, workspaceID, "Foreign Batch Project")
}

func handlerTestMemberID(t *testing.T) string {
	t.Helper()
	var memberID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID,
		testUserID,
	).Scan(&memberID); err != nil {
		t.Fatalf("load handler test member id: %v", err)
	}
	return memberID
}

func handlerTestAgentID(t *testing.T) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent' ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load handler test agent id: %v", err)
	}
	return agentID
}

func createArchivedBatchCreateAgent(t *testing.T) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id, archived_at
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, now())
		RETURNING id
	`, testWorkspaceID, uniqueBatchCreateTitle("archived-agent"), testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create archived agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}
