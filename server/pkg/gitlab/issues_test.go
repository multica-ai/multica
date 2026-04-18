package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateIssue_PostsCorrectBody(t *testing.T) {
	var got CreateIssueInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/issues" {
			t.Errorf("path = %s, want /api/v4/projects/7/issues", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Issue{
			ID: 9001, IID: 11, Title: got.Title, State: "opened",
			Labels: got.Labels, UpdatedAt: "2026-04-17T15:00:00Z",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	out, err := c.CreateIssue(context.Background(), "tok", 7, CreateIssueInput{
		Title:       "hi",
		Description: "body",
		Labels:      []string{"status::todo", "priority::high"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got.Title != "hi" || got.Description != "body" {
		t.Errorf("server received %+v", got)
	}
	if len(got.Labels) != 2 {
		t.Errorf("labels = %v", got.Labels)
	}
	if out.IID != 11 {
		t.Errorf("returned IID = %d, want 11", out.IID)
	}
}

func TestListIssues_DefaultsToStateAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "all" {
			t.Errorf("state query = %q, want all", r.URL.Query().Get("state"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Issue{
			{IID: 1, Title: "one", State: "opened", Labels: []string{"status::todo"}},
			{IID: 2, Title: "two", State: "closed", Labels: []string{"status::done"}},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	issues, err := c.ListIssues(context.Background(), "tok", 7, ListIssuesParams{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 || issues[0].IID != 1 {
		t.Errorf("unexpected: %+v", issues)
	}
}

func TestUpdateIssue_SendsPUTWithFields(t *testing.T) {
	var capturedPath, capturedMethod, capturedToken string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":901,"iid":42,"title":"updated","state":"opened","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	newTitle := "updated"
	stateEvent := "close"
	input := UpdateIssueInput{
		Title:        &newTitle,
		AddLabels:    []string{"status::done"},
		RemoveLabels: []string{"status::in_progress"},
		StateEvent:   &stateEvent,
	}
	issue, err := c.UpdateIssue(context.Background(), "tok", 7, 42, input)
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/7/issues/42" {
		t.Errorf("path = %s, want /api/v4/projects/7/issues/42", capturedPath)
	}
	if capturedToken != "tok" {
		t.Errorf("token header = %s, want tok", capturedToken)
	}
	if capturedBody["title"] != "updated" {
		t.Errorf("body title = %v, want updated", capturedBody["title"])
	}
	if capturedBody["add_labels"] != "status::done" {
		t.Errorf("body add_labels = %v, want status::done", capturedBody["add_labels"])
	}
	if capturedBody["remove_labels"] != "status::in_progress" {
		t.Errorf("body remove_labels = %v, want status::in_progress", capturedBody["remove_labels"])
	}
	if capturedBody["state_event"] != "close" {
		t.Errorf("body state_event = %v, want close", capturedBody["state_event"])
	}
	if issue.IID != 42 || issue.Title != "updated" {
		t.Errorf("issue = %+v, want IID=42 title=updated", issue)
	}
}

func TestUpdateIssue_OmitsUnsetFields(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"iid":1,"state":"opened","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	newTitle := "only title"
	_, err := c.UpdateIssue(context.Background(), "tok", 1, 1, UpdateIssueInput{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if _, ok := capturedBody["description"]; ok {
		t.Errorf("description should be omitted, body = %+v", capturedBody)
	}
	if _, ok := capturedBody["add_labels"]; ok {
		t.Errorf("add_labels should be omitted when empty, body = %+v", capturedBody)
	}
	if _, ok := capturedBody["state_event"]; ok {
		t.Errorf("state_event should be omitted when nil, body = %+v", capturedBody)
	}
}

func TestUpdateIssue_PropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.UpdateIssue(context.Background(), "tok", 1, 1, UpdateIssueInput{})
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want to contain '403'", err)
	}
}

func TestDeleteIssue_SendsDELETE(t *testing.T) {
	var capturedPath, capturedMethod, capturedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteIssue(context.Background(), "tok", 7, 42); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/7/issues/42" {
		t.Errorf("path = %s, want /api/v4/projects/7/issues/42", capturedPath)
	}
	if capturedToken != "tok" {
		t.Errorf("token header = %s, want tok", capturedToken)
	}
}

func TestDeleteIssue_404IsSuccessIdempotent(t *testing.T) {
	// GitLab returns 404 if the issue is already gone. For a delete, that's
	// the desired terminal state, so we treat it as success (idempotent).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteIssue(context.Background(), "tok", 7, 42); err != nil {
		t.Fatalf("DeleteIssue: expected 404 to be treated as success, got %v", err)
	}
}

func TestDeleteIssue_PropagatesNon2xxNon404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.DeleteIssue(context.Background(), "tok", 7, 42)
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want to contain '403'", err)
	}
}

func TestListIssues_UpdatedAfterPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("updated_after") != "2026-04-17T00:00:00Z" {
			t.Errorf("updated_after = %q", r.URL.Query().Get("updated_after"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.ListIssues(context.Background(), "tok", 7, ListIssuesParams{
		UpdatedAfter: "2026-04-17T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
}
