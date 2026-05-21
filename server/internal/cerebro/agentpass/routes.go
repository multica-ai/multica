package agentpass

import (
	"github.com/go-chi/chi/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// NewAdminRoutes constructs the agent-pass admin API inside the cerebro
// package so upstream router.go only needs a small mount hook.
func NewAdminRoutes(cerebro *cerebrodb.Queries) chi.Router {
	svc, err := New(cerebro, nil)
	if err != nil {
		panic("agent-pass admin service construction failed: " + err.Error())
	}

	h := NewHandler(cerebro, svc)
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Delete("/{passId}", h.Revoke)
	return r
}
