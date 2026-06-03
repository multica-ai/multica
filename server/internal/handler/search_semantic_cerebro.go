// CEREBRO-PATCH(search-semantic-fir-2604): FIR-2604 search trin 2 — wire the
// semantic Provider + nearest-neighbour lookup into SearchIssuesCerebro.
//
// Net-new fork file. Defines the SemanticSearchInvoker seam that
// handler.Handler.SemanticSearch references, and provides the concrete
// implementation backed by the semantic package. The seam is so the handler
// package stays free of a direct dependency on the model-talking code, which
// keeps the handler test surface narrow and lets self-host installs without a
// provider skip the worker entirely without breaking imports.
package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrosearch "github.com/multica-ai/multica/server/internal/cerebro/search"
	"github.com/multica-ai/multica/server/internal/cerebro/semantic"
)

// SemanticSearchInvoker is the read path the search handler uses to ask the
// semantic subsystem for a query embedding + vector literal. Concrete
// implementations live in this package's _cerebro.go files; a nil invoker
// keeps SearchIssuesCerebro on the Del-1 keyword-only fast path.
type SemanticSearchInvoker interface {
	// Enabled reports whether the operator wired a provider AND the pgvector
	// store is reachable. False on either count drops the handler back to
	// pure keyword search — no error, no log noise.
	Enabled(ctx context.Context) bool

	// QueryVectorLiteral embeds the query text and returns the pgvector
	// textual literal ready to substitute into a SQL parameter. Returns
	// ("", false) when the provider returned an error or the context timed
	// out; the handler treats either as "skip semantic this request" rather
	// than as a 500.
	QueryVectorLiteral(ctx context.Context, text string) (literal string, ok bool)
}

// SemanticSearch is the production SemanticSearchInvoker. main.go constructs
// it from the env-driven semantic.Config and assigns it to
// Handler.SemanticSearch alongside the worker that drains the queue.
type SemanticSearch struct {
	pool     *pgxpool.Pool
	provider semantic.Provider
	timeout  func() context.Context // optional override for tests
	cfg      semantic.Config
}

// NewSemanticSearch wires a production invoker. provider may be the disabled
// sentinel; Enabled() then always returns false.
func NewSemanticSearch(pool *pgxpool.Pool, provider semantic.Provider, cfg semantic.Config) *SemanticSearch {
	return &SemanticSearch{pool: pool, provider: provider, cfg: cfg}
}

// Enabled reports the provider is non-disabled AND the embedding store exists.
// A missing store can happen on self-host installs that boot before pgvector
// is installed; we check on every request so the handler picks up the store
// the moment it becomes available without a restart.
func (s *SemanticSearch) Enabled(ctx context.Context) bool {
	if s == nil || s.provider == nil || semantic.IsDisabled(s.provider) {
		return false
	}
	return semantic.StoreAvailable(ctx, s.pool)
}

// QueryVectorLiteral embeds with a per-request budget so a slow provider can
// never block an HTTP response by more than the configured timeout.
func (s *SemanticSearch) QueryVectorLiteral(ctx context.Context, text string) (string, bool) {
	if !s.Enabled(ctx) {
		return "", false
	}
	timeout := s.cfg.QueryTimeout
	if timeout <= 0 {
		timeout = 2 * 1e9 // 2s; rare path, kept inline so we don't import time at file scope.
	}
	embedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	vec, err := s.provider.Embed(embedCtx, text)
	if err != nil || len(vec) == 0 {
		return "", false
	}
	return semantic.FormatVectorLiteral(vec), true
}

// nearestNeighbourScoped is the wrapper the handler uses to look up the
// top-K vector matches via the pool. Returned for tests; production code
// inlines it through QueryVectorLiteral + cerebrosearch.BuildHybrid.
func (s *SemanticSearch) NearestNeighbours(ctx context.Context, workspace pgtype.UUID, vec []float32, k int) ([]semantic.VectorHit, error) {
	return semantic.NearestNeighbours(ctx, s.pool, workspace, vec, k)
}

// Ensure HybridInput.QueryVectorLit can use the same literal the handler
// already obtained. Keeping the function next to the seam means the
// alternative path (a test fake) does not have to import semantic.
func hybridInputFor(in cerebrosearch.QueryInput, literal string) cerebrosearch.HybridInput {
	return cerebrosearch.HybridInput{
		Query:          in,
		QueryVectorLit: literal,
	}
}
