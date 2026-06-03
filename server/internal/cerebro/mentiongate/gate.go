// Package mentiongate owns Cerebro's trigger gate for @mention and
// channel-listen dispatch.
package mentiongate

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/channels"
	"github.com/multica-ai/multica/server/internal/cerebro/grouppermissions"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// triggerOtherAgentKey is the platform-capability key (platformcatalog) that
// governs "may you start an agent you do not own". FIR-2409 routes the mention
// gate through the tool-policy chain for this key, so an admin can configure
// the rule per workspace / group / member / agent from Settings instead of it
// living hardcoded in this gate. Must stay in sync with the catalog entry of
// the same name.
const triggerOtherAgentKey = "trigger_other_agent"

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
	// Today's hardcoded rule (admin master key + group allowlist + owner-self)
	// is the baseline. It still runs first and remains the default answer.
	baseline, err := s.baselineCanTrigger(ctx, workspaceID, userID, agentID, ownerID)
	if err != nil {
		return false, err
	}
	// Triggering your OWN agent is never the "trigger someone else's agent"
	// capability, so the configurable rule does not apply to the owner.
	ownsAgent := ownerID.Valid && ownerID.Bytes == userID.Bytes
	if ownsAgent || s.Permissions.Cerebro == nil {
		return baseline, nil
	}
	// FIR-2409: layer the configurable rule on top. Resolve the
	// trigger_other_agent capability through the unified tool-policy chain
	// (workspace > group > member > agent — auto-loads the viewer's groups from
	// UserID). When no admin has set a rule anywhere, DecidedBy is empty and we
	// keep today's behavior 1:1. An explicit Deny removes the master key (even
	// for an owner/admin); an explicit Allow grants a member who would
	// otherwise be blocked. Base = Allow so an unconfigured workspace is fully
	// governed by the baseline above.
	eff, err := toolpolicy.NewStoreFromQueries(s.Permissions.Cerebro).Resolve(ctx, toolpolicy.Query{
		WorkspaceID: workspaceID,
		ToolKey:     triggerOtherAgentKey,
		UserID:      userID,
		AgentID:     agentID,
		Base:        toolpolicy.SettingAllow,
	})
	if err != nil {
		return false, err
	}
	if eff.DecidedBy == "" {
		return baseline, nil
	}
	return eff.Setting == toolpolicy.SettingAllow, nil
}

// baselineCanTrigger is the original hardcoded trigger rule: owners/admins hold
// a master key, members are limited to agents a group grants them or that they
// own. It is the default that the configurable trigger_other_agent policy
// (canTriggerAgent) layers on top of.
func (s *Service) baselineCanTrigger(ctx context.Context, workspaceID, userID, agentID, ownerID pgtype.UUID) (bool, error) {
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
