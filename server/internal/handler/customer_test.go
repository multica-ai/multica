package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomerCRUDAndProjectUnlink(t *testing.T) {
	req := newRequest("POST", "/api/customers?workspace_id="+testWorkspaceID, map[string]any{
		"name":        "Acme Customer",
		"description": "Strategic account",
		"website":     "https://acme.test",
		"email":       "team@acme.test",
		"phone":       "+1 555 0100",
	})
	w := httptest.NewRecorder()
	testHandler.CreateCustomer(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateCustomer: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var customer CustomerResponse
	if err := json.NewDecoder(w.Body).Decode(&customer); err != nil {
		t.Fatalf("decode customer: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM customer WHERE id = $1`, customer.ID)
	})

	req = newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Customer linked project",
		"customer_id": customer.ID,
	})
	w = httptest.NewRecorder()
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject with customer: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})
	if project.CustomerID == nil || *project.CustomerID != customer.ID {
		t.Fatalf("CreateProject customer_id = %v, want %q", project.CustomerID, customer.ID)
	}

	req = newRequest("GET", "/api/customers/"+customer.ID, nil)
	req = withURLParam(req, "id", customer.ID)
	w = httptest.NewRecorder()
	testHandler.GetCustomer(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetCustomer: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got CustomerResponse
	json.NewDecoder(w.Body).Decode(&got)
	if got.ProjectCount != 1 {
		t.Fatalf("project_count = %d, want 1", got.ProjectCount)
	}

	req = newRequest("PUT", "/api/customers/"+customer.ID, map[string]any{
		"name":        "Acme Renamed",
		"description": nil,
		"status":      "archived",
	})
	req = withURLParam(req, "id", customer.ID)
	w = httptest.NewRecorder()
	testHandler.UpdateCustomer(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateCustomer: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&got)
	if got.Name != "Acme Renamed" || got.Status != "archived" || got.Description != nil {
		t.Fatalf("unexpected updated customer: %+v", got)
	}

	req = newRequest("DELETE", "/api/customers/"+customer.ID, nil)
	req = withURLParam(req, "id", customer.ID)
	w = httptest.NewRecorder()
	testHandler.DeleteCustomer(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteCustomer: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	req = newRequest("GET", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	w = httptest.NewRecorder()
	testHandler.GetProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProject after customer delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var afterDelete ProjectResponse
	json.NewDecoder(w.Body).Decode(&afterDelete)
	if afterDelete.CustomerID != nil {
		t.Fatalf("project customer_id after delete = %v, want nil", afterDelete.CustomerID)
	}
}

func TestProjectRejectsForeignWorkspaceCustomer(t *testing.T) {
	ctx := context.Background()
	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Foreign Customer Workspace', 'foreign-customer-workspace', '', 'FCW')
		RETURNING id
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})

	var foreignCustomerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO customer (workspace_id, name)
		VALUES ($1, 'Foreign Customer')
		RETURNING id
	`, foreignWorkspaceID).Scan(&foreignCustomerID); err != nil {
		t.Fatalf("create foreign customer: %v", err)
	}

	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Cross workspace customer project",
		"customer_id": foreignCustomerID,
	})
	w := httptest.NewRecorder()
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("CreateProject with foreign customer: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
