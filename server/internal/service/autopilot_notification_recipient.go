package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AutopilotNotificationRecipient is a human recipient of an autopilot system
// notice.
type AutopilotNotificationRecipient struct {
	Type string
	ID   pgtype.UUID
}

// ListWorkspaceManagerNotificationRecipients returns the current workspace
// owners and admins for workspace-level notices and responsibility fallbacks.
func ListWorkspaceManagerNotificationRecipients(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
) ([]AutopilotNotificationRecipient, error) {
	userIDs, err := queries.ListWorkspaceManagerUserIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	recipients := make([]AutopilotNotificationRecipient, 0, len(userIDs))
	for _, userID := range userIDs {
		recipients = append(recipients, AutopilotNotificationRecipient{Type: "member", ID: userID})
	}
	return recipients, nil
}

// ResolveAutopilotNotificationRecipient routes a notice to the autopilot's
// member creator, or to the owning workspace member when an agent created it.
// A creator that no longer resolves to an actionable human returns ok=false.
func ResolveAutopilotNotificationRecipient(
	ctx context.Context,
	queries *db.Queries,
	autopilot db.Autopilot,
) (recipient AutopilotNotificationRecipient, ok bool, err error) {
	if autopilot.CreatedByType == "member" {
		if !autopilot.CreatedByID.Valid {
			return AutopilotNotificationRecipient{}, false, nil
		}
		member, err := queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      autopilot.CreatedByID,
			WorkspaceID: autopilot.WorkspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return AutopilotNotificationRecipient{}, false, nil
		}
		if err != nil {
			return AutopilotNotificationRecipient{}, false, fmt.Errorf("load autopilot member creator: %w", err)
		}
		return AutopilotNotificationRecipient{
			Type: "member",
			ID:   member.UserID,
		}, true, nil
	}
	if autopilot.CreatedByType != "agent" {
		return AutopilotNotificationRecipient{}, false, nil
	}

	agent, err := queries.GetAgent(ctx, autopilot.CreatedByID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AutopilotNotificationRecipient{}, false, nil
	}
	if err != nil {
		return AutopilotNotificationRecipient{}, false, fmt.Errorf("load autopilot creator agent: %w", err)
	}
	if !agent.OwnerID.Valid || agent.WorkspaceID.Bytes != autopilot.WorkspaceID.Bytes {
		return AutopilotNotificationRecipient{}, false, nil
	}

	member, err := queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      agent.OwnerID,
		WorkspaceID: autopilot.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AutopilotNotificationRecipient{}, false, nil
	}
	if err != nil {
		return AutopilotNotificationRecipient{}, false, fmt.Errorf("load autopilot creator agent owner: %w", err)
	}
	return AutopilotNotificationRecipient{Type: "member", ID: member.UserID}, true, nil
}
