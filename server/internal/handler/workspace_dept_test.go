package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/deptsync"
)

type fakeWorkspaceDeptClient struct {
	users       []deptsync.User
	departments []deptsync.Department
}

func (f fakeWorkspaceDeptClient) Configured() bool { return true }

func (f fakeWorkspaceDeptClient) ListDepartmentUsers(ctx context.Context, deptID string, includeChildren bool) ([]deptsync.User, error) {
	return f.users, nil
}

func (f fakeWorkspaceDeptClient) SearchUsers(ctx context.Context, query string, limit int) ([]deptsync.User, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]deptsync.User, 0, len(f.users))
	for _, user := range f.users {
		if query == "" ||
			strings.Contains(strings.ToLower(user.Username), query) ||
			strings.Contains(strings.ToLower(user.UserID), query) {
			out = append(out, user)
		}
	}
	return out, nil
}

func (f fakeWorkspaceDeptClient) SearchDepartments(ctx context.Context, query string, limit int) ([]deptsync.Department, error) {
	return f.departments, nil
}

func TestSearchDeptDepartmentsReturnsRealDepartmentResults(t *testing.T) {
	prev := testHandler.DeptSync
	testHandler.DeptSync = fakeWorkspaceDeptClient{departments: []deptsync.Department{
		{DeptID: "D100", DeptName: "Platform Dept", DeptPath: "/D000/D100"},
	}}
	t.Cleanup(func() { testHandler.DeptSync = prev })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/dept/departments/search?q=platform", nil)
	testHandler.SearchDeptDepartments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchDeptDepartments: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Platform Dept") || !strings.Contains(w.Body.String(), "D100") {
		t.Fatalf("expected canonical department result, got %s", w.Body.String())
	}
}

func TestSearchDeptDepartmentsReturnsInitialDepartmentsForEmptyQuery(t *testing.T) {
	prev := testHandler.DeptSync
	testHandler.DeptSync = fakeWorkspaceDeptClient{departments: []deptsync.Department{
		{DeptID: "D100", DeptName: "Platform Dept", DeptPath: "/D000/D100"},
	}}
	t.Cleanup(func() { testHandler.DeptSync = prev })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/dept/departments/search", nil)
	testHandler.SearchDeptDepartments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchDeptDepartments: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Platform Dept") {
		t.Fatalf("expected initial department result, got %s", w.Body.String())
	}
}

func TestSearchDeptUsersReturnsNameAndEmployeeMatches(t *testing.T) {
	prev := testHandler.DeptSync
	testHandler.DeptSync = fakeWorkspaceDeptClient{users: []deptsync.User{
		{UserID: "E001", Username: "Active Dept User", UniversalID: "uni-active", DeptID: "D100", DeptName: "Platform", Position: "Engineer", Status: 1},
		{UserID: "29219", Username: "Universal Only User", UniversalID: "bcdce73f-0f2c-4699-ad21-501a4bc13245", DeptID: "D100", DeptName: "Costrict", Position: "Engineer", Status: 1},
	}}
	t.Cleanup(func() { testHandler.DeptSync = prev })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/dept/users/search?q=E001", nil)
	testHandler.SearchDeptUsers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchDeptUsers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Active Dept User") || !strings.Contains(w.Body.String(), "E001") {
		t.Fatalf("expected dept user result, got %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/dept/users/search?q=Dept", nil)
	testHandler.SearchDeptUsers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchDeptUsers by partial name: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Active Dept User") {
		t.Fatalf("expected partial name match, got %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/dept/users/search?q=001", nil)
	testHandler.SearchDeptUsers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchDeptUsers by partial employee id: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "E001") {
		t.Fatalf("expected partial employee id match, got %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/dept/users/search?q=c", nil)
	testHandler.SearchDeptUsers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchDeptUsers by universal id character: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Universal Only User") {
		t.Fatalf("did not expect universal id-only match, got %s", w.Body.String())
	}
}

