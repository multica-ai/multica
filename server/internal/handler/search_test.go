package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearch(t *testing.T) {
	ctx := context.Background()

	// Create an issue with a unique title for search.
	uniqueTitle := "Xylophonic search test issue"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    uniqueTitle,
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	issueID := created.ID
	issueNumber := created.Number

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	t.Run("search by title keyword", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/search?q=Xylophonic", nil)
		testHandler.Search(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Search: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp SearchResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp.Issues) == 0 {
			t.Fatal("Search: expected at least 1 result for keyword search")
		}

		found := false
		for _, issue := range resp.Issues {
			if issue.ID == issueID {
				found = true
				if issue.Title != uniqueTitle {
					t.Fatalf("Search: expected title %q, got %q", uniqueTitle, issue.Title)
				}
				if issue.Status != "todo" {
					t.Fatalf("Search: expected status 'todo', got %q", issue.Status)
				}
				if issue.Priority != "medium" {
					t.Fatalf("Search: expected priority 'medium', got %q", issue.Priority)
				}
				if issue.Identifier == "" {
					t.Fatal("Search: expected non-empty identifier")
				}
				break
			}
		}
		if !found {
			t.Fatalf("Search: created issue %s not found in results", issueID)
		}
	})

	t.Run("search by issue number", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("GET", fmt.Sprintf("/api/search?q=%d", issueNumber), nil)
		testHandler.Search(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Search: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp SearchResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp.Issues) == 0 {
			t.Fatal("Search: expected at least 1 result for issue number search")
		}

		found := false
		for _, issue := range resp.Issues {
			if issue.ID == issueID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Search: created issue %s not found in number search results", issueID)
		}
	})

	t.Run("empty query returns empty results", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/search?q=", nil)
		testHandler.Search(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Search: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp SearchResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp.Issues) != 0 {
			t.Fatalf("Search: expected 0 results for empty query, got %d", len(resp.Issues))
		}
	})

	t.Run("non-matching query returns empty results", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/search?q=zzzznonexistentqueryzzzz", nil)
		testHandler.Search(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Search: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp SearchResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp.Issues) != 0 {
			t.Fatalf("Search: expected 0 results for non-matching query, got %d", len(resp.Issues))
		}
	})
}
