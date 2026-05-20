// CEREBRO-PATCH(cerebro-channel-listen-gate): JEH-1727 — net-new cerebro-only
// factory for the listen-mode AgentTriggerGate. Lives under server/cmd/server/
// alongside the other cerebro_* router glue so router.go's wiring stays a
// single line. See docs/cerebro-patches.md.
//
// Closes the trigger-path hole described in JEH-1727 (Filip-can-trigger-Mia).
// The visibility split landed in JEH-1066 was correct — workspace members may
// SEE every agent — but trigger paths (assignment, chat, mention) must still
// pass the group-allowlist. enqueueMentionedAgentTasks now calls
// cerebroCanUseAgent inline, and the listen-mode dispatcher in
// internal/cerebro/channels calls the closure returned here.

package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/channels"
	"github.com/multica-ai/multica/server/internal/cerebro/grouppermissions"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newChannelListenAgentGate returns the closure wired into
// channels.Service.AgentTriggerGate. It resolves the comment author's viewer
// (admin flag + effective group IDs) and asks the cerebro
// grouppermissions.Service whether the candidate agent is on the viewer's
// allowlist, with the JEH-1057 owner-with-create_agent exemption layered on
// top — same shape as handler.cerebroCanUseAgent.
//
// Returns (true, nil) when the viewer can use the agent, (false, nil) when
// the allowlist denies and no exemption applies, and (false, err) on a
// service-level error (DB hiccup). The caller skips enqueue in either deny
// branch.
func newChannelListenAgentGate(queries *db.Queries, perms *grouppermissions.Service) channels.AgentTriggerGateFn {
	if perms == nil {
		return nil
	}
	return func(ctx context.Context, workspaceID, userID, agentID, ownerID pgtype.UUID) (bool, error) {
		if !userID.Valid {
			return false, nil
		}
		member, err := queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      userID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return false, nil
		}
		isAdmin := member.Role == "owner" || member.Role == "admin"
		var groupIDs []pgtype.UUID
		if !isAdmin {
			ids, err := perms.ResolveGroupIDs(ctx, workspaceID, userID)
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
		allowed, err := perms.CanUseAgent(ctx, viewer, agentID)
		if err != nil || allowed {
			return allowed, err
		}
		if ownerID.Valid && ownerID.Bytes == userID.Bytes {
			if isAdmin {
				return true, nil
			}
			return perms.CanCreateAgent(ctx, viewer, workspaceID)
		}
		return false, nil
	}
}
