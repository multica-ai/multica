package weixin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Store struct{ *db.Queries }

func NewStore(q *db.Queries) *Store      { return &Store{Queries: q} }
func (s *Store) WithTx(tx pgx.Tx) *Store { return &Store{Queries: s.Queries.WithTx(tx)} }

func (s *Store) GetInstallationByBotID(ctx context.Context, botID string) (Installation, error) {
	row, err := s.Queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: channelTypeWeixin,
		AppID:       botID,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *Store) GetInstallation(ctx context.Context, id pgtype.UUID) (Installation, error) {
	row, err := s.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: id, ChannelType: channelTypeWeixin})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *Store) IsWorkspaceMember(ctx context.Context, workspaceID, userID pgtype.UUID) (bool, error) {
	_, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: userID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
