// CEREBRO-PATCH(cerebro-notifications-routes): FIR-2394 route-registration
// guard. The frontend has called /api/inbox/notifications* for a long time
// but the server side was missing — every call returned 404 in prod. This
// test fails closed if any of the four routes goes missing again.
package main

import (
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestInboxNotificationsRoutesRegistered(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, nil)

	want := []string{
		"GET /api/inbox/notifications",
		"GET /api/inbox/notifications/unread-count",
		"POST /api/inbox/notifications/archive-all",
		"POST /api/inbox/notifications/mark-all-read",
	}

	have := map[string]bool{}
	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		have[fmt.Sprintf("%s %s", method, route)] = true
		return nil
	}
	if err := chi.Walk(router, walk); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}

	var missing []string
	for _, id := range want {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("missing notification routes (regression of FIR-2394):\n  %v", missing)
	}
}
