// Package access — autopilot scope visibility & validation (JEH-724).
//
// Autopilots can be created with one of three scopes:
//
//	workspace — visible to every workspace member (legacy default).
//	personal  — visible to owner_user_id only.
//	group     — visible to members of group_id (resolved via cerebro_group_member,
//	            which lands with JEH-721) and to workspace owner/admin.
//
// This file does NOT depend on cerebro_group_member directly: the caller passes
// in the resolved group-id list. That keeps autopilot scope landing ahead of
// JEH-721 and decouples the two features at the type-check level.
package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Scope values mirror the CHECK constraint on autopilot.scope.
const (
	ScopeWorkspace = "workspace"
	ScopePersonal  = "personal"
	ScopeGroup     = "group"
)

// Errors surfaced by ValidateScope so callers can return 4xx with the right code.
var (
	ErrInvalidScope       = errors.New("scope must be workspace, personal, or group")
	ErrPersonalNeedsOwner = errors.New("personal scope requires owner_user_id")
	ErrGroupNeedsGroupID  = errors.New("group scope requires group_id")
	ErrWorkspaceExtraID   = errors.New("workspace scope must not set owner_user_id or group_id")
	ErrPersonalExtraGroup = errors.New("personal scope must not set group_id")
	ErrGroupExtraOwner    = errors.New("group scope must not set owner_user_id")
)

// ValidateScope mirrors the autopilot_scope_consistency_check CHECK constraint
// on the database side. Returning an error before the DB write gives callers a
// clean 400 response instead of a constraint-violation 500.
//
// scope is required; ownerUserID and groupID are pgtype.UUIDs that may be
// invalid (.Valid == false) when the request omits them.
func ValidateScope(scope string, ownerUserID, groupID pgtype.UUID) error {
	switch scope {
	case ScopeWorkspace:
		if ownerUserID.Valid || groupID.Valid {
			return ErrWorkspaceExtraID
		}
		return nil
	case ScopePersonal:
		if !ownerUserID.Valid {
			return ErrPersonalNeedsOwner
		}
		if groupID.Valid {
			return ErrPersonalExtraGroup
		}
		return nil
	case ScopeGroup:
		if !groupID.Valid {
			return ErrGroupNeedsGroupID
		}
		if ownerUserID.Valid {
			return ErrGroupExtraOwner
		}
		return nil
	default:
		return fmt.Errorf("%w: got %q", ErrInvalidScope, scope)
	}
}

// AutopilotView is the minimal autopilot shape needed for visibility checks.
// It mirrors the columns added by 9015_cerebro_autopilot_scope so the caller
// can populate it directly from a db.Autopilot row.
type AutopilotView struct {
	ID            pgtype.UUID
	WorkspaceID   pgtype.UUID
	IsPrivate     bool
	CreatedByType string
	CreatedByID   pgtype.UUID
	Scope         string
	OwnerUserID   pgtype.UUID // valid when scope == personal
	GroupID       pgtype.UUID // valid when scope == group
}

// ViewOf is a convenience converter from a sqlc-generated row.
func ViewOf(a db.Autopilot) AutopilotView {
	return AutopilotView{
		ID:            a.ID,
		WorkspaceID:   a.WorkspaceID,
		IsPrivate:     a.IsPrivate,
		CreatedByType: a.CreatedByType,
		CreatedByID:   a.CreatedByID,
		Scope:         a.Scope,
		OwnerUserID:   a.OwnerUserID,
		GroupID:       a.GroupID,
	}
}

// Viewer is the user attempting an autopilot operation. Group memberships are
// resolved by the caller (via cerebro_group_member, owned by JEH-721) and
// passed in so this package does not depend on that table existing.
type Viewer struct {
	UserID   pgtype.UUID
	IsAdmin  bool          // workspace owner/admin override
	GroupIDs []pgtype.UUID // groups the viewer belongs to in this workspace
}

