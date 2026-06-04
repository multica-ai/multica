// Package channels also owns the cerebro-only "who may create a channel"
// gate (FIR-2660). Today any workspace member or agent can POST /api/channels;
// when the cerebro_channel_create_restricted flag is on for the workspace,
// channel (kind='channel') creation is restricted to owners/admins. DMs are
// never gated — a DM is between two people and has no shared rooster to
// protect.
//
// The guard is gated by the cerebro feature flag (registry.ts), resolved per
// workspace at request time, so it can be turned on/off from the Multica
// feature-flags screen with no env var and no server restart. Default OFF:
// with no override row prod behaviour is unchanged until an admin turns it on.

package channels

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FlagChannelCreateRestricted is the cerebro feature flag (registry.ts) that
// gates this guard. Default OFF: with no override row the guard stays inactive,
// so prod behaviour is unchanged until an admin turns the flag on (FIR-2660).
const FlagChannelCreateRestricted = "cerebro_channel_create_restricted"

// RestrictedMessage is returned to the caller when a channel creation is
// rejected, so the UI can show a clear, actionable error.
const RestrictedMessage = "only workspace owners or admins may create channels in this workspace (FIR-2660). Ask an admin to create the channel for you, or to turn off cerebro_channel_create_restricted."

// createGuardFlagReader is the subset of the cerebro Queries the guard needs
// to resolve its feature flag. Satisfied by *cerebrodb.Queries.
type createGuardFlagReader interface {
	ListCerebroWorkspaceFeatureFlags(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error)
}

// createGuardMemberReader is the subset of the upstream Queries needed to
// look up the caller's workspace role.
type createGuardMemberReader interface {
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
}

// CreateGuard is the Cerebro channel-creation guard. A nil CreateGuard, or one
// built with nil readers, is a safe no-op (guard disabled) so the wiring can
// never block by accident.
type CreateGuard struct {
	flags   createGuardFlagReader
	members createGuardMemberReader
}

// NewCreateGuard returns a guard that resolves its feature flag through the
// cerebro Queries and the member's role through the upstream Queries. Passing
// nil for either yields an always-disabled guard.
func NewCreateGuard(flags createGuardFlagReader, members createGuardMemberReader) *CreateGuard {
	return &CreateGuard{flags: flags, members: members}
}

// RejectCreate reports whether a CreateChannel call must be rejected before it
// is processed. It returns (message, ok): ok=true means the call passes;
// ok=false means it must be rejected with 403 and message explains why.
//
// Only kind='channel' is ever gated, and only when the
// cerebro_channel_create_restricted flag is ON for the workspace. Agents calling
// CreateChannel are treated as members (they cannot be workspace admins by
// definition, so the gate blocks them as it would a plain member).
func (g *CreateGuard) RejectCreate(ctx context.Context, workspaceID pgtype.UUID, callerType, callerID, kind string) (string, bool) {
	if g == nil {
		return "", true
	}
	if kind != "channel" {
		return "", true
	}
	if !g.enabled(ctx, workspaceID) {
		return "", true
	}
	// Agents are never workspace owners/admins — when the gate is on, they
	// cannot create channels.
	if callerType != "member" {
		return RestrictedMessage, false
	}
	if g.members == nil || !workspaceID.Valid || callerID == "" {
		return RestrictedMessage, false
	}
	uid, err := parseUUIDForGuard(callerID)
	if err != nil {
		return RestrictedMessage, false
	}
	row, err := g.members.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      uid,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("channelguard: member lookup failed", "error", err)
		}
		return RestrictedMessage, false
	}
	if row.Role != "owner" && row.Role != "admin" {
		return RestrictedMessage, false
	}
	return "", true
}

// enabled resolves the workspace-level cerebro_channel_create_restricted flag.
// No override row means default OFF; a DB error fails closed (guard inactive)
// and is logged. This mirrors the workspace resolution in
// packages/cerebro-feature-flags/store.ts.
func (g *CreateGuard) enabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if g.flags == nil || !workspaceID.Valid {
		return false
	}
	rows, err := g.flags.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("channelguard: workspace flag lookup failed", "error", err)
		}
		return false
	}
	for _, r := range rows {
		if r.FlagKey == FlagChannelCreateRestricted {
			return r.Enabled
		}
	}
	return false
}

func parseUUIDForGuard(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	err := u.Scan(s)
	return u, err
}
