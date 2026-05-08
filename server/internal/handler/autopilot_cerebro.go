// CEREBRO-PATCH(autopilot-cerebro): scope visibility helpers for the upstream
// autopilot handler (JEH-724). Net-new fork file in upstream-zone path; the
// only inline edits in autopilot.go are tiny call-site swaps that delegate
// the cerebro-specific logic here.
package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/access"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cerebroAutopilotViewer assembles an access.Viewer for the request user.
//
// GroupIDs stays empty until JEH-721 lands cerebro_group_member and a
// `ListGroupsForUser` query — until then group-scoped autopilots are visible
// only to workspace owners/admins (CanSee returns true via IsAdmin path).
func (h *Handler) cerebroAutopilotViewer(w http.ResponseWriter, r *http.Request, workspaceID string) (access.Viewer, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return access.Viewer{}, false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusForbidden, "not a workspace member")
		return access.Viewer{}, false
	}
	userUUID, perr := util.ParseUUID(userID)
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return access.Viewer{}, false
	}
	return access.Viewer{
		UserID:   userUUID,
		IsAdmin:  isWorkspaceAdmin(member),
		GroupIDs: nil, // JEH-721 follow-up: populate from cerebro_group_member.
	}, true
}

// cerebroAutopilotVisible enforces CanSee. Returns 404 (not 403) when invisible
// so probing for existence requires real access — same pattern as project
// access.
func (h *Handler) cerebroAutopilotVisible(w http.ResponseWriter, r *http.Request, workspaceID string, autopilot db.Autopilot) bool {
	viewer, ok := h.cerebroAutopilotViewer(w, r, workspaceID)
	if !ok {
		return false
	}
	if !access.CanSee(access.ViewOf(autopilot), viewer) {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return false
	}
	return true
}

// cerebroAutopilotEditable enforces CanEdit. Returns 403 since the caller has
// already passed loadAutopilotInWorkspace (the row exists in their workspace).
func (h *Handler) cerebroAutopilotEditable(w http.ResponseWriter, r *http.Request, workspaceID string, autopilot db.Autopilot) bool {
	viewer, ok := h.cerebroAutopilotViewer(w, r, workspaceID)
	if !ok {
		return false
	}
	view := access.ViewOf(autopilot)
	if !access.CanSee(view, viewer) {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return false
	}
	if !access.CanEdit(view, viewer) {
		writeError(w, http.StatusForbidden, "you cannot edit this autopilot")
		return false
	}
	return true
}

// cerebroAutopilotTriggerable enforces CanTrigger.
func (h *Handler) cerebroAutopilotTriggerable(w http.ResponseWriter, r *http.Request, workspaceID string, autopilot db.Autopilot) bool {
	viewer, ok := h.cerebroAutopilotViewer(w, r, workspaceID)
	if !ok {
		return false
	}
	view := access.ViewOf(autopilot)
	if !access.CanSee(view, viewer) {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return false
	}
	if !access.CanTrigger(view, viewer) {
		writeError(w, http.StatusForbidden, "you cannot trigger this autopilot")
		return false
	}
	return true
}

// cerebroApplyScopeOnCreate validates and persists the scope columns set on
// the request. Returns the row reloaded with the scope columns populated, plus
// ok=false when the request is rejected (an HTTP error has already been written).
//
// Personal scope auto-fills owner_user_id from the requester when not provided
// in the body — common case ("create a personal autopilot for me").
func (h *Handler) cerebroApplyScopeOnCreate(w http.ResponseWriter, r *http.Request, autopilot db.Autopilot, req *CreateAutopilotRequest, requesterUserID pgtype.UUID) (db.Autopilot, bool) {
	scope := access.ScopeWorkspace
	if req.Scope != nil && *req.Scope != "" {
		scope = *req.Scope
	}
	if scope == access.ScopeWorkspace {
		// Defaults already applied — the autopilot row from CreateAutopilot is correct.
		return autopilot, true
	}

	var ownerUUID, groupUUID pgtype.UUID
	if req.OwnerUserID != nil && *req.OwnerUserID != "" {
		u, ok := parseUUIDOrBadRequest(w, *req.OwnerUserID, "owner_user_id")
		if !ok {
			return autopilot, false
		}
		ownerUUID = u
	} else if scope == access.ScopePersonal {
		ownerUUID = requesterUserID
	}
	if req.GroupID != nil && *req.GroupID != "" {
		u, ok := parseUUIDOrBadRequest(w, *req.GroupID, "group_id")
		if !ok {
			return autopilot, false
		}
		groupUUID = u
	}

	if err := access.ValidateScope(scope, ownerUUID, groupUUID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return autopilot, false
	}

	if err := access.SetScope(r.Context(), h.Queries, autopilot.ID, scope, ownerUUID, groupUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply autopilot scope")
		return autopilot, false
	}

	updated, err := h.Queries.GetAutopilot(r.Context(), autopilot.ID)
	if err != nil {
		// Edge case: SetScope succeeded but the row vanished (concurrent delete).
		writeError(w, http.StatusInternalServerError, "autopilot vanished after scope update")
		return autopilot, false
	}
	return updated, true
}
