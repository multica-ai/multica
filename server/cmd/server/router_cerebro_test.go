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

// CEREBRO-PATCH(cerebro-cost-optimization-routes): FIR-2325 verify the
// per-workspace saving-mode routes are mounted at the expected method+path.
func TestCostOptimizationRoutesAreMounted(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, nil)

	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/api/workspaces/11111111-1111-1111-1111-111111111111/cost-optimization"},
		{"PUT", "/api/workspaces/11111111-1111-1111-1111-111111111111/cost-optimization/snapshot_prompt"},
		{"DELETE", "/api/workspaces/11111111-1111-1111-1111-111111111111/cost-optimization/snapshot_prompt"},
	}
	for _, c := range cases {
		rctx := chi.NewRouteContext()
		if !router.Match(rctx, c.method, c.path) {
			t.Fatalf("expected %s %s to match a mounted route", c.method, c.path)
		}
	}
}

// CEREBRO-PATCH(runtime-setup-routes): FIR-2672 setup-token mint and exchange
// routes must be mounted; otherwise the permission gate cannot be exercised.
func TestRuntimeSetupRoutesAreMounted(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, nil)

	cases := []struct {
		method string
		path   string
	}{
		{"POST", "/api/runtime-setup/tokens"},
		{"POST", "/api/workspaces/11111111-1111-1111-1111-111111111111/runtime-setup-token"},
		{"POST", "/api/runtime-setup/exchange"},
		{"GET", "/install-runtime.sh"},
	}
	for _, c := range cases {
		rctx := chi.NewRouteContext()
		if !router.Match(rctx, c.method, c.path) {
			t.Fatalf("expected %s %s to match a mounted route", c.method, c.path)
		}
	}
}
