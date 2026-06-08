package lark

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InstallationParams is the input shape RegistrationService assembles
// after a successful device-flow scan-to-install. The credentials are
// supplied here as plaintext — encryption happens inside
// InstallationService.Upsert via the supplied *secretbox.Box, so
// callers never see (and therefore cannot leak) the ciphertext that
// lands in the DB.
type InstallationParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	AppID           string
	AppSecret       string // plaintext; encrypted at the service boundary
	TenantKey       string // optional, "" treated as NULL
	BotOpenID       string
	InstallerUserID pgtype.UUID
	Region          Region // which cloud (feishu/lark); empty defaults to feishu
}

// InstallationService creates, refreshes and revokes per-agent Lark
// installations. It owns the at-rest encryption of `app_secret` so
// that no caller (and no test fixture) can accidentally insert a row
// with plaintext credentials — the only path to writing
// lark_installation goes through here.
type InstallationService struct {
	queries *db.Queries
	box     *secretbox.Box
}

// NewInstallationService binds the service to a queries handle and a
// secretbox keyed for at-rest encryption. The box MUST be non-nil; we
// refuse to fall back to plaintext storage even in test or dev
// configurations because that is exactly the regression the §4.4
// requirement guards against.
func NewInstallationService(queries *db.Queries, box *secretbox.Box) (*InstallationService, error) {
	if box == nil {
		return nil, errors.New("lark: InstallationService requires a non-nil secretbox.Box")
	}
	return &InstallationService{queries: queries, box: box}, nil
}

// Upsert creates a new installation or refreshes an existing one in
// place (matching on the (workspace_id, agent_id) UNIQUE). Re-install
// resets status to 'active' but does NOT touch the WS lease — that is
// the hub's concern, not ours. The returned row is the post-write
// state; the encrypted secret column is included for completeness but
// callers SHOULD NOT log or persist it elsewhere.
func (s *InstallationService) Upsert(ctx context.Context, p InstallationParams) (db.LarkInstallation, error) {
	if err := validateInstallationParams(p); err != nil {
		return db.LarkInstallation{}, err
	}
	sealed, err := s.box.Seal([]byte(p.AppSecret))
	if err != nil {
		return db.LarkInstallation{}, fmt.Errorf("encrypt app_secret: %w", err)
	}
	return s.queries.UpsertLarkInstallation(ctx, db.UpsertLarkInstallationParams{
		WorkspaceID:        p.WorkspaceID,
		AgentID:            p.AgentID,
		AppID:              p.AppID,
		AppSecretEncrypted: sealed,
		TenantKey:          textOrNull(p.TenantKey),
		BotOpenID:          p.BotOpenID,
		InstallerUserID:    p.InstallerUserID,
		Region:             string(RegionOrDefault(string(p.Region))),
	})
}

// Revoke flips status to 'revoked' so the WS hub tears the connection
// down on its next sweep and the dispatcher drops any in-flight
// events. The row is preserved (no DELETE) so audit history remains
// queryable; a subsequent re-install via Upsert flips status back to
// 'active' atomically.
func (s *InstallationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.queries.SetLarkInstallationStatus(ctx, db.SetLarkInstallationStatusParams{
		ID:     id,
		Status: string(InstallationRevoked),
	})
}

// DecryptAppSecret returns the plaintext app_secret for the supplied
// installation row. Used by the WebSocket hub when it needs to
// authenticate against the Lark API on behalf of an installation; do
// NOT use this for read-only display surfaces. The plaintext value
// must never round-trip through an HTTP response.
func (s *InstallationService) DecryptAppSecret(inst db.LarkInstallation) (string, error) {
	plain, err := s.box.Open(inst.AppSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt app_secret: %w", err)
	}
	return string(plain), nil
}

// GetInWorkspace is the workspace-scoped lookup helper. Internal
// callers (Dispatcher) use GetLarkInstallationByAppID directly because
// the event payload only carries app_id; HTTP-side callers always
// know the workspace and should use this so a forged installation_id
// from a different workspace returns NotFound instead of leaking
// existence.
func (s *InstallationService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (db.LarkInstallation, error) {
	row, err := s.queries.GetLarkInstallationInWorkspace(ctx, db.GetLarkInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.LarkInstallation{}, ErrInstallationNotFound
		}
		return db.LarkInstallation{}, err
	}
	return row, nil
}

// ListByWorkspace returns every installation rooted at the workspace,
// active and revoked, oldest first. The status column lets the UI
// distinguish "wired up" from "torn down but kept for audit".
func (s *InstallationService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.LarkInstallation, error) {
	return s.queries.ListLarkInstallationsByWorkspace(ctx, workspaceID)
}

// ErrInstallationNotFound surfaces "no row matches in this workspace"
// — used by the HTTP layer to return 404. Distinct from a plain
// pgx.ErrNoRows so handlers do not need to import pgx.
var ErrInstallationNotFound = errors.New("lark installation not found")

func validateInstallationParams(p InstallationParams) error {
	switch {
	case !p.WorkspaceID.Valid:
		return errors.New("workspace_id is required")
	case !p.AgentID.Valid:
		return errors.New("agent_id is required")
	case !p.InstallerUserID.Valid:
		return errors.New("installer_user_id is required")
	case p.AppID == "":
		return errors.New("app_id is required")
	case p.AppSecret == "":
		return errors.New("app_secret is required")
	case p.BotOpenID == "":
		return errors.New("bot_open_id is required")
	}
	return nil
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
