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

// CEREBRO-PATCH(analytics-query-api): FIR-2996 canonical analytics routes.
func TestAnalyticsQueryRoutesAreMounted(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, nil)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/analytics/catalog"},
		{"POST", "/api/analytics/query"},
		{"GET", "/api/analytics/visuals"},
		{"POST", "/api/analytics/visuals"},
		{"PUT", "/api/analytics/visuals/11111111-1111-1111-1111-111111111111"},
		{"DELETE", "/api/analytics/visuals/11111111-1111-1111-1111-111111111111"},
		{"POST", "/api/analytics/backfill"},
	} {
		rctx := chi.NewRouteContext()
		if !router.Match(rctx, route.method, route.path) {
			t.Fatalf("expected %s %s to match a mounted route", route.method, route.path)
		}
	}
}

// CEREBRO-PATCH(cerebro-account-routes): FIR-3118 — the per-account routes
// sit inside the workspace group whose RequireWorkspaceMemberFromURL reads
// URLParam("id"). chi returns the innermost value for duplicate param names,
// so naming the account segment {id} handed the ACCOUNT id to the membership
// check and every per-account route 404'd with "workspace not found". Guard
// that the workspace {id} and account {accountID} params resolve separately.
func TestAccountDetailRoutesResolveWorkspaceAndAccountParams(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, nil)

	const ws = "11111111-1111-1111-1111-111111111111"
	const acc = "22222222-2222-2222-2222-222222222222"
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/workspaces/" + ws + "/accounts/" + acc},
		{"DELETE", "/api/workspaces/" + ws + "/accounts/" + acc},
		{"PATCH", "/api/workspaces/" + ws + "/accounts/" + acc + "/controls"},
		{"GET", "/api/workspaces/" + ws + "/accounts/" + acc + "/usage-history"},
	} {
		rctx := chi.NewRouteContext()
		if !router.Match(rctx, route.method, route.path) {
			t.Fatalf("expected %s %s to match a mounted route", route.method, route.path)
		}
		if got := rctx.URLParam("id"); got != ws {
			t.Errorf("%s %s: URLParam(\"id\") = %q, want workspace id %q — account param is shadowing the workspace param", route.method, route.path, got, ws)
		}
		if got := rctx.URLParam("accountID"); got != acc {
			t.Errorf("%s %s: URLParam(\"accountID\") = %q, want account id %q", route.method, route.path, got, acc)
		}
	}
}
