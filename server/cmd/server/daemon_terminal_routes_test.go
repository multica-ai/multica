package main

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

// TestFencedTerminalRoutesAreRegistered pins the versioned terminal surface in
// the router table. The daemon's no-fallback routing depends on these routes
// existing on a current server — and, just as much, on a replica that predates
// the fence NOT having them, which is why an unmatched route is retried instead
// of being downgraded to the legacy endpoints.
func TestFencedTerminalRoutesAreRegistered(t *testing.T) {
	if testPool == nil {
		t.Skip("server test harness unavailable")
	}

	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	registerListeners(bus, hub)
	router := NewRouter(testPool, hub, bus, analytics.NoopClient{}, nil)

	registered := map[string]bool{}
	if err := chi.Walk(router, func(_, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[pattern] = true
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	for _, want := range []string{
		"/api/daemon/v2/tasks/{taskId}/complete",
		"/api/daemon/v2/tasks/{taskId}/fail",
		"/api/daemon/tasks/{taskId}/complete",
		"/api/daemon/tasks/{taskId}/fail",
	} {
		if !registered[want] {
			t.Fatalf("route %s is not registered", want)
		}
	}
}
