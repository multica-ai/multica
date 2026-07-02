package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrAppOwnedByLiveAgent is returned by ReclaimDeadAppID when the bot app's
// current owner is a LIVE binding — a present agent (archived or not) whose
// installation is active. The robot maps to one agent, so reusing it requires
// disconnecting the owner first. An archived owner counts as LIVE on purpose:
// archiving is reversible, so silently reclaiming it would destroy the owner's
// binding when they unarchive. Each channel translates this into its own
// user-facing sentinel (DingTalk: ErrAppOwnedByAnotherAgent, ...).
var ErrAppOwnedByLiveAgent = errors.New("channel: bot app is already connected to a live agent")

// ReclaimQuerier is the slice of generated queries ReclaimDeadAppID needs. The
// per-channel install-query interfaces (which embed these two methods) and the
// Lark ChannelStore all satisfy it, so every IM channel can share one reclaim
// implementation.
type ReclaimQuerier interface {
	GetChannelInstallationReclaimByAppID(ctx context.Context, arg db.GetChannelInstallationReclaimByAppIDParams) (db.GetChannelInstallationReclaimByAppIDRow, error)
	DeleteChannelInstallation(ctx context.Context, id pgtype.UUID) error
}

// ReclaimDeadAppID frees the (channel_type, app_id) routing key when the bot's
// current owner is a DEAD binding, so a NEW agent can take it over. "Dead" means
// EITHER the owner row is orphaned — its agent is gone (deleted workspace/agent;
// channel_installation has no FK, so a deleted workspace cascade-drops the agent
// but leaves this row) — OR it is a revoked binding IN THE SAME WORKSPACE, which
// the same team is free to reclaim when it moves the robot to another of its
// agents. A revoked binding in ANOTHER workspace is NOT dead: Revoke preserves
// the row so the owning workspace can re-install and restore its member/session
// bindings, so silently hard-deleting it here would destroy another workspace's
// recoverable data. Such a cross-workspace revoked owner is refused as LIVE. An
// ARCHIVED agent is likewise NOT dead: archiving is reversible, so reclaiming it
// would silently destroy the owner's binding when they unarchive — an archived
// owner is refused as LIVE, and the user must disconnect it first. A LIVE owner
// (present agent, active installation) is refused with ErrAppOwnedByLiveAgent.
// Re-connecting the SAME (workspace, agent) is left to the upsert's in-place
// update — never reclaimed, so an archived agent can still re-connect its robot.
//
// Callers MUST run this in the SAME tx and BEFORE the keyed upsert: a failed
// statement aborts a pgx tx, so the unique violation must not fire first. The
// same tx also matters for correctness — the probe takes a FOR UPDATE row lock
// on the owner installation (see GetChannelInstallationReclaimByAppID) that must
// be held through the DELETE, so a concurrent dead->live re-activation cannot
// slip in and get hard-deleted as if still dead.
func ReclaimDeadAppID(ctx context.Context, q ReclaimQuerier, channelType, appID string, wsID, agentID pgtype.UUID) error {
	owner, err := q.GetChannelInstallationReclaimByAppID(ctx, db.GetChannelInstallationReclaimByAppIDParams{
		ChannelType: channelType,
		AppID:       appID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // app_id is free; the upsert will insert cleanly.
		}
		return fmt.Errorf("probe channel app_id owner: %w", err)
	}
	if owner.WorkspaceID == wsID && owner.AgentID == agentID {
		return nil // same target: the upsert updates the row in place.
	}
	// Orphaned (agent/workspace gone) is dead regardless of workspace. A revoked
	// binding is dead only within the SAME workspace (a same-team robot move);
	// another workspace's revoked row is recoverable data (Revoke preserves it)
	// and must not be hard-deleted, so it is refused as LIVE.
	orphaned := !owner.AgentExists
	revokedSameWorkspace := owner.Status != "active" && owner.WorkspaceID == wsID
	if !orphaned && !revokedSameWorkspace {
		return ErrAppOwnedByLiveAgent
	}
	if err := q.DeleteChannelInstallation(ctx, owner.ID); err != nil {
		return fmt.Errorf("reclaim dead channel installation: %w", err)
	}
	return nil
}
