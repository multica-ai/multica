package infisical

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/credentials"
)

// Provisioner wires the admin client to the persisted per-user identity
// record. It's the only thing handlers should call from the allowlist save
// path and the claim path — keeping the HTTP client + the DB + the cipher
// behind one interface keeps the handler code small.
type Provisioner struct {
	Admin   *AdminClient
	Queries *cerebrodb.Queries
	Cipher  *credentials.Cipher
}

// ErrProvisionerNotConfigured is returned when one of admin / queries /
// cipher is nil. Handlers map this to a 503 so an operator-side misconfig
// (missing env var, no DB) is distinguishable from a request-shape bug.
var ErrProvisionerNotConfigured = errors.New("infisical provisioner is not configured")

// SyncUserIdentity is called by the allowlist replace handler after the
// folder rows are committed. Behavior is contract-by-state:
//
//   - paths is empty → delete the user's identity (if any) and the DB row.
//     Removes the scope from Infisical entirely; nothing about this user is
//     left in the project once they have no granted folders.
//   - paths is non-empty and no identity row exists → create one.
//   - paths is non-empty and an identity row exists with a matching scope
//     fingerprint → no-op, return early. Important: re-saving the same
//     allow-list (a frequent UI shape — open the picker, hit save, close) must
//     not re-mint client secrets, because every new identity invalidates the
//     previous client_secret stored in our DB.
//   - paths is non-empty and an identity row exists with a *different*
//     fingerprint → delete the old identity in Infisical and provision a fresh
//     one. Atomic privilege-replace is not exposed by Infisical, so the safer
//     primitive is delete + recreate.
func (p *Provisioner) SyncUserIdentity(ctx context.Context, workspaceID, userID pgtype.UUID, name string, paths []FolderRef) error {
	if p == nil || p.Admin == nil || p.Queries == nil || p.Cipher == nil {
		return ErrProvisionerNotConfigured
	}

	existing, err := p.Queries.GetUserInfisicalIdentity(ctx, cerebrodb.GetUserInfisicalIdentityParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	hasExisting := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load user identity: %w", err)
	}

	if len(paths) == 0 {
		if !hasExisting {
			return nil
		}
		if err := p.Admin.DeleteIdentity(ctx, existing.IdentityID); err != nil && !isInfisicalNotFound(err) {
			return fmt.Errorf("delete identity in infisical: %w", err)
		}
		if err := p.Queries.DeleteUserInfisicalIdentity(ctx, cerebrodb.DeleteUserInfisicalIdentityParams{
			WorkspaceID: workspaceID,
			UserID:      userID,
		}); err != nil {
			return fmt.Errorf("delete user identity row: %w", err)
		}
		return nil
	}

	scope := ScopedPaths{Folders: paths}
	hash := scope.Hash()
	if hasExisting && existing.ScopedPathsHash == hash {
		return nil
	}

	priorIdentityID := ""
	if hasExisting {
		priorIdentityID = existing.IdentityID
	}
	creds, err := p.Admin.EnsureUserIdentity(ctx, name, priorIdentityID, scope)
	if err != nil {
		return fmt.Errorf("ensure identity: %w", err)
	}

	enc, err := p.Cipher.Encrypt([]byte(creds.ClientSecret))
	if err != nil {
		// Best-effort: roll back the freshly-created Infisical identity so we
		// don't leave it dangling with no DB record pointing at it.
		_ = p.Admin.DeleteIdentity(ctx, creds.IdentityID)
		return fmt.Errorf("encrypt client secret: %w", err)
	}

	if _, err := p.Queries.UpsertUserInfisicalIdentity(ctx, cerebrodb.UpsertUserInfisicalIdentityParams{
		WorkspaceID:            workspaceID,
		UserID:                 userID,
		IdentityID:             creds.IdentityID,
		ClientID:               creds.ClientID,
		ClientSecretCiphertext: enc,
		ScopedPathsHash:        hash,
	}); err != nil {
		_ = p.Admin.DeleteIdentity(ctx, creds.IdentityID)
		return fmt.Errorf("persist user identity: %w", err)
	}
	return nil
}

// FetchSecretsForFolders is called on the claim path. It loads the user's
// identity credentials, decrypts the client secret, and pulls the requested
// folders (which the caller is responsible for having intersected against the
// owner's allow-list already — Infisical's own scoping is the second layer).
//
// Returns (nil, nil) when the user has no provisioned identity: that's the
// "owner never had any allow-list entries" case, and the agent simply spawns
// with no Infisical-injected env. Same shape as the old daemon-side path
// when an agent had no folder grants.
func (p *Provisioner) FetchSecretsForFolders(ctx context.Context, workspaceID, userID pgtype.UUID, folders []FolderRef) (map[string]string, error) {
	if p == nil || p.Admin == nil || p.Queries == nil || p.Cipher == nil {
		return nil, ErrProvisionerNotConfigured
	}
	if len(folders) == 0 {
		return nil, nil
	}

	row, err := p.Queries.GetUserInfisicalIdentity(ctx, cerebrodb.GetUserInfisicalIdentityParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load user identity: %w", err)
	}

	secret, err := p.Cipher.Decrypt(row.ClientSecretCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt client secret: %w", err)
	}

	user := &UserClient{
		Domain:       p.Admin.Domain,
		ProjectID:    p.Admin.ProjectID,
		ClientID:     row.ClientID,
		ClientSecret: string(secret),
		HTTP:         p.Admin.HTTP,
	}
	return user.FetchFolderSecrets(ctx, folders)
}
