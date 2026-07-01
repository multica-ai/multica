package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

const testNanoClawToken = "0123456789abcdef0123456789abcdef"

func TestNanoClawBridgeRequiresBearerToken(t *testing.T) {
	bridge := &nanoclawBridge{token: testNanoClawToken}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	bridge.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestNanoClawBridgeCreatesIssueForNamedSquad(t *testing.T) {
	var created map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			json.NewEncoder(w).Encode([]map[string]any{{"id": "agent-1", "name": "Backend"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{{"id": "squad-1", "name": "AWG Service"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "status": "todo"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	bridge := &nanoclawBridge{
		client: cli.NewAPIClient(api.URL, "workspace-1", "multica-token"),
		token:  testNanoClawToken,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/issues", strings.NewReader(`{
		"title":"Fix billing",
		"description":"Handle retries",
		"assignee":"AWG Service",
		"assignee_kind":"squad",
		"status":"todo"
	}`))
	req.Header.Set("Authorization", "Bearer "+testNanoClawToken)
	rec := httptest.NewRecorder()

	bridge.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := created["assignee_type"]; got != "squad" {
		t.Fatalf("assignee_type = %#v, want squad", got)
	}
	if got := created["assignee_id"]; got != "squad-1" {
		t.Fatalf("assignee_id = %#v, want squad-1", got)
	}
	if got := created["status"]; got != "todo" {
		t.Fatalf("status = %#v, want todo", got)
	}
}

func TestNanoClawBridgeRejectsAmbiguousAssignee(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "agent-1", "name": "Backend One"},
				{"id": "agent-2", "name": "Backend Two"},
			})
		case "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	bridge := &nanoclawBridge{
		client: cli.NewAPIClient(api.URL, "workspace-1", "multica-token"),
		token:  testNanoClawToken,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/issues", strings.NewReader(`{
		"title":"Fix API",
		"assignee":"Backend",
		"assignee_kind":"agent"
	}`))
	req.Header.Set("Authorization", "Bearer "+testNanoClawToken)
	rec := httptest.NewRecorder()

	bridge.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ambiguous assignee") {
		t.Fatalf("body = %q, want ambiguity detail", rec.Body.String())
	}
}

func TestNanoClawBridgeGetsIssueStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || (r.URL.Path != "/api/issues/A-26" && r.URL.Path != "/api/issues/issue-26") {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":         "issue-26",
			"identifier": "A-26",
			"status":     "in_progress",
		})
	}))
	defer api.Close()

	bridge := &nanoclawBridge{
		client: cli.NewAPIClient(api.URL, "workspace-1", "multica-token"),
		token:  testNanoClawToken,
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/issues/A-26", nil)
	req.Header.Set("Authorization", "Bearer "+testNanoClawToken)
	rec := httptest.NewRecorder()

	bridge.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"in_progress"`) {
		t.Fatalf("body = %q, want issue status", rec.Body.String())
	}
}
