package deptsync

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientListDepartmentUsersIncludesChildrenAndQueryKey(t *testing.T) {
	var sawQueryKey string
	var sawPath string
	var sawIncludeChildren string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawQueryKey = r.Header.Get("X-Query-Key")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/department/tree":
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": [
					{
						"dept_id": "D0",
						"dept_name": "Root Company",
						"children": [
							{"dept_id": "D1", "dept_name": "Platform"}
						]
					}
				]
			}`))
		case "/department/D1/users":
			sawPath = r.URL.Path
			sawIncludeChildren = r.URL.Query().Get("include_children")
			_, _ = w.Write([]byte(`{
				"success": true,
				"code": "",
				"data": [
					{
						"user_id": "E001",
						"username": "Alice",
						"universal_id": "u-1",
						"dept_id": "D1",
						"dept_name": "Platform",
						"is_main": 1,
						"position": "Engineer",
						"status": 1,
						"dept_path": "/D0/D1"
					}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	users, err := client.ListDepartmentUsers(t.Context(), "D1", true)
	if err != nil {
		t.Fatalf("ListDepartmentUsers: %v", err)
	}
	if sawQueryKey != "secret" {
		t.Fatalf("expected X-Query-Key header, got %q", sawQueryKey)
	}
	if sawPath != "/department/D1/users" {
		t.Fatalf("unexpected path %q", sawPath)
	}
	if sawIncludeChildren != "true" {
		t.Fatalf("expected include_children=true, got %q", sawIncludeChildren)
	}
	if len(users) != 1 || users[0].UniversalID != "u-1" || users[0].Position != "Engineer" {
		t.Fatalf("unexpected users: %+v", users)
	}
	if users[0].DeptPath != "Platform" {
		t.Fatalf("expected display dept path, got %q", users[0].DeptPath)
	}
}

func TestClientListDepartmentUsersToleratesNullableUserFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": "",
			"data": [
				{
					"user_id": "29219",
					"username": "Alice",
					"universal_id": null,
					"dept_id": "6571",
					"dept_name": "开发组",
					"is_main": 1,
					"position": null,
					"status": 1,
					"dept_path": null
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	users, err := client.ListDepartmentUsers(t.Context(), "6571", true)
	if err != nil {
		t.Fatalf("ListDepartmentUsers: %v", err)
	}
	if len(users) != 1 || users[0].UserID != "29219" || users[0].UniversalID != "" || users[0].Position != "" {
		t.Fatalf("unexpected users: %+v", users)
	}
}

func TestClientSearchUsersCallsSearchEndpoint(t *testing.T) {
	var sawQueryKey string
	var sawPath string
	var sawQ string
	var sawLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawQueryKey = r.Header.Get("X-Query-Key")
		sawPath = r.URL.Path
		sawQ = r.URL.Query().Get("q")
		sawLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": "",
			"data": [
				{
					"user_id": "E001",
					"username": "Alice Platform",
					"universal_id": "u-1",
					"dept_id": "D110",
					"dept_name": "客户成功组",
					"is_main": 1,
					"position": "Engineer",
					"status": 1,
					"dept_path": "研发体系/Costrict研发部/客户成功组"
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	users, err := client.SearchUsers(t.Context(), "E001", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if sawQueryKey != "secret" {
		t.Fatalf("expected X-Query-Key header, got %q", sawQueryKey)
	}
	if sawPath != "/users/search" {
		t.Fatalf("unexpected path %q (want /users/search, no tree fetch)", sawPath)
	}
	if sawQ != "E001" {
		t.Fatalf("unexpected q %q", sawQ)
	}
	if sawLimit != "20" {
		t.Fatalf("unexpected limit %q", sawLimit)
	}
	if len(users) != 1 || users[0].UserID != "E001" || users[0].Username != "Alice Platform" {
		t.Fatalf("unexpected users: %+v", users)
	}
	// dept_path now comes straight from the server; no client-side rebuild.
	if users[0].DeptPath != "研发体系/Costrict研发部/客户成功组" {
		t.Fatalf("expected server-provided dept_path, got %q", users[0].DeptPath)
	}
}

func TestClientSearchUsersClampsLimit(t *testing.T) {
	var sawLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": []}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	if _, err := client.SearchUsers(t.Context(), "x", 0); err != nil {
		t.Fatalf("SearchUsers limit=0: %v", err)
	}
	if sawLimit != "20" {
		t.Fatalf("expected clamped limit 20 for zero, got %q", sawLimit)
	}
	if _, err := client.SearchUsers(t.Context(), "x", 999); err != nil {
		t.Fatalf("SearchUsers limit=999: %v", err)
	}
	if sawLimit != "20" {
		t.Fatalf("expected clamped limit 20 for over-max, got %q", sawLimit)
	}
}

func TestClientSearchDepartmentsCallsSearchEndpoint(t *testing.T) {
	var sawQueryKey string
	var sawPath string
	var sawQ string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawQueryKey = r.Header.Get("X-Query-Key")
		sawPath = r.URL.Path
		sawQ = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"dept_id": "D100",
					"dept_name": "Platform Dept",
					"dept_path": "/D000/D010/D100",
					"parent_dept_id": "D010",
					"dept_level": 3,
					"child_dept_count": 1
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	departments, err := client.SearchDepartments(t.Context(), "plat", 10)
	if err != nil {
		t.Fatalf("SearchDepartments: %v", err)
	}
	if sawQueryKey != "secret" {
		t.Fatalf("expected X-Query-Key header, got %q", sawQueryKey)
	}
	if sawPath != "/departments/search" {
		t.Fatalf("unexpected path %q (want /departments/search)", sawPath)
	}
	if sawQ != "plat" {
		t.Fatalf("unexpected q %q", sawQ)
	}
	if len(departments) != 1 || departments[0].DeptID != "D100" || departments[0].DeptName != "Platform Dept" {
		t.Fatalf("unexpected departments: %+v", departments)
	}
	if departments[0].DeptPath != "/D000/D010/D100" {
		t.Fatalf("expected server-provided dept_path, got %q", departments[0].DeptPath)
	}
}

func TestClientGetDepartmentReturnsCanonicalDepartmentFromTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"dept_id": "D000",
					"dept_name": "Root",
					"dept_path": "/D000",
					"children": [
						{
							"dept_id": "D100",
							"dept_name": "Platform Dept",
							"dept_path": "/D000/D100",
							"parent_dept_id": "D000"
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	department, err := client.GetDepartment(t.Context(), "D100")
	if err != nil {
		t.Fatalf("GetDepartment: %v", err)
	}
	if department == nil {
		t.Fatal("expected department, got nil")
	}
	if department.DeptName != "Platform Dept" || department.DeptPath != "/D000/D100" {
		t.Fatalf("unexpected department: %+v", department)
	}
}
