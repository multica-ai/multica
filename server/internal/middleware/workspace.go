package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/util"
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

// errWorkspaceNotFound is returned when a slug was provided but doesn't match
// any workspace. This lets the middleware distinguish "no identifier provided"
// (400) from "identifier provided but invalid" (404).
var errWorkspaceNotFound = errors.New("workspace not found")

// ResolveWorkspaceIDFromRequest returns the workspace UUID for an HTTP
// request using the same priority order as the workspace middleware. This is
// the single source of truth for "which workspace is this request targeting?",
// shared by middleware-protected routes (via context fast path) and
// middleware-less routes (e.g. /api/upload-file) that must resolve the slug
// themselves.
//
// Priority:
//  1. middleware-injected context (fast path for middleware-protected routes)
//  2. X-Workspace-Slug header → GetWorkspaceBySlug → UUID (post-refactor frontend)
//  3. ?workspace_slug query → GetWorkspaceBySlug → UUID
//  4. X-Workspace-ID header (CLI/daemon compat)
//  5. ?workspace_id query (CLI/daemon compat)
//
// Returns "" when no identifier was provided OR a slug was provided but
// doesn't resolve to any workspace. Callers that need to distinguish "no
// identifier" (400) from "invalid slug" (404) should use the middleware's
// internal resolver instead — this helper collapses both cases to "" for
// simpler handler-level checks.
func ResolveWorkspaceIDFromRequest(r *http.Request, queries *db.Queries) string {
	if id := WorkspaceIDFromContext(r.Context()); id != "" {
		return id
	}
	if slug := r.Header.Get("X-Workspace-Slug"); slug != "" {
		if ws, err := queries.GetWorkspaceBySlug(r.Context(), slug); err == nil {
			return util.UUIDToString(ws.ID)
		}
	}
	if slug := r.URL.Query().Get("workspace_slug"); slug != "" {
		if ws, err := queries.GetWorkspaceBySlug(r.Context(), slug); err == nil {
			return util.UUIDToString(ws.ID)
		}
	}
	if id := r.Header.Get("X-Workspace-ID"); id != "" {
		return id
	}
	return r.URL.Query().Get("workspace_id")
}

// workspaceResolver extracts a workspace UUID from the request.
// Returns ("", nil) if no workspace identifier was provided at all.
// Returns ("", errWorkspaceNotFound) if a slug was provided but doesn't exist.
// Returns (uuid, nil) on success.
type workspaceResolver func(r *http.Request) (string, error)

// resolveWorkspaceUUID builds a resolver that accepts slug-first identification.
//
// Priority:
//  1. X-Workspace-Slug header / ?workspace_slug query → GetWorkspaceBySlug → UUID
//  2. X-Workspace-ID header / ?workspace_id query → UUID directly (CLI/daemon compat)
//
// TODO: cache slug→UUID lookup (slug is immutable, safe to cache with short TTL)
func resolveWorkspaceUUID(queries *db.Queries) workspaceResolver {
	return func(r *http.Request) (string, error) {
		// Slug path (preferred — frontend sends this after the URL refactor)
		if slug := r.URL.Query().Get("workspace_slug"); slug != "" {
			ws, err := queries.GetWorkspaceBySlug(r.Context(), slug)
			if err != nil {
				return "", errWorkspaceNotFound
			}
			return util.UUIDToString(ws.ID), nil
		}
		if slug := r.Header.Get("X-Workspace-Slug"); slug != "" {
			ws, err := queries.GetWorkspaceBySlug(r.Context(), slug)
			if err != nil {
				return "", errWorkspaceNotFound
			}
			return util.UUIDToString(ws.ID), nil
		}
		// UUID fallback (CLI, daemon, legacy clients)
		if id := r.URL.Query().Get("workspace_id"); id != "" {
			return id, nil
		}
		if id := r.Header.Get("X-Workspace-ID"); id != "" {
			return id, nil
		}
		return "", nil
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

// RequireWorkspaceMember resolves the workspace from slug (preferred) or UUID
// (fallback), validates membership, and injects the member and workspace ID
// into the request context.
func RequireWorkspaceMember(queries *db.Queries) func(http.Handler) http.Handler {
	return buildMiddleware(queries, resolveWorkspaceUUID(queries), nil)
}

// RequireWorkspaceRole is like RequireWorkspaceMember but additionally checks
// that the member has one of the specified roles.
func RequireWorkspaceRole(queries *db.Queries, roles ...string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, resolveWorkspaceUUID(queries), roles)
}

// RequireWorkspaceMemberFromURL resolves the workspace ID from a chi URL
// parameter, validates membership, and injects into context.
func RequireWorkspaceMemberFromURL(queries *db.Queries, param string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, func(r *http.Request) (string, error) {
		id := chi.URLParam(r, param)
		if id == "" {
			return "", nil
		}
		return id, nil
	}, nil)
}

// RequireWorkspaceRoleFromURL is like RequireWorkspaceMemberFromURL but
// additionally checks that the member has one of the specified roles.
func RequireWorkspaceRoleFromURL(queries *db.Queries, param string, roles ...string) func(http.Handler) http.Handler {
	return buildMiddleware(queries, func(r *http.Request) (string, error) {
		id := chi.URLParam(r, param)
		if id == "" {
			return "", nil
		}
		return id, nil
	}, roles)
}

func buildMiddleware(queries *db.Queries, resolve workspaceResolver, roles []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID, resolveErr := resolve(r)
			if resolveErr != nil {
				writeError(w, http.StatusNotFound, "workspace not found")
				return
			}
			if workspaceID == "" {
				writeError(w, http.StatusBadRequest, "workspace_id or workspace_slug is required")
				return
			}

			userID := r.Header.Get("X-User-ID")
			if userID == "" {
				writeError(w, http.StatusUnauthorized, "user not authenticated")
				return
			}

			member, err := queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      util.ParseUUID(userID),
				WorkspaceID: util.ParseUUID(workspaceID),
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
