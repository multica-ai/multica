package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProjectMembers_AllEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/7/members/all" {
			t.Errorf("path = %s, want /api/v4/projects/7/members/all", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ProjectMember{
			{ID: 10, Username: "alice", Name: "Alice A", AvatarURL: "https://x/alice.png"},
			{ID: 11, Username: "bob", Name: "Bob B", AvatarURL: "https://x/bob.png"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	members, err := c.ListProjectMembers(context.Background(), "tok", 7)
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 2 || members[0].Username != "alice" {
		t.Errorf("unexpected members: %+v", members)
	}
}
