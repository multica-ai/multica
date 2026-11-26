package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// errWorkspaceTokenUnavailable is returned when a workspace token cannot be
// looked up at all, as opposed to looked up and found invalid.
var errWorkspaceTokenUnavailable = errors.New("workspace token lookup unavailable")

// resolveWorkspaceToken turns a workspace token (mwt_) into its owner and the
// workspace it is confined to. Auth and DaemonAuth both resolve through here,
// so the two entry points cannot disagree on what a token is bound to.
//
// The cache holds the workspace together with the owner: a hit returns the
// same confinement a database read would.
func resolveWorkspaceToken(ctx context.Context, queries *db.Queries, patCache *auth.PATCache, token string) (auth.WorkspaceTokenIdentity, error) {
	hash := auth.HashToken(token)
	if identity, ok := patCache.GetWorkspaceToken(ctx, hash); ok {
		return identity, nil
	}
	if queries == nil {
		return auth.WorkspaceTokenIdentity{}, errWorkspaceTokenUnavailable
	}
	row, err := queries.GetWorkspaceTokenByHash(ctx, hash)
	if err != nil {
		return auth.WorkspaceTokenIdentity{}, err
	}
	identity := auth.WorkspaceTokenIdentity{
		UserID:      uuidToString(row.UserID),
		WorkspaceID: uuidToString(row.WorkspaceID),
	}

	// Same contract as the mul_ branch: clamp the entry to the token's
	// remaining lifetime, and refresh last_used_at once per cache window.
	var expiresAt time.Time
	if row.ExpiresAt.Valid {
		expiresAt = row.ExpiresAt.Time
	}
	patCache.SetWorkspaceToken(ctx, hash, identity, auth.TTLForExpiry(time.Now(), expiresAt))
	go queries.UpdatePersonalAccessTokenLastUsed(context.Background(), row.ID)

	return identity, nil
}

// errWorkspaceTokenAccountRoute is the 403 body for a workspace token on a
// route that acts on the account rather than on one workspace.
const errWorkspaceTokenAccountRoute = "workspace tokens cannot access account-level endpoints"

// RefuseWorkspaceToken refuses a workspace token. It guards the routes that
// act on the account as a whole (other credentials, user settings, creating
// workspaces, billing): nothing there can be confined to one workspace, so a
// token that is confined to one has no business on them.
func RefuseWorkspaceToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := WorkspaceTokenWorkspaceID(r.Context()); ok {
			writeError(w, http.StatusForbidden, errWorkspaceTokenAccountRoute)
			return
		}
		next.ServeHTTP(w, r)
	})
}
