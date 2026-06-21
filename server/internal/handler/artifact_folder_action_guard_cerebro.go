package handler

// CEREBRO-PATCH(folder-action-guard): FIR-1590 follow-up — action permissions
// on folders. FIR-1590 shipped folder *visibility* (who can SEE a folder). This
// adds the first *capability* layer (who may ACT on a folder): delete, rename,
// and move. The guard is opt-in per folder owner via the per-user feature flag
// `cerebro_folder_action_guard`, so it can be rolled out to one user first
// ("kun for mig til en start") before any workspace-wide enablement.
//
// When the guard is ON for a folder's owner, only that owner and workspace
// owners/admins may delete/rename/move the folder. When OFF (the default), the
// legacy behavior is unchanged: any workspace member may act. Legacy folders
// with no owner are never guarded.

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// folderActionGuardFlagKey is the per-user / workspace feature-flag key that
// turns the folder action guard on for a folder's owner. Mirrored in the
// TypeScript feature-flag registry (packages/cerebro-feature-flags/registry.ts).
const folderActionGuardFlagKey = "cerebro_folder_action_guard"

// folderActionAllowed is the pure authorization predicate behind the guard. It
// is deliberately free of HTTP/DB concerns so it can be unit-tested in full.
//
//   - guardEnabled false  -> always allowed (legacy behavior, the default)
//   - ownerID empty       -> always allowed (legacy/unowned folder, no one to protect)
//   - callerIsAdmin       -> allowed (workspace owner/admin governance override)
//   - callerID == ownerID -> allowed (the owner acting on their own folder)
//   - otherwise           -> denied
func folderActionAllowed(ownerID string, guardEnabled bool, callerID string, callerIsAdmin bool) bool {
	if !guardEnabled {
		return true
	}
	if ownerID == "" {
		return true
	}
	if callerIsAdmin {
		return true
	}
	return callerID == ownerID
}

// folderActionGuardEnabledForOwner resolves the action-guard flag for a folder
// owner using the same precedence the rest of the cerebro fork uses for
// feature flags: locked workspace override > owner's personal override >
// unlocked workspace override > default (OFF). It never fails closed onto a
// stricter rule — any lookup error resolves to "not guarded" so a flag-system
// problem can never lock members out of folder operations they have today.
func (h *Handler) folderActionGuardEnabledForOwner(ctx context.Context, workspaceID, ownerID string) bool {
	if h.CerebroQueries == nil || ownerID == "" {
		return false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false
	}

	var wsEnabled, wsLocked, wsFound bool
	if wsRows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, wsUUID); err == nil {
		for _, row := range wsRows {
			if row.FlagKey == folderActionGuardFlagKey {
				wsEnabled, wsLocked, wsFound = row.Enabled, row.Locked, true
				break
			}
		}
	}
	if wsFound && wsLocked {
		return wsEnabled
	}

	ownerUUID, err := util.ParseUUID(ownerID)
	if err == nil {
		on, perr := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
			WorkspaceID: wsUUID,
			UserID:      ownerUUID,
			FlagKey:     folderActionGuardFlagKey,
		})
		if perr == nil {
			return on
		}
		if !errors.Is(perr, pgx.ErrNoRows) {
			return false
		}
	}

	if wsFound {
		return wsEnabled
	}
	return false
}

// requireFolderActionAllowed enforces the action guard for a destructive folder
// mutation (delete/rename/move). It returns true when the caller may proceed;
// otherwise it writes a 403 and returns false. member is the caller's resolved
// workspace membership (for the owner/admin governance override).
func (h *Handler) requireFolderActionAllowed(w http.ResponseWriter, r *http.Request, folder db.ArtifactFolder, callerID string, member db.Member) bool {
	ownerID := ""
	if folder.OwnerID.Valid {
		ownerID = uuidToString(folder.OwnerID)
	}
	guardEnabled := h.folderActionGuardEnabledForOwner(r.Context(), uuidToString(folder.WorkspaceID), ownerID)
	callerIsAdmin := roleAllowed(member.Role, "owner", "admin")
	if folderActionAllowed(ownerID, guardEnabled, callerID, callerIsAdmin) {
		return true
	}
	writeError(w, http.StatusForbidden, "only the folder owner or a workspace admin can delete, rename, or move this folder")
	return false
}
