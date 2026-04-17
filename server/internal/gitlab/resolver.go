package gitlab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolverQueries is the narrow surface of *db.Queries the resolver needs.
// Defined as an interface so tests can stub without a DB.
type resolverQueries interface {
	GetWorkspaceGitlabConnection(ctx context.Context, workspaceID pgtype.UUID) (db.WorkspaceGitlabConnection, error)
	GetUserGitlabConnection(ctx context.Context, arg db.GetUserGitlabConnectionParams) (db.UserGitlabConnection, error)
}

// Resolver picks the right GitLab token for a write request.
//
// Construction takes a TokenDecrypter so tests can stub it; production wires
// in the secrets.Cipher's Decrypt method.
type Resolver struct {
	queries resolverQueries
	decrypt TokenDecrypter
}

// NewResolver constructs a Resolver. queries can be *db.Queries (production)
// or any stub implementing the resolverQueries interface (tests).
func NewResolver(queries resolverQueries, decrypt TokenDecrypter) *Resolver {
	return &Resolver{queries: queries, decrypt: decrypt}
}

// ResolveTokenForWrite returns the plaintext token to use for a GitLab API
// write call, plus a "source" string ("user" or "service") so the caller
// can attribute the cache row correctly.
//
// Rules:
//   - actorType="member", user PAT registered → user PAT, "user"
//   - actorType="member", no PAT             → workspace service PAT, "service"
//   - actorType="agent"                      → workspace service PAT, "service"
//
// Any other actorType returns an error — an unknown actor type (e.g. an
// empty string or a typo) must NOT silently fall back to the service PAT,
// because that would misattribute writes to the service account.
//
// Returns an error when the workspace itself has no GitLab connection
// (writes shouldn't have been routed here in that case).
func (r *Resolver) ResolveTokenForWrite(ctx context.Context, workspaceID, actorType, actorID string) (token string, source string, err error) {
	wsUUID, err := pgUUID(workspaceID)
	if err != nil {
		return "", "", fmt.Errorf("resolver: workspace_id: %w", err)
	}
	wsConn, err := r.queries.GetWorkspaceGitlabConnection(ctx, wsUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", fmt.Errorf("resolver: workspace has no gitlab connection")
		}
		return "", "", fmt.Errorf("resolver: workspace lookup: %w", err)
	}

	switch actorType {
	case "member":
		userUUID, err := pgUUID(actorID)
		if err == nil {
			userConn, err := r.queries.GetUserGitlabConnection(ctx, db.GetUserGitlabConnectionParams{
				UserID:      userUUID,
				WorkspaceID: wsUUID,
			})
			if err == nil {
				token, err := r.decrypt(ctx, userConn.PatEncrypted)
				if err != nil {
					return "", "", fmt.Errorf("resolver: decrypt user pat: %w", err)
				}
				return token, "user", nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return "", "", fmt.Errorf("resolver: user lookup: %w", err)
			}
			// no user PAT → fall through to service PAT
		}
	case "agent":
		// Agents don't have GitLab identities; always use the service PAT.
	default:
		return "", "", fmt.Errorf("resolver: unknown actor type %q", actorType)
	}

	token, err = r.decrypt(ctx, wsConn.ServiceTokenEncrypted)
	if err != nil {
		return "", "", fmt.Errorf("resolver: decrypt service pat: %w", err)
	}
	return token, "service", nil
}
