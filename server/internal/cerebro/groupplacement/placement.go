// Package groupplacement assigns workspace members to the configured default
// cerebro group without pulling in handler or identity packages (avoids cycles).
package groupplacement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// PlaceUserInDefaultGroup adds the user to the workspace's configured default
// group when one exists. Idempotent: duplicate membership is ignored.
func PlaceUserInDefaultGroup(
	ctx context.Context,
	cerebro *cerebrodb.Queries,
	workspaceID, userID pgtype.UUID,
) error {
	if cerebro == nil {
		return nil
	}
	groupID, err := cerebro.GetCerebroDefaultGroupID(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup default group: %w", err)
	}
	if _, err := cerebro.AddCerebroGroupMember(ctx, cerebrodb.AddCerebroGroupMemberParams{
		GroupID: groupID,
		UserID:  userID,
		AddedBy: pgtype.UUID{},
	}); err != nil {
		return fmt.Errorf("add default-group member: %w", err)
	}
	return nil
}

// SetWorkspaceDefaultGroup validates and stores the workspace fallback group.
func SetWorkspaceDefaultGroup(
	ctx context.Context,
	cerebro *cerebrodb.Queries,
	workspaceID pgtype.UUID,
	groupIDRaw string,
) error {
	groupIDRaw = strings.TrimSpace(groupIDRaw)
	if groupIDRaw == "" {
		return ErrInvalidDefaultGroup
	}
	groupID, err := util.ParseUUID(groupIDRaw)
	if err != nil {
		return ErrInvalidDefaultGroup
	}
	group, err := cerebro.GetCerebroGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidDefaultGroup
		}
		return err
	}
	if group.WorkspaceID != workspaceID {
		return ErrInvalidDefaultGroup
	}
	return cerebro.SetCerebroDefaultGroup(ctx, cerebrodb.SetCerebroDefaultGroupParams{
		WorkspaceID: workspaceID,
		GroupID:     groupID,
	})
}

// LoadDefaultGroupID returns the configured default group UUID string, or nil.
func LoadDefaultGroupID(
	ctx context.Context,
	cerebro *cerebrodb.Queries,
	workspaceID pgtype.UUID,
) (*string, error) {
	groupID, err := cerebro.GetCerebroDefaultGroupID(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	id := util.UUIDToString(groupID)
	return &id, nil
}

// ErrInvalidDefaultGroup is returned when the group id is missing or invalid.
var ErrInvalidDefaultGroup = errors.New("invalid default_group_id")
