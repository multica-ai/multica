package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListNotes_PerIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/7/issues/42/notes" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Note{
			{ID: 1, Body: "hello", System: false, Author: User{ID: 100, Username: "alice"}, UpdatedAt: "2026-04-17T10:00:00Z"},
			{ID: 2, Body: "added status::todo", System: true, Author: User{ID: 200, Username: "bot"}, UpdatedAt: "2026-04-17T10:01:00Z"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	notes, err := c.ListNotes(context.Background(), "tok", 7, 42)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 2 || !notes[1].System {
		t.Errorf("unexpected: %+v", notes)
	}
}
