package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentUser_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" {
			t.Errorf("path = %q, want /api/v4/user", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 42, "username": "alice", "name": "Alice A", "avatar_url": "https://x/avatar.png"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	u, err := c.CurrentUser(context.Background(), "tok")
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if u.ID != 42 || u.Username != "alice" || u.Name != "Alice A" {
		t.Fatalf("unexpected user: %+v", u)
	}
}
