// Package mentiongate owns Cerebro's trigger gate for @mention and
// channel-listen dispatch.
package mentiongate

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/channels"
	"github.com/multica-ai/multica/server/internal/cerebro/grouppermissions"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Service centralises the group-permission allowlist used by trigger paths
// that live outside the grouppermissions package.
type Service struct {
	Queries     *db.Queries
	Permissions *grouppermissions.Service
}

func New(queries *db.Queries, perms *grouppermissions.Service) *Service {
	return &Service{Queries: queries, Permissions: perms}
}

// CanTriggerMention implements the handler.MentionTriggerGateInvoker seam.
// It intentionally fails closed when a request cannot resolve to a workspace
// member; a non-member request must not be allowed to wake an agent.
func (s *Service) CanTriggerMention(ctx context.Context, r *http.Request, workspaceID string, agentID, ownerID pgtype.UUID) (bool, error) {
	if s == nil || s.Permissions == nil {
		return true, nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false, nil
	}
	userUUID, err := util.ParseUUID(r.Header.Get("X-User-ID"))
	if err != nil {
		return false, nil
	}
	return s.canTriggerAgent(ctx, wsUUID, userUUID, agentID, ownerID)
}

// ChannelListenGate adapts the same gate to channels.Service.
func (s *Service) ChannelListenGate() channels.AgentTriggerGateFn {
	if s == nil || s.Permissions == nil {
		return nil
	}
	return func(ctx context.Context, workspaceID, userID, agentID, ownerID pgtype.UUID) (bool, error) {
		if !workspaceID.Valid || !userID.Valid {
			return false, nil
		}
		return s.canTriggerAgent(ctx, workspaceID, userID, agentID, ownerID)
	}
}

func (s *Service) canTriggerAgent(ctx context.Context, workspaceID, userID, agentID, ownerID pgtype.UUID) (bool, error) {
	if s == nil || s.Permissions == nil {
		return true, nil
	}
	member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return false, nil
	}
	isAdmin := member.Role == "owner" || member.Role == "admin"
	var groupIDs []pgtype.UUID
	if !isAdmin {
		ids, err := s.Permissions.ResolveGroupIDs(ctx, workspaceID, userID)
		if err != nil {
			return false, err
		}
		groupIDs = ids
	}
	viewer := grouppermissions.Viewer{
		UserID:   userID,
		IsAdmin:  isAdmin,
		GroupIDs: groupIDs,
	}
	allowed, err := s.Permissions.CanUseAgent(ctx, viewer, agentID)
	if err != nil || allowed {
		return allowed, err
	}
	if ownerID.Valid && ownerID.Bytes == userID.Bytes {
		if isAdmin {
			return true, nil
		}
		return s.Permissions.CanCreateAgent(ctx, viewer, workspaceID)
	}
	return false, nil
}
