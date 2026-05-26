// CEREBRO-PATCH(agent-avatar-backfill): FIR-2236 guard static agent GET route ordering.
package main

import (
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestAgentBackfillStatusRouteIsNotShadowedByAgentID(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, nil)

	rctx := chi.NewRouteContext()
	if !router.Match(rctx, "GET", "/api/agents/backfill-avatars") {
		t.Fatal("expected GET /api/agents/backfill-avatars to match")
	}
	if got := rctx.RoutePattern(); got != "/api/agents/backfill-avatars" {
		t.Fatalf("GET /api/agents/backfill-avatars matched %q, want static backfill route", got)
	}
	if id := rctx.URLParam("id"); id != "" {
		t.Fatalf("GET /api/agents/backfill-avatars was routed as agent id %q", id)
	}
}
