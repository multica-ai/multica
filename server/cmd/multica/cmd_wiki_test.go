package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestWikiList(t *testing.T) {
	pages := []map[string]any{
		{"id": "aaa", "title": "Getting Started", "slug": "getting-started", "position": 0.0, "parent_id": nil, "updated_at": "2026-01-01T00:00:00Z"},
		{"id": "bbb", "title": "Architecture", "slug": "architecture", "position": 1.0, "parent_id": "aaa", "updated_at": "2026-01-02T00:00:00Z"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wiki-pages" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"pages": pages, "total": 2})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	var result map[string]any
	err := client.GetJSON(ctx, "/api/wiki-pages?workspace_id=ws-1", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pagesRaw, ok := result["pages"].([]any)
	if !ok || len(pagesRaw) != 2 {
		t.Fatalf("expected 2 pages, got %v", result)
	}
}

func TestWikiCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wiki-pages" && r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["title"] == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "title is required"})
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"id":    "ccc",
				"title": body["title"],
				"slug":  "test-page",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	body := map[string]any{"title": "Test Page", "content": "Hello world"}
	var result map[string]any
	err := client.PostJSON(ctx, "/api/wiki-pages?workspace_id=ws-1", body, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["id"] != "ccc" {
		t.Errorf("expected id=ccc, got %v", result["id"])
	}
}

func TestWikiGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wiki-pages/page-123" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "page-123",
				"title":   "My Page",
				"slug":    "my-page",
				"content": "# Hello\nWorld",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	var result map[string]any
	err := client.GetJSON(ctx, "/api/wiki-pages/page-123?workspace_id=ws-1", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["title"] != "My Page" {
		t.Errorf("expected title=My Page, got %v", result["title"])
	}
	if result["content"] != "# Hello\nWorld" {
		t.Errorf("expected content preserved, got %v", result["content"])
	}
}

func TestWikiUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wiki-pages/page-123" && r.Method == http.MethodPatch {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{
				"id":    "page-123",
				"title": body["title"],
				"slug":  "updated-page",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	body := map[string]any{"title": "Updated Title"}
	var result map[string]any
	err := client.PatchJSON(ctx, "/api/wiki-pages/page-123?workspace_id=ws-1", body, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["title"] != "Updated Title" {
		t.Errorf("expected title=Updated Title, got %v", result["title"])
	}
}

func TestWikiDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wiki-pages/page-123" && r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	err := client.DeleteJSON(ctx, "/api/wiki-pages/page-123?workspace_id=ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
