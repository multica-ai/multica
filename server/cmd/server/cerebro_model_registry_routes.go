// CEREBRO (FIR-2698): wiring for the model registry — the single source for
// model prices, context windows, and display metadata, replacing four
// hand-maintained in-code tables. Net-new cerebro file (cerebro_ prefix
// carve-out) so router.go only carries two one-line marked calls.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebromodelregistry "github.com/multica-ai/multica/server/internal/cerebro/modelregistry"
	"github.com/multica-ai/multica/server/internal/metrics"
)

// modelRegistryRefreshInterval bounds how stale another replica's price table
// can be after an approval served elsewhere.
const modelRegistryRefreshInterval = 5 * time.Minute

// wireCerebroModelRegistry constructs the model-registry handler, loads the
// price/window table into the in-process store (which also feeds pkg/pricing
// and thereby the /api/cerebro/pricing contract), starts the periodic
// refresher, and installs the Prometheus metrics price lookup.
//
// A nil pool means the caller built the router for wiring inspection only
// (e.g. TestInboxNotificationsRoutesRegistered constructs NewRouter(nil, ...)
// to walk registered routes without a live database) — skip the eager DB
// load and refresher in that case instead of panicking on a nil *pgxpool.Pool.
func wireCerebroModelRegistry(cerebroQueries *cerebrodb.Queries, pool *pgxpool.Pool) *cerebromodelregistry.Handler {
	h := cerebromodelregistry.NewHandler(cerebromodelregistry.New(cerebroQueries, pool))

	if pool == nil {
		slog.Warn("model registry: pool is nil — skipping startup load and refresher (router built without a database)")
		return h
	}

	if err := cerebromodelregistry.LoadFromDB(context.Background(), cerebroQueries); err != nil {
		// Fail-expensive, not fail-closed: until the refresher succeeds,
		// ComputeCents uses the static worst-case rate and the metrics
		// recorder reports usage as unpriced.
		slog.Error("model registry: startup load failed — cost computation uses the fail-safe rate until a reload succeeds", "error", err)
	}
	cerebromodelregistry.StartRefresher(context.Background(), cerebroQueries, modelRegistryRefreshInterval)

	metrics.SetRegistryPriceLookup(func(model string) (metrics.ModelPrice, bool) {
		id, e, ok := cerebromodelregistry.Lookup(model)
		if !ok {
			return metrics.ModelPrice{}, false
		}
		return metrics.ModelPrice{
			Provider:       e.Provider,
			Model:          id,
			InputPerM:      e.InputUSDPerMtok,
			CacheReadPerM:  e.CacheReadUSDPerMtok,
			CacheWritePerM: e.CacheWriteUSDPerMtok,
			OutputPerM:     e.OutputUSDPerMtok,
		}, true
	})

	return h
}

// mountCerebroModelRegistryRoutes registers the governance REST surface.
// Mounted inside the authenticated, user-scoped API group so
// middleware.MemberFromContext resolves; reads are open to any workspace
// member, writes are gated by canManage in the handler.
func mountCerebroModelRegistryRoutes(r chi.Router, h *cerebromodelregistry.Handler) {
	r.Route("/api/model-registry", func(r chi.Router) {
		r.Get("/", h.Get)
		r.Put("/ownership", h.UpdateOwnership)
		r.Get("/versions", h.ListVersions)
		r.Get("/diff", h.Diff)
		r.Post("/rollback", h.Rollback)
		r.Get("/change-requests", h.ListChangeRequests)
		r.Post("/change-requests", h.CreateChangeRequest)
		r.Post("/change-requests/{crId}/review", h.ReviewChangeRequest)
	})
}
