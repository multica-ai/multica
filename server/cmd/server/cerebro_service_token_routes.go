// CEREBRO (FIR-3608): wiring for scoped, non-personal service tokens ("msv_").
// Net-new cerebro file (cerebro_ prefix carve-out) so router.go carries only
// two one-line marked calls. Two surfaces are mounted here:
//
//   - /api/service-tokens — human owner/admin CRUD over the credentials.
//   - /api/service/...     — the scoped, read-first data API a service token
//     itself calls, gated per-route by RequireScope. A service token can reach
//     ONLY this prefix (enforced in the auth middleware), so the management
//     surface (/api/service-tokens, which does NOT match /api/service/) stays
//     human-only.
package main

import (
	"github.com/go-chi/chi/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/servicetoken"
	servicetokenapi "github.com/multica-ai/multica/server/internal/cerebro/servicetoken/api"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// wireCerebroServiceToken builds the token service, registers it as the
// authenticator the auth middleware calls for "msv_" tokens, and returns the
// HTTP handler for both surfaces.
func wireCerebroServiceToken(cerebroQueries *cerebrodb.Queries, queries *db.Queries, issues *service.IssueService) *servicetokenapi.Handler {
	svc := servicetoken.NewTokenService(servicetoken.NewStore(cerebroQueries))
	servicetoken.SetAuthenticator(svc)
	return servicetokenapi.NewHandler(svc, queries)
}

// mountCerebroServiceTokenRoutes registers both the management and machine
// surfaces. Mounted inside the authenticated API group so middleware.Auth has
// resolved the caller before these run.
func mountCerebroServiceTokenRoutes(r chi.Router, h *servicetokenapi.Handler, queries *db.Queries) {
	// Management surface — owner/admin only, authenticated as a human member.
	r.Route("/api/service-tokens", func(r chi.Router) {
		r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
		r.Post("/", h.CreateToken)
		r.Get("/", h.ListTokens)
		r.Delete("/{id}", h.RevokeToken)
	})

	// Machine surface — service-token only, no member context; each route is
	// gated solely by its required scope. RequireScope also 403s any caller
	// that is not a service token (e.g. a member PAT that reached this path).
	r.Route("/api/service", func(r chi.Router) {
		r.With(servicetoken.RequireScope(servicetoken.ScopeSkillsRead)).Get("/skills", h.ListSkills)
		r.With(servicetoken.RequireScope(servicetoken.ScopeAgentsRead)).Get("/agents", h.ListAgents)
		r.With(servicetoken.RequireScope(servicetoken.ScopeIssuesRead)).Get("/issues", h.ListIssues)
	})
}
