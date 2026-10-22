package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// errTaskTokenWorkspaceMismatch is the 403 body returned when a task-token
// request explicitly targets a workspace other than the one bound into the
// token. It matches the message buildMiddleware already uses for URL-param
// conflicts so clients see one error for one condition.
const errTaskTokenWorkspaceMismatch = "task token is bound to a different workspace"

// checkTaskTokenWorkspaceTarget rejects a task-token request whose explicit
// workspace selectors disagree with the token's bound workspace (GitHub
// #9403).
//
// The token decides WHERE the agent may act; the selectors say where the
// caller THINKS it is acting. The mat_ branch of Auth later overwrites
// X-Workspace-ID with the bound workspace, and both resolvers short-circuit
// on task_token, so without this check a request for workspace B silently
// ran against A. Running it here, before the overwrite, also covers routes
// that never pass through the workspace middleware (e.g. file upload).
//
// Every value of every selector is checked — X-Workspace-ID,
// X-Workspace-Slug, ?workspace_id, ?workspace_slug — so a duplicated
// parameter (`workspace_id=A&workspace_id=B`) cannot slip a conflicting
// value past a first-value-only read. Empty values carry no target and are
// treated like an absent selector, matching how every resolver and the CLI
// (which may send `workspace_id=` when unset) already behave; they never
// select a workspace, so they cannot cause the silent substitution.
//
// Returns true when it has written an error response; the caller must stop.
// Status codes:
//   - 400 query string that cannot be fully parsed, or malformed workspace ID
//   - 404 unknown workspace slug
//   - 403 selector resolves to a different workspace than the token's
//   - 500 slug lookup failed for a reason other than "not found"
func checkTaskTokenWorkspaceTarget(w http.ResponseWriter, r *http.Request, queries *db.Queries, boundWorkspaceID string) bool {
	// r.URL.Query() silently drops pairs it cannot decode (bad percent
	// escapes, unencoded ';'), so `workspace_slug=ws-b%zz` would look like
	// "no selector" and fall through to the bound workspace. Parse strictly
	// and refuse anything we cannot read in full.
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		slog.Warn("auth: task token request has malformed query string",
			"path", r.URL.Path,
			"method", r.Method,
			"bound_workspace_id", boundWorkspaceID,
			"error", err,
		)
		writeError(w, http.StatusBadRequest, "malformed query string")
		return true
	}

	reject := func(status int, msg, selector, value string) bool {
		slog.Warn("auth: task token workspace selector rejected",
			"path", r.URL.Path,
			"method", r.Method,
			"selector", selector,
			"value", value,
			"bound_workspace_id", boundWorkspaceID,
			"status", status,
		)
		writeError(w, status, msg)
		return true
	}

	// UUID selectors: compare canonical forms so casing / formatting
	// differences of the same workspace are not mistaken for a conflict.
	idSelectors := []struct {
		name   string
		values []string
	}{
		{"header:X-Workspace-ID", r.Header.Values("X-Workspace-ID")},
		{"query:workspace_id", query["workspace_id"]},
	}
	for _, sel := range idSelectors {
		for _, v := range sel.values {
			if v == "" {
				continue
			}
			parsed, err := util.ParseUUID(v)
			if err != nil {
				return reject(http.StatusBadRequest, "invalid workspace_id", sel.name, v)
			}
			if util.UUIDToString(parsed) != boundWorkspaceID {
				return reject(http.StatusForbidden, errTaskTokenWorkspaceMismatch, sel.name, v)
			}
		}
	}

	// Slug selectors: resolve each to a UUID. A lookup failure must never
	// fall back to the bound workspace.
	slugSelectors := []struct {
		name   string
		values []string
	}{
		{"header:X-Workspace-Slug", r.Header.Values("X-Workspace-Slug")},
		{"query:workspace_slug", query["workspace_slug"]},
	}
	resolved := make(map[string]string)
	for _, sel := range slugSelectors {
		for _, slug := range sel.values {
			if slug == "" {
				continue
			}
			id, ok := resolved[slug]
			if !ok {
				ws, err := queries.GetWorkspaceBySlug(r.Context(), slug)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return reject(http.StatusNotFound, "workspace not found", sel.name, slug)
					}
					slog.Error("auth: task token workspace slug lookup failed", "path", r.URL.Path, "error", err)
					writeError(w, http.StatusInternalServerError, "failed to resolve workspace")
					return true
				}
				id = util.UUIDToString(ws.ID)
				resolved[slug] = id
			}
			if id != boundWorkspaceID {
				return reject(http.StatusForbidden, errTaskTokenWorkspaceMismatch, sel.name, slug)
			}
		}
	}
	return false
}
