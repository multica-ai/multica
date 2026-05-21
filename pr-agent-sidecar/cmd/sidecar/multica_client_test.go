package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMulticaClient_CreateIssue_HappyPath(t *testing.T) {
	var capturedMethod, capturedPath, capturedAuth, capturedWS, capturedCT string
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedWS = r.Header.Get("X-Workspace-ID")
		capturedCT = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(IssueResponse{ID: "u-1", Identifier: "INV-512", Title: "x"})
	}))
	defer srv.Close()

	c := NewMulticaClient(srv.URL, "mul_test", "ws-uuid")

	got, err := c.CreateIssue(context.Background(), CreateIssueRequest{
		Title:        "Review PR #1",
		Description:  "body",
		AssigneeType: "agent",
		AssigneeID:   "agent-uuid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "u-1" || got.Identifier != "INV-512" {
		t.Fatalf("response = %+v", got)
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/issues" {
		t.Errorf("path = %q, want /api/issues", capturedPath)
	}
	if capturedAuth != "Bearer mul_test" {
		t.Errorf("auth header = %q", capturedAuth)
	}
	if capturedWS != "ws-uuid" {
		t.Errorf("X-Workspace-ID = %q", capturedWS)
	}
	if !strings.HasPrefix(capturedCT, "application/json") {
		t.Errorf("Content-Type = %q", capturedCT)
	}

	var sent CreateIssueRequest
	if err := json.Unmarshal(capturedBody, &sent); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	want := CreateIssueRequest{
		Title:        "Review PR #1",
		Description:  "body",
		AssigneeType: "agent",
		AssigneeID:   "agent-uuid",
	}
	if sent != want {
		t.Fatalf("body = %+v, want %+v", sent, want)
	}
}

func TestMulticaClient_CreateIssue_Non201IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"assignee_type and assignee_id must be provided together"}`)
	}))
	defer srv.Close()

	c := NewMulticaClient(srv.URL, "mul_test", "ws-uuid")
	_, err := c.CreateIssue(context.Background(), CreateIssueRequest{Title: "x"})
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should include status code, got: %v", err)
	}
}

func TestMulticaClient_CreateIssue_TrimsTrailingSlash(t *testing.T) {
	// LoadConfig trims the trailing slash; sanity-check the client tolerates
	// a clean URL too. (Negative test for double-slash regressions.)
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"x","identifier":"INV-1"}`)
	}))
	defer srv.Close()

	c := NewMulticaClient(srv.URL, "mul_test", "ws-uuid")
	if _, err := c.CreateIssue(context.Background(), CreateIssueRequest{Title: "x", AssigneeType: "agent", AssigneeID: "a"}); err != nil {
		t.Fatal(err)
	}
	if capturedPath != "/api/issues" {
		t.Errorf("path = %q (double-slash regression?)", capturedPath)
	}
}
