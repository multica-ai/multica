// TECH-3698 — per-channel permission settings: who may rename the channel,
// who may add/remove other participants, and whether members may remove
// themselves (leave). Channels are issues with kind='channel'. A missing row
// means "defaults" (rename = admins only, add/remove others = everyone, leave
// = allowed). The creator picks these at creation time and an admin/owner (or
// the creator) can change them later from channel settings. DMs are never
// gated — the gate methods short-circuit before this package is consulted.
package channels

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Permission-policy values.
const (
	RenamePolicyAdmins       = "admins"
	RenamePolicyEveryone     = "everyone"
	AddMembersPolicyAdmins   = "admins"
	AddMembersPolicyEveryone = "everyone"
)

// ChannelPermissions is the API + storage shape for a channel's settings.
type ChannelPermissions struct {
	RenamePolicy     string `json:"rename_policy"`
	AddMembersPolicy string `json:"add_members_policy"`
	AllowSelfLeave   bool   `json:"allow_self_leave"`
}

// DefaultPermissions are applied to any channel without an explicit row.
func DefaultPermissions() ChannelPermissions {
	return ChannelPermissions{
		RenamePolicy:     RenamePolicyAdmins,
		AddMembersPolicy: AddMembersPolicyEveryone,
		AllowSelfLeave:   true,
	}
}

func validRenamePolicy(p string) bool {
	return p == RenamePolicyAdmins || p == RenamePolicyEveryone
}

func validAddMembersPolicy(p string) bool {
	return p == AddMembersPolicyAdmins || p == AddMembersPolicyEveryone
}

// EffectivePermissions returns the stored settings, or defaults when no row
// exists (or on any read error — fail open to defaults, never block on a
// settings lookup hiccup).
func (s *Service) EffectivePermissions(ctx context.Context, channelID pgtype.UUID) ChannelPermissions {
	row, err := s.CerebroQueries.GetChannelPermissions(ctx, channelID)
	if err != nil {
		return DefaultPermissions()
	}
	return ChannelPermissions{
		RenamePolicy:     row.RenamePolicy,
		AddMembersPolicy: row.AddMembersPolicy,
		AllowSelfLeave:   row.AllowSelfLeave,
	}
}

// SetPermissionsRow upserts the settings for a channel. Used on creation and
// from the settings UI.
func (s *Service) SetPermissionsRow(ctx context.Context, channelID pgtype.UUID, p ChannelPermissions) error {
	_, err := s.CerebroQueries.UpsertChannelPermissions(ctx, cerebrodb.UpsertChannelPermissionsParams{
		ChannelID:        channelID,
		RenamePolicy:     p.RenamePolicy,
		AddMembersPolicy: p.AddMembersPolicy,
		AllowSelfLeave:   p.AllowSelfLeave,
	})
	return err
}

// SetCreatePermissions persists the creator's chosen policies for a new
// channel. Invalid/empty values fall back to the defaults so a malformed
// create request can never store an out-of-range policy.
func (s *Service) SetCreatePermissions(ctx context.Context, channelID pgtype.UUID, renamePolicy, addMembersPolicy string, allowSelfLeave bool) error {
	p := DefaultPermissions()
	if validRenamePolicy(renamePolicy) {
		p.RenamePolicy = renamePolicy
	}
	if validAddMembersPolicy(addMembersPolicy) {
		p.AddMembersPolicy = addMembersPolicy
	}
	p.AllowSelfLeave = allowSelfLeave
	return s.SetPermissionsRow(ctx, channelID, p)
}

// isChannelPrivileged reports whether the caller may bypass the per-channel
// policies — true for the channel creator, for workspace admins/owners, and
// for agent callers (trusted automation). The result is the privilege that
// every policy treats as "always allowed".
func (s *Service) isChannelPrivileged(ctx context.Context, channelID pgtype.UUID, callerType, callerID string) bool {
	if callerType == "agent" {
		return true
	}
	issue, err := s.Queries.GetIssue(ctx, channelID)
	if err != nil {
		return false
	}
	if issue.CreatorType == callerType && util.UUIDToString(issue.CreatorID) == callerID {
		return true
	}
	uid, err := util.ParseUUID(callerID)
	if err != nil {
		return false
	}
	m, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      uid,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}
	return m.Role == "owner" || m.Role == "admin"
}

// CanRenameChannel implements the upstream ChannelPermsInvoker seam: may this
// caller change the channel's name?
func (s *Service) CanRenameChannel(ctx context.Context, channelID pgtype.UUID, callerType, callerID string) bool {
	if s.EffectivePermissions(ctx, channelID).RenamePolicy == RenamePolicyEveryone {
		return true
	}
	return s.isChannelPrivileged(ctx, channelID, callerType, callerID)
}

// CanManageMembers implements the seam: may this caller add or remove *other*
// participants in the channel?
func (s *Service) CanManageMembers(ctx context.Context, channelID pgtype.UUID, callerType, callerID string) bool {
	if s.EffectivePermissions(ctx, channelID).AddMembersPolicy == AddMembersPolicyEveryone {
		return true
	}
	return s.isChannelPrivileged(ctx, channelID, callerType, callerID)
}

// CanSelfLeave implements the seam: may a member remove themselves (leave)?
// Privileged callers (admins/creator) may always leave.
func (s *Service) CanSelfLeave(ctx context.Context, channelID pgtype.UUID, callerType, callerID string) bool {
	if s.EffectivePermissions(ctx, channelID).AllowSelfLeave {
		return true
	}
	return s.isChannelPrivileged(ctx, channelID, callerType, callerID)
}

// --- HTTP handlers ---------------------------------------------------------

// GetPermissions handles GET /api/channels/{id}/permissions. Any participant
// may read the settings.
func (s *Service) GetPermissions(w http.ResponseWriter, r *http.Request) {
	channelID, ok := s.requireChannelMember(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.EffectivePermissions(r.Context(), channelID))
}

// SetPermissions handles PUT /api/channels/{id}/permissions. Only privileged
// callers (admins/owners or the channel creator) may change the settings.
func (s *Service) SetPermissions(w http.ResponseWriter, r *http.Request) {
	channelID, ok := s.requireChannelMember(w, r)
	if !ok {
		return
	}

	callerType := "member"
	if at := r.Header.Get("X-Agent-ID"); at != "" {
		callerType = "agent"
	}
	callerID := r.Header.Get("X-User-ID")
	if callerType == "agent" {
		callerID = r.Header.Get("X-Agent-ID")
	}
	if !s.isChannelPrivileged(r.Context(), channelID, callerType, callerID) {
		writeError(w, http.StatusForbidden, "only admins, owners or the channel creator can change channel settings")
		return
	}

	var req ChannelPermissions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validRenamePolicy(req.RenamePolicy) || !validAddMembersPolicy(req.AddMembersPolicy) {
		writeError(w, http.StatusBadRequest, "invalid policy value")
		return
	}
	if err := s.SetPermissionsRow(r.Context(), channelID, req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save channel settings")
		return
	}
	writeJSON(w, http.StatusOK, req)
}
