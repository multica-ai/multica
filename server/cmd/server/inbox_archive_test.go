package main

import (
	"testing"

	"github.com/google/uuid"
)

// Exercise routing and the JSON contract; group/filter semantics live in the
// handler suite, where fixtures isolate every archive from other test data.
func TestArchivedInboxPageAndFacetsThroughRouter(t *testing.T) {
	resp := authRequest(t, "GET", "/api/inbox/archived/page?group_id="+uuid.NewString(), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("archive page status = %d", resp.StatusCode)
	}
	var page struct {
		Items      []inboxItemJSON `json:"items"`
		NextCursor *string         `json:"next_cursor"`
		HasMore    bool            `json:"has_more"`
	}
	readJSON(t, resp, &page)
	if page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil || page.HasMore {
		t.Fatalf("empty lookup contract: %+v", page)
	}
	resp = authRequest(t, "GET", "/api/inbox/archived/facets", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("archive facets status = %d", resp.StatusCode)
	}
	var facets struct {
		Statuses    map[string]int64 `json:"statuses"`
		Priorities  map[string]int64 `json:"priorities"`
		Actors      map[string]int64 `json:"actors"`
		UnreadCount int64            `json:"unread_count"`
	}
	readJSON(t, resp, &facets)
	if facets.Statuses == nil || facets.Priorities == nil || facets.Actors == nil || facets.UnreadCount < 0 {
		t.Fatalf("facets contract: %+v", facets)
	}
}
