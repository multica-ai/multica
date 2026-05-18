// CEREBRO-PATCH(autopilot-cerebro): scope visibility helpers for the upstream
// autopilot handler (JEH-724). Net-new fork file in upstream-zone path; the
// only inline edits in autopilot.go are tiny call-site swaps that delegate
// the cerebro-specific logic here.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/access"
	// CEREBRO-PATCH(autopilot-model-import): per-autopilot model override (JEH-1310).
	"github.com/multica-ai/multica/server/internal/cerebro/autopilotmodel"
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

// cerebroApplyModelOnCreate validates and persists the optional model column
// after CreateAutopilot. Same UPDATE-after-create pattern as cerebroApplyScopeOnCreate
// (JEH-1310). Returns the reloaded row when the override is applied; the
// untouched input row when the request omits the field.
func (h *Handler) cerebroApplyModelOnCreate(w http.ResponseWriter, r *http.Request, autopilot db.Autopilot, req *CreateAutopilotRequest) (db.Autopilot, bool) {
	if req.Model == nil {
		return autopilot, true
	}
	if err := autopilotmodel.SetOnAutopilot(r.Context(), h.Queries, autopilot.ID, *req.Model); err != nil {
		if errors.Is(err, autopilotmodel.ErrUnknownModel) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "failed to apply autopilot model")
		}
		return autopilot, false
	}
	updated, err := h.Queries.GetAutopilot(r.Context(), autopilot.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "autopilot vanished after model update")
		return autopilot, false
	}
	return updated, true
}

// cerebroApplyModelOnUpdate persists the model column when the PATCH body
// includes a "model" key (rawFields is the same map UpdateAutopilot already
// builds to distinguish present-but-null from absent). Sending {"model": null}
// or {"model": ""} clears the override; omitting the key leaves it alone.
func (h *Handler) cerebroApplyModelOnUpdate(w http.ResponseWriter, r *http.Request, autopilot db.Autopilot, req *UpdateAutopilotRequest, rawFields map[string]json.RawMessage) (db.Autopilot, bool) {
	if _, present := rawFields["model"]; !present {
		return autopilot, true
	}
	model := ""
	if req.Model != nil {
		model = *req.Model
	}
	if err := autopilotmodel.SetOnAutopilot(r.Context(), h.Queries, autopilot.ID, model); err != nil {
		if errors.Is(err, autopilotmodel.ErrUnknownModel) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "failed to apply autopilot model")
		}
		return autopilot, false
	}
	updated, err := h.Queries.GetAutopilot(r.Context(), autopilot.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "autopilot vanished after model update")
		return autopilot, false
	}
	return updated, true
}

// cerebroAuditAutopilotPrivacyChange writes a workspace-scoped activity row
// when the owner-only privacy flag changes. This keeps privacy flips visible in
// the existing audit feed without adding a parallel audit table.
func (h *Handler) cerebroAuditAutopilotPrivacyChange(r *http.Request, before, after db.Autopilot, actorType, actorID string) {
	if before.IsPrivate == after.IsPrivate {
		return
	}
	details, err := json.Marshal(map[string]any{
		"autopilot_id": uuidToString(after.ID),
		"title":        after.Title,
		"old_value":    before.IsPrivate,
		"new_value":    after.IsPrivate,
	})
	if err != nil {
		return
	}
	_, _ = h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: after.WorkspaceID,
		ActorType:   pgtype.Text{String: actorType, Valid: actorType != ""},
		ActorID:     parseUUID(actorID),
		Action:      "autopilot_privacy_changed",
		Details:     details,
	})
}
