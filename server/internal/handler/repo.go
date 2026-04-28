package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Scope types for repo_binding.scope_type. The schema CHECK already restricts
// values, but exposing constants here keeps callers from hand-typing a string
// every time and makes it cheap to add `project` / `issue` in Step 2 / 3.
const (
	repoScopeWorkspace = "workspace"
)

// loadRepoDataByScope returns the repos bound to (scope_type, scope_id), in
// the normalized shape that the daemon/agent code expects (URL-trimmed,
// deduped, empty URLs dropped). Description comes from the binding row, so
// two scopes that share the same git URL can each carry their own
// description without one overwriting the other.
// Returns an empty slice — never nil — when no bindings exist.
func (h *Handler) loadRepoDataByScope(ctx context.Context, scopeType string, scopeID pgtype.UUID) ([]RepoData, error) {
	rows, err := h.Queries.ListReposByScope(ctx, db.ListReposByScopeParams{
		ScopeType: scopeType,
		ScopeID:   scopeID,
	})
	if err != nil {
		return nil, err
	}
	repos := make([]RepoData, 0, len(rows))
	for _, r := range rows {
		repos = append(repos, RepoData{URL: r.Url, Description: r.Description})
	}
	return normalizeWorkspaceRepos(repos), nil
}

// loadWorkspaceRepoData is a thin convenience wrapper around the
// "workspace"-scoped lookup. Most call sites only need the workspace level
// today, so spelling it out here keeps them readable.
func (h *Handler) loadWorkspaceRepoData(ctx context.Context, workspaceID pgtype.UUID) ([]RepoData, error) {
	return h.loadRepoDataByScope(ctx, repoScopeWorkspace, workspaceID)
}

// setRepoBindingsForScope replaces the entire set of bindings attached to
// (scope_type, scope_id) with `repos`. The repo catalog itself is upsert-by-URL
// so two scopes sharing the same git URL share a single repo row, but the
// description is stored per-binding so workspace A's "Backend API" doesn't
// clobber workspace B's "Shared SDK" for the same URL. Orphan repo rows that
// no scope references anymore are pruned at the end of the transaction.
func (h *Handler) setRepoBindingsForScope(ctx context.Context, scopeType string, scopeID pgtype.UUID, repos []RepoData) error {
	repos = normalizeWorkspaceRepos(repos)

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
		ScopeType: scopeType,
		ScopeID:   scopeID,
	}); err != nil {
		return err
	}
	for _, r := range repos {
		repo, err := qtx.UpsertRepoByURL(ctx, r.URL)
		if err != nil {
			return err
		}
		if _, err := qtx.CreateRepoBinding(ctx, db.CreateRepoBindingParams{
			RepoID:      repo.ID,
			ScopeType:   scopeType,
			ScopeID:     scopeID,
			Description: r.Description,
		}); err != nil {
			return err
		}
	}
	if err := qtx.DeleteOrphanRepos(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
