package wecom

// store.go — the data-layer adapter behind wecom's ResolverSet. It rides on
// the generalized channel_* tables (migration 124) using the shared sqlc
// queries; nothing in this file writes wecom-specific SQL.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Store is the read/write surface the wecom resolvers and outbound path use.
// It embeds *db.Queries so callers who need generic queries (workspace,
// member, agent) can reach them without another wrapper. All wecom-specific
// helpers scope by channel_type = 'wecom' internally.
type Store struct {
	*db.Queries
}

// NewStore constructs a wecom Store bound to a *db.Queries.
func NewStore(q *db.Queries) *Store {
	return &Store{Queries: q}
}

// WithTx binds this Store to a pgx.Tx so callers running the ingest tx can
// keep wecom lookups inside it.
func (s *Store) WithTx(tx pgx.Tx) *Store {
	return &Store{Queries: s.Queries.WithTx(tx)}
}

// GetInstallationByBotID looks up an installation by its smart-bot id. The
// routing key column is config->>'app_id' stored as the BotID directly (see
// encodeInstallConfig) so the shared idx_channel_installation_type_appid
// index does the work.
func (s *Store) GetInstallationByBotID(ctx context.Context, botID string) (Installation, error) {
	row, err := s.Queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: channelTypeWecom,
		AppID:       botID,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

// GetInstallation loads an installation by primary key, scoped to
// channel_type = 'wecom' so a Feishu id passed here is not silently reused.
func (s *Store) GetInstallation(ctx context.Context, id pgtype.UUID) (Installation, error) {
	row, err := s.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          id,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

// IsWorkspaceMember re-checks membership at inbound time. With channel_* FKs
// removed (MUL-3515 §4) a stale binding could otherwise route a message to a
// user who has since left the workspace.
func (s *Store) IsWorkspaceMember(ctx context.Context, workspaceID, userID pgtype.UUID) (bool, error) {
	_, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
