package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Exercise routing and the JSON contract; group/filter semantics live in the
// handler suite, where fixtures isolate every inbox from other test data.
func TestInboxPageAndFacetsThroughRouter(t *testing.T) {
	resp := authRequest(t, "GET", "/api/inbox/page?group_id="+uuid.NewString(), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("inbox page status = %d", resp.StatusCode)
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

	// A seeded notification resolves through the page route, which must not be
	// taken for an item id, and stays in the legacy array.
	var itemID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title)
		VALUES ($1, 'member', $2, 'issue_assigned', 'Page fixture')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&itemID); err != nil {
		t.Fatalf("failed to seed inbox item: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE id = $1`, itemID)
	})
	resp = authRequest(t, "GET", "/api/inbox/page?group_id="+itemID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("inbox page lookup status = %d", resp.StatusCode)
	}
	readJSON(t, resp, &page)
	if len(page.Items) != 1 || page.Items[0].ID != itemID || page.Items[0].Archived {
		t.Fatalf("lookup did not resolve the seeded notification: %+v", page)
	}
	resp = authRequest(t, "GET", "/api/inbox", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("legacy inbox status = %d", resp.StatusCode)
	}
	var legacy []inboxItemJSON
	readJSON(t, resp, &legacy)
	found := false
	for _, item := range legacy {
		found = found || item.ID == itemID
	}
	if !found {
		t.Fatal("legacy inbox array lost the seeded notification")
	}

	resp = authRequest(t, "GET", "/api/inbox/facets", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("inbox facets status = %d", resp.StatusCode)
	}
	var facets struct {
		Statuses    map[string]int64 `json:"statuses"`
		Priorities  map[string]int64 `json:"priorities"`
		Actors      map[string]int64 `json:"actors"`
		UnreadCount int64            `json:"unread_count"`
	}
	readJSON(t, resp, &facets)
	if facets.Statuses == nil || facets.Priorities == nil || facets.Actors == nil || facets.UnreadCount < 1 {
		t.Fatalf("facets contract: %+v", facets)
	}
}
