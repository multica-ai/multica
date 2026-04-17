package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIteratePages_FollowsLinkNext(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/v4/items?page=2>; rel="next"`, srvURLFromRequest(r)))
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1}, {"id": 2}})
		case "2":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 3}})
		default:
			t.Fatalf("unexpected page %q", page)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	var collected []map[string]any
	err := iteratePages(context.Background(), c, "tok", "/items", func(items []map[string]any) error {
		collected = append(collected, items...)
		return nil
	})
	if err != nil {
		t.Fatalf("iteratePages: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 server calls, got %d", calls)
	}
	if len(collected) != 3 {
		t.Errorf("expected 3 items, got %d (%+v)", len(collected), collected)
	}
}

func srvURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
