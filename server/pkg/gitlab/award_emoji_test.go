package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAwardEmoji_PerIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/7/issues/42/award_emoji" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AwardEmoji{
			{ID: 1, Name: "thumbsup", User: User{ID: 100, Username: "alice"}, UpdatedAt: "2026-04-17T10:00:00Z"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	awards, err := c.ListAwardEmoji(context.Background(), "tok", 7, 42)
	if err != nil {
		t.Fatalf("ListAwardEmoji: %v", err)
	}
	if len(awards) != 1 || awards[0].Name != "thumbsup" {
		t.Errorf("unexpected: %+v", awards)
	}
}