// CanSee returns true when the viewer is allowed to see the autopilot.
//
//	workspace → everyone
//	personal  → owner only (admins do NOT see personal autopilots — see plan note)
//	group     → group members + workspace admins
func CanSee(view AutopilotView, viewer Viewer) bool {
	if view.IsPrivate {
		return view.CreatedByType == "member" && uuidEqual(view.CreatedByID, viewer.UserID)
	}
	switch view.Scope {
	case ScopeWorkspace, "": // empty == legacy rows where the column hasn't been backfilled
		return true
	case ScopePersonal:
		return uuidEqual(view.OwnerUserID, viewer.UserID)
	case ScopeGroup:
		if viewer.IsAdmin {
			return true
		}
		for _, gid := range viewer.GroupIDs {
			if uuidEqual(view.GroupID, gid) {
				return true
			}
		}
		return false
	default:
		// Unknown scope value — fail closed.
		return false
	}
}

// CanEdit gates Update / Delete. Plan §Visibility-regler:
//
//	workspace → admin/owner + creator (here: admin only; creator-check is layered
//	            in the handler since it has the row's CreatedByID).
//	personal  → owner only.
//	group     → group members (until group roles land — JEH-721 follow-up —
//	            members can edit) + workspace admins.
func CanEdit(view AutopilotView, viewer Viewer) bool {
	if view.IsPrivate {
		return view.CreatedByType == "member" && uuidEqual(view.CreatedByID, viewer.UserID)
	}
	switch view.Scope {
	case ScopeWorkspace, "":
		return viewer.IsAdmin
	case ScopePersonal:
		return uuidEqual(view.OwnerUserID, viewer.UserID)
	case ScopeGroup:
		if viewer.IsAdmin {
			return true
		}
		for _, gid := range viewer.GroupIDs {
			if uuidEqual(view.GroupID, gid) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// CanTrigger gates manual TriggerAutopilot calls. Plan §Visibility-regler:
//
//	workspace → all members.
//	personal  → owner only.
//	group     → members.
func CanTrigger(view AutopilotView, viewer Viewer) bool {
	if view.IsPrivate {
		return view.CreatedByType == "member" && uuidEqual(view.CreatedByID, viewer.UserID)
	}
	switch view.Scope {
	case ScopeWorkspace, "":
		return true
	case ScopePersonal:
		return uuidEqual(view.OwnerUserID, viewer.UserID)
	case ScopeGroup:
		if viewer.IsAdmin {
			return true
		}
		for _, gid := range viewer.GroupIDs {
			if uuidEqual(view.GroupID, gid) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// ListForUser executes the scope-aware list query, returning [] when no rows
// match. Wraps db.Queries.ListAutopilotsForUser so the call site in the
// upstream handler is a single line.
func ListForUser(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, viewer Viewer, status pgtype.Text) ([]db.Autopilot, error) {
	groups := viewer.GroupIDs
	if groups == nil {
		groups = []pgtype.UUID{}
	}
	return q.ListAutopilotsForUser(ctx, db.ListAutopilotsForUserParams{
		WorkspaceID:  workspaceID,
		UserID:       viewer.UserID,
		UserGroupIds: groups,
		IsAdmin:      viewer.IsAdmin,
		Status:       status,
	})
}

// SetScope writes the scope columns immediately after CreateAutopilot. Kept as
// an UPDATE so the upstream CreateAutopilot signature stays untouched.
func SetScope(ctx context.Context, q *db.Queries, autopilotID pgtype.UUID, scope string, ownerUserID, groupID pgtype.UUID) error {
	if scope == "" || scope == ScopeWorkspace {
		// Workspace scope — defaults already applied by the migration; skip.
		return nil
	}
	return q.SetAutopilotScope(ctx, db.SetAutopilotScopeParams{
		ID:          autopilotID,
		Scope:       scope,
		OwnerUserID: ownerUserID,
		GroupID:     groupID,
	})
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}
