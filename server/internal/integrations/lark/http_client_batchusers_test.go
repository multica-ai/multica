package lark

import (
	"context"
	"net/http"
	"sort"
	"testing"
	"time"
)

// TestHTTPClient_BatchGetUsers exercises the contact name-resolution
// path: the request carries user_id_type=open_id and a user_ids list, and
// the response items[] are folded into an open_id -> name map (entries
// without a name are dropped).
func TestHTTPClient_BatchGetUsers(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc("/open-apis/contact/v3/users/batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("want GET, got %s", r.Method)
		}
		q := r.URL.Query()
		if q.Get("user_id_type") != "open_id" {
			t.Errorf("user_id_type = %q", q.Get("user_id_type"))
		}
		ids := q["user_ids"]
		sort.Strings(ids)
		if len(ids) != 3 || ids[0] != "ou_a" || ids[1] != "ou_b" || ids[2] != "ou_c" {
			t.Errorf("user_ids = %v", q["user_ids"])
		}
		writeJSON(w, map[string]any{
			"code": 0, "msg": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{"open_id": "ou_a", "name": "Alice"},
					map[string]any{"open_id": "ou_b", "name": "Bob"},
					map[string]any{"open_id": "ou_c"}, // no name -> dropped
				},
			},
		})
	})

	c := newTestClient(fake, time.Now)
	names, err := c.BatchGetUsers(context.Background(), testCreds(), []string{"ou_a", "ou_b", "ou_c"})
	if err != nil {
		t.Fatalf("BatchGetUsers: %v", err)
	}
	if len(names) != 2 || names["ou_a"] != "Alice" || names["ou_b"] != "Bob" {
		t.Errorf("names = %v", names)
	}
	if _, ok := names["ou_c"]; ok {
		t.Errorf("ou_c should be dropped (no name): %v", names)
	}
}

// TestHTTPClient_BatchGetUsersEmpty returns an empty map and makes no HTTP
// call when given no ids.
func TestHTTPClient_BatchGetUsersEmpty(t *testing.T) {
	fake := newLarkFake(t)
	// No token stub and no handler: any HTTP call would panic the fake.
	c := newTestClient(fake, time.Now)
	names, err := c.BatchGetUsers(context.Background(), testCreds(), nil)
	if err != nil {
		t.Fatalf("BatchGetUsers(empty): %v", err)
	}
	if len(names) != 0 {
		t.Errorf("names = %v, want empty", names)
	}
}
