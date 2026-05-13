// CEREBRO-PATCH(permguard-http-test): JEH-1171 regression guard. Walks the
// live Chi router, filters to mutating HTTP routes, and diffs the set
// against server/internal/cerebro/permguard/inventory.json. Failure modes
// (missing entry, stale entry, unclassified, ambiguous) are documented in
// the permguard README.
package main

import (
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/cerebro/permguard"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestPermissionGuard_HTTPRoutesInventoried(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, nil)

	var ids []string
	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !permguard.IsMutating(method) {
			return nil
		}
		ids = append(ids, fmt.Sprintf("%s %s", method, route))
		return nil
	}
	if err := chi.Walk(router, walk); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	sort.Strings(ids)

	inv, err := permguard.Load()
	if err != nil {
		t.Fatalf("permguard.Load: %v", err)
	}
	res := inv.Diff(permguard.SurfaceHTTP, ids)
	if res.Clean() {
		return
	}
	path, _ := permguard.InventoryPath()
	t.Fatalf("\n%s\nInventory: %s", res.Report(permguard.SurfaceHTTP), path)
}
