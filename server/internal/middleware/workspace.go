package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Context keys for workspace-scoped request data.
type contextKey int

const (
	ctxKeyWorkspaceID contextKey = iota
	ctxKeyMember
)

// MemberFromContext returns the workspace member injected by the workspace middleware.
func MemberFromContext(ctx context.Context) (db.Member, bool) {
	m, ok := ctx.Value(ctxKeyMember).(db.Member)
	return m, ok
}

// WorkspaceIDFromContext returns the workspace ID injected by the workspace middleware.
func WorkspaceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyWorkspaceID).(string)
	return id
}

// SetMemberContext injects workspace ID and member into the context.
// This is useful for handlers that resolve the workspace from an entity lookup
// and want to share the member with downstream code.
func SetMemberContext(ctx context.Context, workspaceID string, member db.Member) context.Context {
	ctx = context.WithValue(ctx, ctxKeyWorkspaceID, workspaceID)
	ctx = context.WithValue(ctx, ctxKeyMember, member)
	return ctx
}

func resolveWorkspaceID(r *http.Request) string {
	if id := r.URL.Query().Get("workspace_id"); id != "" {
		return id
	}
	return r.Header.Get("X-Workspace-ID")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

// RequireWorkspaceMember resolves the workspace ID from query param or
// X-Workspace-ID header, validates membership, and injects the member
// and workspace ID into the request context.
func RequireWorkspaceMember(queries *db.Queries) func(http.Handler) http.Handler {
	return buildMiddleware(queries, resolveWorkspaceID, nil)
}

// RequireWorkspaceRole is like RequireWorkspaceMember but additionally checks
// that the member has one of the specified roles.
func RequireWorkspaceRole(queries *db.Queries, roles ...string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, resolveWorkspaceID, roles)
}

// RequireWorkspaceMemberFromURL resolves the workspace ID from a chi URL
// parameter, validates membership, and injects into context.
func RequireWorkspaceMemberFromURL(queries *db.Queries, param string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, func(r *http.Request) string {
		return chi.URLParam(r, param)
	}, nil)
}

// RequireWorkspaceRoleFromURL is like RequireWorkspaceMemberFromURL but
// additionally checks that the member has one of the specified roles.
func RequireWorkspaceRoleFromURL(queries *db.Queries, param string, roles ...string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, func(r *http.Request) string {
		return chi.URLParam(r, param)
	}, roles)
}

func buildMiddleware(queries *db.Queries, resolve func(*http.Request) string, roles []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID := resolve(r)
			if workspaceID == "" {
				writeError(w, http.StatusBadRequest, "workspace_id is required")
				return
			}

			userID := r.Header.Get("X-User-ID")
			if userID == "" {
				writeError(w, http.StatusUnauthorized, "user not authenticated")
				return
			}

			member, err := queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      userID,
				WorkspaceID: workspaceID,
			})
			if err != nil {
				writeError(w, http.StatusNotFound, "workspace not found")
				return
			}

			if len(roles) > 0 {
				allowed := false
				for _, role := range roles {
					if member.Role == role {
						allowed = true
						break
					}
				}
				if !allowed {
					writeError(w, http.StatusForbidden, "insufficient permissions")
					return
				}
			}

			ctx := SetMemberContext(r.Context(), workspaceID, member)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