func TestListDeptDepartmentUsersReturnsRecursiveMembers(t *testing.T) {
	prev := testHandler.DeptSync
	testHandler.DeptSync = fakeWorkspaceDeptClient{users: []deptsync.User{
		{UserID: "E004", Username: "Runtime Dept User", UniversalID: "uni-runtime", DeptID: "D110", DeptName: "Platform Runtime", Position: "SRE", Status: 1},
	}}
	t.Cleanup(func() { testHandler.DeptSync = prev })

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/dept/departments/D100/users", nil), "id", "D100")
	testHandler.ListDeptDepartmentUsers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListDeptDepartmentUsers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Runtime Dept User") || !strings.Contains(w.Body.String(), "Platform Runtime") {
		t.Fatalf("expected recursive department user result, got %s", w.Body.String())
	}
}

func TestBatchAddDeptMembersAddsResolvedAndPendingUsers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "handler-batch-add-dept-members"
	_, _ = testPool.Exec(ctx, `DELETE FROM multica_workspace WHERE slug = $1`, slug)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM multica_workspace WHERE slug = $1`, slug)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM multica_user WHERE email = 'batch-dept-resolved@example.test'`)
	})

	var resolvedUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_user (name, email, casdoor_universal_id)
		VALUES ('Resolved Batch Dept User', 'batch-dept-resolved@example.test', 'uni-batch-resolved')
		RETURNING id
	`).Scan(&resolvedUserID); err != nil {
		t.Fatalf("create resolved user: %v", err)
	}

	prev := testHandler.DeptSync
	testHandler.DeptSync = fakeWorkspaceDeptClient{users: []deptsync.User{
		{UserID: "E010", Username: "Resolved Batch Dept User", UniversalID: "uni-batch-resolved", DeptID: "D100", DeptName: "Platform", DeptPath: "/D000/D100", Position: "Engineer", Status: 1, IsMain: 1},
		{UserID: "E011", Username: "Pending Batch Dept User", UniversalID: "uni-batch-pending", DeptID: "D100", DeptName: "Platform", DeptPath: "/D000/D100", Position: "Designer", Status: 1, IsMain: 1},
	}}
	t.Cleanup(func() { testHandler.DeptSync = prev })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/workspaces", map[string]any{
		"name": "Batch Dept Members",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var workspaceID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM multica_workspace WHERE slug = $1`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("lookup workspace: %v", err)
	}

	w = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/dept-members", map[string]any{
		"users": []map[string]string{
			{"external_user_id": "E010", "external_universal_id": "uni-batch-resolved"},
			{"external_user_id": "E011", "external_universal_id": "uni-batch-pending"},
		},
	}), "id", workspaceID)
	testHandler.BatchAddDeptMembers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchAddDeptMembers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"added":2`) {
		t.Fatalf("expected added count, got %s", w.Body.String())
	}

	var resolvedStatus, pendingStatus, pendingDept, pendingEmployee string
	var resolvedMemberUserID, pendingMemberUserID *string
	if err := testPool.QueryRow(ctx, `
		SELECT status, user_id FROM multica_member
		WHERE workspace_id = $1 AND external_universal_id = 'uni-batch-resolved'
	`, workspaceID).Scan(&resolvedStatus, &resolvedMemberUserID); err != nil {
		t.Fatalf("lookup resolved member: %v", err)
	}
	if resolvedStatus != "active" || resolvedMemberUserID == nil || *resolvedMemberUserID != resolvedUserID {
		t.Fatalf("resolved member mismatch: status=%q user_id=%v want active %s", resolvedStatus, resolvedMemberUserID, resolvedUserID)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT status, user_id, dept_name, employee_id FROM multica_member
		WHERE workspace_id = $1 AND external_universal_id = 'uni-batch-pending'
	`, workspaceID).Scan(&pendingStatus, &pendingMemberUserID, &pendingDept, &pendingEmployee); err != nil {
		t.Fatalf("lookup pending member: %v", err)
	}
	if pendingStatus != "pending_activation" || pendingMemberUserID != nil || pendingDept != "Platform" || pendingEmployee != "E011" {
		t.Fatalf("pending member mismatch: status=%q user_id=%v dept=%q employee=%q", pendingStatus, pendingMemberUserID, pendingDept, pendingEmployee)
	}
}
