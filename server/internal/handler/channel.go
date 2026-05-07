package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Channels are issues with kind IN ('channel', 'dm'). They reuse the issue
// table so they inherit comments, mentions, agent dispatch, subscribers,
// inbox, push notifications, attachments, and reactions for free. The
// difference is purely in how the UI presents them — title is the channel
// name, description is the topic, subscribers are participants, comments
// are messages.

// ChannelKindChannel is a multi-party named channel.
const ChannelKindChannel = "channel"

// ChannelKindDM is a 1:1 direct message between two members.
const ChannelKindDM = "dm"

// ChannelMember is one participant in a channel — a workspace member or agent.
type ChannelMember struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id"`
}

// ChannelResponse is the JSON payload for a channel. It mirrors a subset of
// IssueResponse — the fields that mean something for chat — and adds the
// participant list and unread count.
type ChannelResponse struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	Number       int32           `json:"number"`
	Identifier   string          `json:"identifier"`
	Kind         string          `json:"kind"`
	Title        string          `json:"title"`
	Description  *string         `json:"description"`
	Status       string          `json:"status"`
	ProjectID    *string         `json:"project_id"`
	AssigneeType *string         `json:"assignee_type"`
	AssigneeID   *string         `json:"assignee_id"`
	CreatorType  string          `json:"creator_type"`
	CreatorID    string          `json:"creator_id"`
	Participants []ChannelMember `json:"participants"`
	UnreadCount  int64           `json:"unread_count"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// CreateChannelRequest is the body for POST /api/channels. For kind='dm'
// the server expects exactly one peer in MemberIDs and ignores Name (it's
// derived from participants client-side). For kind='channel' the server
// requires Name and accepts any number of members and agents.
type CreateChannelRequest struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ProjectID   *string  `json:"project_id"`
	MemberIDs   []string `json:"member_ids"`
	AgentIDs    []string `json:"agent_ids"`
}

// CreateChannel handles POST /api/channels. It is idempotent for DMs:
// reopening a DM with the same peer returns the existing channel.
func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var req CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Kind != ChannelKindChannel && req.Kind != ChannelKindDM {
		writeError(w, http.StatusBadRequest, "kind must be 'channel' or 'dm'")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	if req.Kind == ChannelKindDM {
		if len(req.MemberIDs) != 1 || len(req.AgentIDs) > 0 {
			writeError(w, http.StatusBadRequest, "DM requires exactly one member peer and no agents")
			return
		}
		if req.MemberIDs[0] == userID {
			writeError(w, http.StatusBadRequest, "cannot DM yourself")
			return
		}
		// Idempotent open — return the existing DM if one already exists.
		existing, err := h.Queries.GetDMByMembers(r.Context(), db.GetDMByMembersParams{
			WorkspaceID: parseUUID(workspaceID),
			UserID:      parseUUID(userID),
			UserID_2:    parseUUID(req.MemberIDs[0]),
		})
		if err == nil {
			resp := h.channelToResponse(r.Context(), existing, userID)
			writeJSON(w, http.StatusOK, resp)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to look up existing DM")
			return
		}
	} else {
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required for channels")
			return
		}
	}

	// Verify private-agent access for every requested agent before any write.
	// This mirrors the gate on issue assignment and chat session creation.
	for _, aID := range req.AgentIDs {
		if ok, msg := h.canAssignAgent(r.Context(), r, aID, workspaceID); !ok {
			writeError(w, http.StatusForbidden, msg)
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	number, err := qtx.IncrementIssueCounter(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("increment issue counter failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	creatorType, actualCreatorID := h.resolveActor(r, userID, workspaceID)

	var description pgtype.Text
	if req.Description != "" {
		description = pgtype.Text{String: req.Description, Valid: true}
	}
	var projectID pgtype.UUID
	if req.ProjectID != nil && *req.ProjectID != "" {
		projectID = parseUUID(*req.ProjectID)
	}

	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID: parseUUID(workspaceID),
		Title:       req.Name,
		Description: description,
		Status:      "todo", // 'todo' is "active" for channels; archive uses 'cancelled'.
		Priority:    "none",
		CreatorType: creatorType,
		CreatorID:   parseUUID(actualCreatorID),
		Position:    0,
		Number:      number,
		ProjectID:   projectID,
		Kind:        pgtype.Text{String: req.Kind, Valid: true},
	})
	if err != nil {
		slog.Warn("create channel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create channel: "+err.Error())
		return
	}

	// Subscribe the creator first, then every requested participant. Reason
	// is 'creator' for the creator and 'manual' for explicitly added
	// participants; the subscriber listener uses these to build the
	// notification audience and de-dupe.
	if err := qtx.AddIssueSubscriber(r.Context(), db.AddIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: "member",
		UserID:   parseUUID(userID),
		Reason:   "creator",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe creator")
		return
	}
	for _, mID := range req.MemberIDs {
		if mID == userID {
			continue
		}
		if err := qtx.AddIssueSubscriber(r.Context(), db.AddIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: "member",
			UserID:   parseUUID(mID),
			Reason:   "manual",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to subscribe member")
			return
		}
	}
	for _, aID := range req.AgentIDs {
		if err := qtx.AddIssueSubscriber(r.Context(), db.AddIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: "agent",
			UserID:   parseUUID(aID),
			Reason:   "manual",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to subscribe agent")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	resp := h.channelToResponse(r.Context(), issue, userID)

	// Realtime: broadcast channel:created to every participant. Reuses the
	// issue audience helper since channels live in the issue table — we
	// further restrict to the participant set so non-members don't see it.
	h.publishToAudience(protocol.EventChannelCreated, workspaceID, creatorType, actualCreatorID, map[string]any{
		"channel": resp,
	}, h.audienceForChannel(resp))

	slog.Info("channel created", append(logger.RequestAttrs(r), "channel_id", uuidToString(issue.ID), "kind", req.Kind, "workspace_id", workspaceID)...)
	writeJSON(w, http.StatusCreated, resp)
}

// ListChannels handles GET /api/channels. Returns every channel/DM the
// requesting member is a subscriber of.
func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	rows, err := h.Queries.ListChannelsForUser(r.Context(), db.ListChannelsForUserParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), parseUUID(workspaceID))
	resp := make([]ChannelResponse, 0, len(rows))
	for _, row := range rows {
		channel := ChannelResponse{
			ID:           uuidToString(row.ID),
			WorkspaceID:  uuidToString(row.WorkspaceID),
			Number:       row.Number,
			Identifier:   prefix + "-" + strconv.Itoa(int(row.Number)),
			Kind:         row.Kind,
			Title:        row.Title,
			Description:  textToPtr(row.Description),
			Status:       row.Status,
			ProjectID:    uuidToPtr(row.ProjectID),
			AssigneeType: textToPtr(row.AssigneeType),
			AssigneeID:   uuidToPtr(row.AssigneeID),
			CreatorType:  row.CreatorType,
			CreatorID:    uuidToString(row.CreatorID),
			Participants: h.loadParticipants(r.Context(), row.ID),
			UnreadCount:  h.unreadInboxCount(r.Context(), userID, row.ID),
			CreatedAt:    timestampToString(row.CreatedAt),
			UpdatedAt:    timestampToString(row.UpdatedAt),
		}
		resp = append(resp, channel)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetChannel handles GET /api/channels/{id}. Refuses if the issue is not a
// channel/DM, or if the requester is not a participant.
func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	id := chi.URLParam(r, "id")

	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if issue.Kind != ChannelKindChannel && issue.Kind != ChannelKindDM {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	subscribed, err := h.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: "member",
		UserID:   parseUUID(userID),
	})
	if err != nil || !subscribed {
		writeError(w, http.StatusForbidden, "not a participant of this channel")
		return
	}

	writeJSON(w, http.StatusOK, h.channelToResponse(r.Context(), issue, userID))
}

// MarkChannelRead handles POST /api/channels/{id}/read. Marks every unread
// inbox_item for this channel/DM and the calling user as read in one shot —
// across BOTH inbox- and notifications-routed rows. CountUnreadInboxForChannel
// sums across routes, so missing the notifications side leaves the channel
// list stuck in "unread". Broadcasts inbox:batch-read so other tabs of the
// same user clear their badge without a refetch.
func (h *Handler) MarkChannelRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	id := chi.URLParam(r, "id")

	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if issue.Kind != ChannelKindChannel && issue.Kind != ChannelKindDM {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	subscribed, err := h.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: "member",
		UserID:   parseUUID(userID),
	})
	if err != nil || !subscribed {
		writeError(w, http.StatusForbidden, "not a participant of this channel")
		return
	}

	count, err := h.Queries.MarkInboxReadByIssue(r.Context(), db.MarkInboxReadByIssueParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
		IssueID:       issue.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark channel read")
		return
	}

	slog.Info("channel: mark read", append(logger.RequestAttrs(r), "user_id", userID, "channel_id", id, "count", count)...)
	h.publish(protocol.EventInboxBatchRead, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"issue_id":     id,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

// channelToResponse converts a full Issue row into a ChannelResponse.
// Only safe to call once the caller has verified the issue's kind.
func (h *Handler) channelToResponse(ctx context.Context, i db.Issue, viewerUserID string) ChannelResponse {
	prefix := h.getIssuePrefix(ctx, i.WorkspaceID)
	return ChannelResponse{
		ID:           uuidToString(i.ID),
		WorkspaceID:  uuidToString(i.WorkspaceID),
		Number:       i.Number,
		Identifier:   prefix + "-" + strconv.Itoa(int(i.Number)),
		Kind:         i.Kind,
		Title:        i.Title,
		Description:  textToPtr(i.Description),
		Status:       i.Status,
		ProjectID:    uuidToPtr(i.ProjectID),
		AssigneeType: textToPtr(i.AssigneeType),
		AssigneeID:   uuidToPtr(i.AssigneeID),
		CreatorType:  i.CreatorType,
		CreatorID:    uuidToString(i.CreatorID),
		Participants: h.loadParticipants(ctx, i.ID),
		UnreadCount:  h.unreadInboxCount(ctx, viewerUserID, i.ID),
		CreatedAt:    timestampToString(i.CreatedAt),
		UpdatedAt:    timestampToString(i.UpdatedAt),
	}
}

func (h *Handler) loadParticipants(ctx context.Context, issueID pgtype.UUID) []ChannelMember {
	subs, err := h.Queries.ListIssueSubscribers(ctx, issueID)
	if err != nil {
		return []ChannelMember{}
	}
	out := make([]ChannelMember, 0, len(subs))
	for _, s := range subs {
		out = append(out, ChannelMember{
			UserType: s.UserType,
			UserID:   uuidToString(s.UserID),
		})
	}
	return out
}

func (h *Handler) unreadInboxCount(ctx context.Context, userID string, issueID pgtype.UUID) int64 {
	count, err := h.Queries.CountUnreadInboxForChannel(ctx, db.CountUnreadInboxForChannelParams{
		RecipientID: parseUUID(userID),
		IssueID:     issueID,
	})
	if err != nil {
		return 0
	}
	return count
}

// audienceForChannel returns the WS audience for events on a channel —
// exactly the participant set, so non-members never see channel events.
func (h *Handler) audienceForChannel(c ChannelResponse) []string {
	out := make([]string, 0, len(c.Participants))
	for _, p := range c.Participants {
		if p.UserType == "member" {
			out = append(out, "member:"+p.UserID)
		}
	}
	return out
}
