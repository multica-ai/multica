package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// BriefPushPreference is a member's choice to receive the brief in a chat app
// (spec 2.7). The same shape is read and written. binding_id names one of the
// member's chat bindings in this workspace. quiet_hours is a window in the
// member's timezone; null means the push may arrive at any hour.
type BriefPushPreference struct {
	BriefPush  bool                 `json:"brief_push"`
	BindingID  *string              `json:"binding_id"`
	QuietHours *BriefPushQuietHours `json:"quiet_hours"`
}

// BriefPushQuietHours is the window [start, end) in hours 0 to 23. It wraps
// midnight when start is after end.
type BriefPushQuietHours struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func briefPushPreferenceToResponse(p db.BriefPushPreference) BriefPushPreference {
	out := BriefPushPreference{BriefPush: p.BriefPush}
	if p.BindingID.Valid {
		id := uuidToString(p.BindingID)
		out.BindingID = &id
	}
	if p.QuietHoursStart.Valid && p.QuietHoursEnd.Valid {
		out.QuietHours = &BriefPushQuietHours{Start: int(p.QuietHoursStart.Int16), End: int(p.QuietHoursEnd.Int16)}
	}
	return out
}

// briefPushMember resolves the caller as the member whose preference is read or
// written. The preference is the caller's own: no request names another member,
// and an agent has no brief preference at all.
func (h *Handler) briefPushMember(w http.ResponseWriter, r *http.Request) (wsUUID, userUUID pgtype.UUID, ok bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "brief push belongs to a member; an agent cannot set it")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	wsUUID, ok = parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	userUUID, ok = parseUUIDOrBadRequest(w, userID, "user id")
	return wsUUID, userUUID, ok
}

// GetBriefPushPreference returns the caller's brief push preference. A member
// who has never set one gets the default: off, with no chat and no quiet hours.
//
//	GET /api/office/brief-push
func (h *Handler) GetBriefPushPreference(w http.ResponseWriter, r *http.Request) {
	wsUUID, userUUID, ok := h.briefPushMember(w, r)
	if !ok {
		return
	}
	pref, err := h.Queries.GetBriefPushPreference(r.Context(), db.GetBriefPushPreferenceParams{
		WorkspaceID: wsUUID, MemberID: userUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, BriefPushPreference{})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load brief push preference")
		return
	}
	writeJSON(w, http.StatusOK, briefPushPreferenceToResponse(pref))
}

// UpdateBriefPushPreference replaces the caller's brief push preference. The
// body carries all three fields; an omitted one is cleared.
//
//	PUT /api/office/brief-push {brief_push, binding_id?, quiet_hours?}
func (h *Handler) UpdateBriefPushPreference(w http.ResponseWriter, r *http.Request) {
	wsUUID, userUUID, ok := h.briefPushMember(w, r)
	if !ok {
		return
	}
	var req BriefPushPreference
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpsertBriefPushPreferenceParams{
		ID:          dbid.NewV7(),
		WorkspaceID: wsUUID,
		MemberID:    userUUID,
		BriefPush:   req.BriefPush,
	}

	if req.QuietHours != nil {
		start, end := req.QuietHours.Start, req.QuietHours.End
		if start < 0 || start > 23 || end < 0 || end > 23 {
			writeError(w, http.StatusBadRequest, "quiet_hours start and end are hours from 0 to 23")
			return
		}
		if start == end {
			writeError(w, http.StatusBadRequest, "quiet_hours needs two different hours; leave it off to allow briefs at any time")
			return
		}
		params.QuietHoursStart = pgtype.Int2{Int16: int16(start), Valid: true}
		params.QuietHoursEnd = pgtype.Int2{Int16: int16(end), Valid: true}
	}

	if req.BindingID != nil {
		bindingUUID, ok := parseUUIDOrBadRequest(w, *req.BindingID, "binding_id")
		if !ok {
			return
		}
		// The binding must be this member's own, in this workspace. The same
		// refusal covers an unknown id, another workspace's binding and another
		// member's binding, so the response does not reveal which it was.
		const notYours = "binding_id is not a chat linked to your account in this workspace"
		binding, err := h.Queries.GetChannelUserBindingInWorkspace(r.Context(), db.GetChannelUserBindingInWorkspaceParams{
			ID: bindingUUID, WorkspaceID: wsUUID,
		})
		if err != nil || uuidToString(binding.MulticaUserID) != uuidToString(userUUID) {
			writeError(w, http.StatusBadRequest, notYours)
			return
		}
		if !h.BriefPush.ChatAppSupported(channel.Type(binding.ChannelType)) {
			writeError(w, http.StatusBadRequest, "that chat app cannot receive briefs yet")
			return
		}
		params.BindingID = bindingUUID
	}

	if req.BriefPush && !params.BindingID.Valid {
		writeError(w, http.StatusBadRequest, "choose the chat that briefs go to before turning brief push on")
		return
	}

	pref, err := h.Queries.UpsertBriefPushPreference(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save brief push preference")
		return
	}
	writeJSON(w, http.StatusOK, briefPushPreferenceToResponse(pref))
}
