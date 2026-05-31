package identitysync

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dbStore is the production Store: cerebro queries for the group tables,
// upstream queries for user + workspace-membership lookups.
type dbStore struct {
	cerebro  *cerebrodb.Queries
	upstream *db.Queries
}

// compile-time assertion.
var _ Store = (*dbStore)(nil)

func (s *dbStore) UpsertGroup(ctx context.Context, workspaceID pgtype.UUID, googleEmail, name string) (pgtype.UUID, error) {
	row, err := s.cerebro.UpsertCerebroGroupByGoogleEmail(ctx, cerebrodb.UpsertCerebroGroupByGoogleEmailParams{
		WorkspaceID:      workspaceID,
		Name:             name,
		GoogleGroupEmail: pgtype.Text{String: googleEmail, Valid: true},
	})
	if err != nil {
		return pgtype.UUID{}, err
	}
	return row.ID, nil
}

func (s *dbStore) ResolveWorkspaceUser(ctx context.Context, workspaceID pgtype.UUID, email string) (pgtype.UUID, bool, error) {
	user, err := s.upstream.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, false, nil
		}
		return pgtype.UUID{}, false, err
	}
	if _, err := s.upstream.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Has a Multica account but is not a member of this workspace —
			// skip, same as "no account". Group membership requires workspace
			// membership.
			return pgtype.UUID{}, false, nil
		}
		return pgtype.UUID{}, false, err
	}
	return user.ID, true, nil
}

func (s *dbStore) ListSyncedMemberIDs(ctx context.Context, groupID pgtype.UUID) ([]pgtype.UUID, error) {
	return s.cerebro.ListCerebroGroupMembersBySource(ctx, cerebrodb.ListCerebroGroupMembersBySourceParams{
		GroupID: groupID,
		Source:  SourceGoogleWorkspace,
	})
}

func (s *dbStore) AddSyncedMember(ctx context.Context, groupID, userID pgtype.UUID) error {
	return s.cerebro.AddCerebroGroupMemberWithSource(ctx, cerebrodb.AddCerebroGroupMemberWithSourceParams{
		GroupID: groupID,
		UserID:  userID,
		Source:  SourceGoogleWorkspace,
	})
}

func (s *dbStore) RemoveSyncedMember(ctx context.Context, groupID, userID pgtype.UUID) error {
	return s.cerebro.RemoveCerebroGroupMemberBySource(ctx, cerebrodb.RemoveCerebroGroupMemberBySourceParams{
		GroupID: groupID,
		UserID:  userID,
		Source:  SourceGoogleWorkspace,
	})
}
