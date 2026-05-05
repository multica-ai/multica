package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ChannelResponse is the JSON shape returned for channels and DMs.
type ChannelResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	Name           string  `json:"name"`
	DisplayName    string  `json:"display_name"`
	Description    string  `json:"description"`
	Kind           string  `json:"kind"`
	Visibility     string  `json:"visibility"`
	CreatedByType  string  `json:"created_by_type"`
	CreatedByID    string  `json:"created_by_id"`
	RetentionDays  *int32  `json:"retention_days"`
	ArchivedAt     *string `json:"archived_at"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	// Per-actor unread state. Only populated by ListChannels (the sidebar
	// uses these for the badge + first-unread anchor). GetChannel and
	// other endpoints leave them zero/null since they're cheap there.
	UnreadCount       int64   `json:"unread_count"`
	LastReadMessageID *string `json:"last_read_message_id"`
}

func channelToResponse(c db.Channel) ChannelResponse {
	resp := ChannelResponse{
		ID:            uuidToString(c.ID),
		WorkspaceID:   uuidToString(c.WorkspaceID),
		Name:          c.Name,
		DisplayName:   c.DisplayName,
		Description:   c.Description,
		Kind:          c.Kind,
		Visibility:    c.Visibility,
		CreatedByType: c.CreatedByType,
		CreatedByID:   uuidToString(c.CreatedByID),
		CreatedAt:     timestampToString(c.CreatedAt),
		UpdatedAt:     timestampToString(c.UpdatedAt),
	}
	if c.RetentionDays.Valid {
		v := c.RetentionDays.Int32
		resp.RetentionDays = &v
	}
	if c.ArchivedAt.Valid {
		s := timestampToString(c.ArchivedAt)
		resp.ArchivedAt = &s
	}
	return resp
}

// ChannelMembershipResponse is the JSON shape for memberships.
type ChannelMembershipResponse struct {
	ChannelID         string  `json:"channel_id"`
	MemberType        string  `json:"member_type"`
	MemberID          string  `json:"member_id"`
	Role              string  `json:"role"`
	NotificationLevel string  `json:"notification_level"`
	JoinedAt          string  `json:"joined_at"`
	LastReadMessageID *string `json:"last_read_message_id"`
	LastReadAt        *string `json:"last_read_at"`
}

func membershipToResponse(m db.ChannelMembership) ChannelMembershipResponse {
	resp := ChannelMembershipResponse{
		ChannelID:         uuidToString(m.ChannelID),
		MemberType:        m.MemberType,
		MemberID:          uuidToString(m.MemberID),
		Role:              m.Role,
		NotificationLevel: m.NotificationLevel,
		JoinedAt:          timestampToString(m.JoinedAt),
	}
	if m.LastReadMessageID.Valid {
		s := uuidToString(m.LastReadMessageID)
		resp.LastReadMessageID = &s
	}
	if m.LastReadAt.Valid {
		s := timestampToString(m.LastReadAt)
		resp.LastReadAt = &s
	}
	return resp
}

// requireChannelsEnabled gates every Channels endpoint behind the
// workspace.channels_enabled flag, returning 404 when off so the surface
// is invisible to anyone in a workspace that hasn't opted in. Returns the
// workspace UUID and the resolved workspace row on success; writes the
// error response and returns ok=false on failure.
func (h *Handler) requireChannelsEnabled(w http.ResponseWriter, r *http.Request) (pgtype.UUID, db.Workspace, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return pgtype.UUID{}, db.Workspace{}, false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return pgtype.UUID{}, db.Workspace{}, false
	}
	if !ws.ChannelsEnabled {
		writeError(w, http.StatusNotFound, "not found")
		return pgtype.UUID{}, db.Workspace{}, false
	}
	return wsID, ws, true
}

// channelErrorStatus maps the service's typed errors to HTTP status codes.
// Unknown errors fall through to 500 — callers should log the error too.
func channelErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, channel.ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, channel.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, channel.ErrConflict):
		return http.StatusConflict, "name already taken in this workspace"
	case errors.Is(err, channel.ErrInvalid):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, channel.ErrChannelClosed):
		return http.StatusGone, "channel archived"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

// requireChannelAccess loads a channel by id, verifies it belongs to the
// workspace, and verifies the actor can read it. Writes the appropriate
// error response and returns ok=false on failure.
func (h *Handler) requireChannelAccess(w http.ResponseWriter, r *http.Request, channelID, workspaceID pgtype.UUID, actor channel.Actor) (db.Channel, bool) {
	ch, err := h.ChannelService.GetInWorkspace(r.Context(), channelID, workspaceID)
	if err != nil {
		status, msg := channelErrorStatus(err)
		writeError(w, status, msg)
		return db.Channel{}, false
	}
	ok, err := h.ChannelService.CanActorAccess(r.Context(), channelID, workspaceID, actor)
	if err != nil {
		status, msg := channelErrorStatus(err)
		writeError(w, status, msg)
		return db.Channel{}, false
	}
	if !ok {
		// 404, not 403 — never leak the existence of a private channel to
		// a non-member.
		writeError(w, http.StatusNotFound, "not found")
		return db.Channel{}, false
	}
	return ch, true
}

// CreateChannelRequest is the JSON body for POST /api/channels.
type CreateChannelRequest struct {
	Name          string  `json:"name"`
	DisplayName   string  `json:"display_name"`
	Description   string  `json:"description"`
	Visibility    string  `json:"visibility"`     // "public" | "private"
	RetentionDays *int32  `json:"retention_days"` // optional override
}

// CreateChannel handles POST /api/channels.
func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Visibility == "" {
		req.Visibility = channel.VisibilityPublic
	}
	authorType, authorID := h.resolveActor(r, userID, uuidToString(wsID))
	authorUUID, ok := parseUUIDOrBadRequest(w, authorID, "actor id")
	if !ok {
		return
	}

	ch, err := h.ChannelService.Create(r.Context(), channel.CreateChannelParams{
		WorkspaceID: wsID,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Kind:        channel.KindChannel,
		Visibility:  req.Visibility,
		CreatedBy:   channel.Actor{Type: authorType, ID: authorUUID},
		RetentionDays: req.RetentionDays,
	})
	if err != nil {
		status, msg := channelErrorStatus(err)
		writeError(w, status, msg)
		return
	}

	resp := channelToResponse(ch)
	h.publish(protocol.EventChannelCreated, uuidToString(wsID), authorType, authorID, map[string]any{
		"channel": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// ListChannels handles GET /api/channels.
func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}

	chs, err := h.ChannelService.ListForActor(r.Context(), wsID, channel.Actor{Type: actorType, ID: actorUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	// Batch-fetch unread counts for the actor's memberships in one query
	// rather than N+1. Best-effort: a query failure renders the list with
	// zeroed counts so the page doesn't 500 over a sidebar-only feature.
	unreadByChannel := map[string]struct {
		count    int64
		lastRead pgtype.UUID
	}{}
	if rows, err := h.Queries.ListChannelUnreadCountsForActor(r.Context(), db.ListChannelUnreadCountsForActorParams{
		MemberType: actorType,
		MemberID:   actorUUID,
	}); err == nil {
		for _, row := range rows {
			unreadByChannel[uuidToString(row.ChannelID)] = struct {
				count    int64
				lastRead pgtype.UUID
			}{count: row.UnreadCount, lastRead: row.LastReadMessageID}
		}
	} else {
		slog.Warn("ListChannels: unread counts query failed", "error", err)
	}

	out := make([]ChannelResponse, len(chs))
	for i, c := range chs {
		resp := channelToResponse(c)
		if u, ok := unreadByChannel[resp.ID]; ok {
			resp.UnreadCount = u.count
			if u.lastRead.Valid {
				s := uuidToString(u.lastRead)
				resp.LastReadMessageID = &s
			}
		}
		out[i] = resp
	}
	writeJSON(w, http.StatusOK, out)
}

// GetChannel handles GET /api/channels/{channelId}.
func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	ch, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, channelToResponse(ch))
}

// UpdateChannelRequest is the JSON body for PATCH /api/channels/{channelId}.
// Pointer fields distinguish "not present" from "present and empty".
// retention_days_set=true with retention_days=null clears the override.
type UpdateChannelRequest struct {
	DisplayName      *string `json:"display_name"`
	Description      *string `json:"description"`
	Visibility       *string `json:"visibility"`
	RetentionDays    *int32  `json:"retention_days"`
	RetentionDaysSet bool    `json:"retention_days_set"`
}

// UpdateChannel handles PATCH /api/channels/{channelId}.
func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if _, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID}); !ok {
		return
	}

	var req UpdateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.ChannelService.Update(r.Context(), channelUUID, channel.UpdateChannelParams{
		DisplayName:      req.DisplayName,
		Description:      req.Description,
		Visibility:       req.Visibility,
		RetentionDays:    req.RetentionDays,
		RetentionDaysSet: req.RetentionDaysSet,
	})
	if err != nil {
		status, msg := channelErrorStatus(err)
		writeError(w, status, msg)
		return
	}

	resp := channelToResponse(updated)
	h.publish(protocol.EventChannelUpdated, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// ArchiveChannel handles DELETE /api/channels/{channelId}. The channel is
// soft-archived (rows preserved). Use Phase 5's hard-delete endpoint for
// purges if/when we add it.
func (h *Handler) ArchiveChannel(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if _, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID}); !ok {
		return
	}
	if err := h.ChannelService.Archive(r.Context(), channelUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive channel")
		return
	}
	h.publish(protocol.EventChannelArchived, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id": uuidToString(channelUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// AddMemberRequest is the JSON body for POST /api/channels/{channelId}/members.
type AddMemberRequest struct {
	MemberType        string  `json:"member_type"` // "member" | "agent"
	MemberID          string  `json:"member_id"`
	Role              string  `json:"role"`
	NotificationLevel string  `json:"notification_level"`
}

// AddChannelMember handles POST /api/channels/{channelId}/members.
func (h *Handler) AddChannelMember(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if _, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID}); !ok {
		return
	}

	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}
	addedBy := channel.Actor{Type: actorType, ID: actorUUID}
	mem, err := h.ChannelService.AddMember(r.Context(), channelUUID, channel.AddMemberParams{
		Member:            channel.Actor{Type: req.MemberType, ID: memberUUID},
		Role:              req.Role,
		AddedBy:           &addedBy,
		NotificationLevel: req.NotificationLevel,
	})
	if err != nil {
		status, msg := channelErrorStatus(err)
		writeError(w, status, msg)
		return
	}

	resp := membershipToResponse(mem)
	h.publish(protocol.EventChannelMemberAdded, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id": uuidToString(channelUUID),
		"membership": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// RemoveChannelMember handles DELETE /api/channels/{channelId}/members/{memberType}/{memberId}.
func (h *Handler) RemoveChannelMember(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	memberType := chi.URLParam(r, "memberType")
	memberUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if _, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID}); !ok {
		return
	}

	if err := h.ChannelService.RemoveMember(r.Context(), channelUUID, channel.Actor{Type: memberType, ID: memberUUID}); err != nil {
		status, msg := channelErrorStatus(err)
		writeError(w, status, msg)
		return
	}
	h.publish(protocol.EventChannelMemberRemoved, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id":   uuidToString(channelUUID),
		"member_type":  memberType,
		"member_id":    uuidToString(memberUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// ListChannelMembers handles GET /api/channels/{channelId}/members.
func (h *Handler) ListChannelMembers(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if _, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID}); !ok {
		return
	}

	mems, err := h.ChannelService.ListMembers(r.Context(), channelUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	out := make([]ChannelMembershipResponse, len(mems))
	for i, m := range mems {
		out[i] = membershipToResponse(m)
	}
	writeJSON(w, http.StatusOK, out)
}

// MarkReadRequest is the JSON body for POST /api/channels/{channelId}/read.
type MarkReadRequest struct {
	MessageID string `json:"message_id"`
}

// MarkChannelRead handles POST /api/channels/{channelId}/read for the
// calling actor. Body carries the latest message id seen by the actor.
//
// (Spec used `PATCH /channels/{channelId}/members/{type}/{memberId}/read` with
// path-positioned actor; we use the calling actor inferred from auth and a
// shorter URL because admin-marking-someone-else's-read isn't a Phase 1 use
// case and the URL gets unwieldy.)
func (h *Handler) MarkChannelRead(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if _, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID}); !ok {
		return
	}

	var req MarkReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	messageUUID, ok := parseUUIDOrBadRequest(w, req.MessageID, "message_id")
	if !ok {
		return
	}
	if err := h.ChannelService.MarkRead(r.Context(), channelUUID, channel.Actor{Type: actorType, ID: actorUUID}, messageUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}
	h.publish(protocol.EventChannelRead, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id":     uuidToString(channelUUID),
		"member_type":    actorType,
		"member_id":      actorID,
		"last_read_id":   uuidToString(messageUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// CreateOrFetchDMRequest is the JSON body for POST /api/dms.
type CreateOrFetchDMRequest struct {
	Participants []DMParticipant `json:"participants"`
}

// DMParticipant identifies a DM participant in CreateOrFetchDMRequest.
type DMParticipant struct {
	Type string `json:"type"` // "member" | "agent"
	ID   string `json:"id"`
}

// CreateOrFetchDM handles POST /api/dms. Idempotent on the participant set
// + workspace: repeated calls return the same DM channel.
func (h *Handler) CreateOrFetchDM(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}

	var req CreateOrFetchDMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Participants) == 0 {
		writeError(w, http.StatusBadRequest, "participants must not be empty")
		return
	}
	// The caller must be one of the participants. We don't allow opening a
	// DM between two other actors.
	caller := channel.Actor{Type: actorType, ID: actorUUID}
	parts := make([]channel.Actor, 0, len(req.Participants)+1)
	parts = append(parts, caller)
	callerInRequest := false
	for _, p := range req.Participants {
		uid, ok := parseUUIDOrBadRequest(w, p.ID, "participant id")
		if !ok {
			return
		}
		a := channel.Actor{Type: p.Type, ID: uid}
		if a.Equal(caller) {
			callerInRequest = true
			continue
		}
		parts = append(parts, a)
	}
	// callerInRequest is informational only — we accept the caller-only case
	// (1:1 self-DM) and the caller-prepended case equally; idempotency is on
	// the deduplicated participant set anyway.
	_ = callerInRequest

	dm, err := h.ChannelService.GetOrCreateDM(r.Context(), wsID, parts)
	if err != nil {
		status, msg := channelErrorStatus(err)
		writeError(w, status, msg)
		return
	}
	resp := channelToResponse(dm)
	// We always publish a created event; the UI can dedupe on channel id.
	h.publish(protocol.EventChannelCreated, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

