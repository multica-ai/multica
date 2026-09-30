package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTaskResponseFinalizerPostsAuthenticatedStructuredRequest(t *testing.T) {
	var gotAuth string
	var gotInput TaskResponseFinalizationInput
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/response/finalize" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type=%q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotInput); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(TaskResponseFinalizationResult{Rendered: "[Goal]\nrendered"})
	}))
	defer server.Close()

	client, err := NewHTTPTaskResponseFinalizer(HTTPTaskResponseFinalizerConfig{
		BaseURL: server.URL + "/",
		Token:   "secret-token",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPTaskResponseFinalizer: %v", err)
	}
	input := TaskResponseFinalizationInput{
		SchemaVersion: "1",
		ResponseID:    "multica:t1:terminal:r7",
		TaskID:        "t1",
		WorkspaceID:   "w1",
		Attempt:       2,
		TaskStatus:    "running",
		RawOutput:     "raw",
		Issue: &ResponseIssueSnapshot{
			ID: "i1", Identifier: "MUL-7", Title: "Issue", Status: "in_progress", Revision: 7,
		},
	}
	got, err := client.FinalizeTaskCompletion(context.Background(), input)
	if err != nil {
		t.Fatalf("FinalizeTaskCompletion: %v", err)
	}
	if got.Rendered != "[Goal]\nrendered" {
		t.Fatalf("rendered=%q", got.Rendered)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("authorization=%q", gotAuth)
	}
	if gotInput.ResponseID != input.ResponseID || gotInput.RawOutput != "raw" {
		t.Fatalf("input=%+v", gotInput)
	}
}

func TestHTTPTaskResponseFinalizerRejectsMissingURLOrToken(t *testing.T) {
	for _, tc := range []HTTPTaskResponseFinalizerConfig{
		{Token: "x", Timeout: time.Second},
		{BaseURL: "http://127.0.0.1:1234", Timeout: time.Second},
	} {
		if _, err := NewHTTPTaskResponseFinalizer(tc); err == nil {
			t.Fatalf("config=%+v expected error", tc)
		}
	}
}

func TestHTTPTaskResponseFinalizerRejectsNonHTTPURL(t *testing.T) {
	_, err := NewHTTPTaskResponseFinalizer(HTTPTaskResponseFinalizerConfig{
		BaseURL: "file:///tmp/socket",
		Token:   "x",
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected non-http URL error")
	}
}

func TestHTTPTaskResponseFinalizerTreatsNon2xxAsFailureWithoutReturningBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "SENSITIVE-UPSTREAM-BODY", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewHTTPTaskResponseFinalizer(HTTPTaskResponseFinalizerConfig{
		BaseURL: server.URL,
		Token:   "secret-token",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.FinalizeTaskCompletion(context.Background(), TaskResponseFinalizationInput{ResponseID: "r1"})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if strings.Contains(err.Error(), "SENSITIVE-UPSTREAM-BODY") {
		t.Fatalf("upstream body leaked into error: %v", err)
	}
}

func TestHTTPTaskResponseFinalizerRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 70<<10)))
	}))
	defer server.Close()
	client, err := NewHTTPTaskResponseFinalizer(HTTPTaskResponseFinalizerConfig{
		BaseURL: server.URL,
		Token:   "secret-token",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.FinalizeTaskCompletion(context.Background(), TaskResponseFinalizationInput{ResponseID: "r1"}); err == nil {
		t.Fatal("expected oversized response error")
	}
}
