package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// gitlabConnectionLookup is the narrow surface of db.Queries that this
// middleware needs. *db.Queries satisfies it; tests stub with a fake.
type gitlabConnectionLookup interface {
	GetWorkspaceGitlabConnection(ctx context.Context, workspaceID pgtype.UUID) (db.WorkspaceGitlabConnection, error)
}

// GitlabWritesBlocked returns a chi-compatible middleware that responds 501
// to any non-GET/HEAD/OPTIONS request when the workspace (resolved from the
// URL param "id" or the X-Workspace-ID header) has a workspace_gitlab_connection
// row. Reads always pass through.
func GitlabWritesBlocked(q gitlabConnectionLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			workspaceID := workspaceIDFromRequest(r)
			if workspaceID == "" {
				next.ServeHTTP(w, r)
				return
			}
			var u pgtype.UUID
			if err := u.Scan(workspaceID); err != nil {
				next.ServeHTTP(w, r)
				return
			}
			_, err := q.GetWorkspaceGitlabConnection(r.Context(), u)
			if errors.Is(err, pgx.ErrNoRows) {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				// Lookup error — defer to downstream which has its own
				// error handling.
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "writes not yet wired to GitLab — Phase 3 will enable this",
			})
		})
	}
}

// workspaceIDFromRequest pulls the workspace ID from the chi URL param "id"
// or the X-Workspace-ID header. Mirrors the existing handler-package helper
// pattern (workspaces_id is the URL convention; X-Workspace-ID is the header
// fallback used by routes that don't take a path param).
func workspaceIDFromRequest(r *http.Request) string {
	if id := chi.URLParam(r, "id"); id != "" {
		return id
	}
	return r.Header.Get("X-Workspace-ID")
}
