package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// textMentionRe matches @Word tokens (word = non-whitespace sequence after @).
var textMentionRe = regexp.MustCompile(`@(\S+)`)

// parseTextMentions extracts unique @name strings from raw message text.
func parseTextMentions(content string) []string {
	matches := textMentionRe.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var names []string
	for _, m := range matches {
		name := strings.TrimRight(m[1], ".,!?;:")
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// ---------------------------------------------------------------------------
// Channels
// ---------------------------------------------------------------------------

type CreateChannelRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // public, private, dm
}

type ChannelResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	CreatedBy   string  `json:"created_by"`
	IsMember    bool    `json:"is_member"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func channelToResponse(c db.Channel, isMember bool) ChannelResponse {
	r := ChannelResponse{
		ID:          uuidToString(c.ID),
		WorkspaceID: uuidToString(c.WorkspaceID),
		Name:        c.Name,
		Description: c.Description,
		Type:        c.Type,
		CreatedBy:   uuidToString(c.CreatedBy),
		IsMember:    isMember,
		CreatedAt:   timestampToString(c.CreatedAt),
		UpdatedAt:   timestampToString(c.UpdatedAt),
	}
	if c.ArchivedAt.Valid {
		ts := timestampToString(c.ArchivedAt)
		r.ArchivedAt = &ts
	}
	return r
}

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	var req CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	channelType := "public"
	if req.Type != "" {
		channelType = req.Type
	}

	channel, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Description: req.Description,
		Type:        channelType,
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "channel name already exists")
			return
		}
		slog.Error("create channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// Creator is automatically a member-owner
	_, err = h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID:  channel.ID,
		MemberType: "user",
		MemberID:   parseUUID(userID),
		Role:       "owner",
	})
	if err != nil {
		slog.Warn("add channel creator as member failed", "error", err)
	}

	h.publish(protocol.EventChannelCreated, workspaceID, "member", userID, protocol.ChannelCreatedPayload{
		ChannelID: uuidToString(channel.ID),
	})

	writeJSON(w, http.StatusCreated, channelToResponse(channel, true))
}

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "member" {
		actorType = "user"
	}
	_ = actorType // used implicitly via MemberID lookup in query

	channels, err := h.Queries.ListChannelsByWorkspace(r.Context(), db.ListChannelsByWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		MemberID:    parseUUID(actorID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	resp := make([]ChannelResponse, len(channels))
	for i, c := range channels {
		resp[i] = channelToResponse(db.Channel{
			ID:          c.ID,
			WorkspaceID: c.WorkspaceID,
			Name:        c.Name,
			Description: c.Description,
			Type:        c.Type,
			CreatedBy:   c.CreatedBy,
			ArchivedAt:  c.ArchivedAt,
			CreatedAt:   c.CreatedAt,
			UpdatedAt:   c.UpdatedAt,
		}, c.IsMember)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "member" {
		actorType = "user"
	}
	if !h.canAccessChannel(w, r, channelID, actorID, actorType) {
		return
	}

	c, err := h.Queries.GetChannelInWorkspace(r.Context(), db.GetChannelInWorkspaceParams{
		ID:          parseUUID(channelID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	writeJSON(w, http.StatusOK, channelToResponse(c, false))
}

func (h *Handler) ArchiveChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")

	// Only workspace owner/admin or channel owner can archive
	if !h.canManageChannel(w, r, channelID, workspaceID, userID) {
		return
	}

	if err := h.Queries.ArchiveChannel(r.Context(), parseUUID(channelID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive channel")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Channel Members
// ---------------------------------------------------------------------------

type AddMemberRequest struct {
	MemberType string `json:"member_type"` // user or agent
	MemberID   string `json:"member_id"`
	Role       string `json:"role"` // owner, admin, member
}

func (h *Handler) AddChannelMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")

	if !h.canManageChannel(w, r, channelID, workspaceID, userID) {
		return
	}

	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MemberID == "" || req.MemberType == "" {
		writeError(w, http.StatusBadRequest, "member_id and member_type are required")
		return
	}
	role := "member"
	if req.Role != "" {
		role = req.Role
	}

	member, err := h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberType: req.MemberType,
		MemberID:   parseUUID(req.MemberID),
		Role:       role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}

	writeJSON(w, http.StatusCreated, member)
}

func (h *Handler) RemoveChannelMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")
	memberID := chi.URLParam(r, "memberId")
	memberType := r.URL.Query().Get("type")
	if memberType == "" {
		memberType = "user"
	}

	if !h.canManageChannel(w, r, channelID, workspaceID, userID) {
		return
	}

	if err := h.Queries.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberType: memberType,
		MemberID:   parseUUID(memberID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type ChannelMemberResponse struct {
	ID         string `json:"id"`
	ChannelID  string `json:"channel_id"`
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url,omitempty"`
	JoinedAt   string `json:"joined_at"`
}

func (h *Handler) ListChannelMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "member" {
		actorType = "user"
	}
	if !h.canAccessChannel(w, r, channelID, actorID, actorType) {
		return
	}

	members, err := h.Queries.ListChannelMembers(r.Context(), parseUUID(channelID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	resp := make([]ChannelMemberResponse, 0, len(members))
	for _, m := range members {
		cm := ChannelMemberResponse{
			ID:         uuidToString(m.ID),
			ChannelID:  uuidToString(m.ChannelID),
			MemberType: m.MemberType,
			MemberID:   uuidToString(m.MemberID),
			Role:       m.Role,
			JoinedAt:   timestampToString(m.JoinedAt),
		}
		if m.MemberType == "user" {
			if u, err := h.Queries.GetUser(r.Context(), m.MemberID); err == nil {
				cm.Name = u.Name
				cm.AvatarURL = u.AvatarUrl.String
			}
		} else if m.MemberType == "agent" {
			if a, err := h.Queries.GetAgent(r.Context(), m.MemberID); err == nil {
				cm.Name = a.Name
				cm.AvatarURL = a.AvatarUrl.String
			}
		}
		resp = append(resp, cm)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Channel Messages
// ---------------------------------------------------------------------------

type SendChannelMessageRequest struct {
	Content      string  `json:"content"`
	ThreadRootID *string `json:"thread_root_id"`
}

type ChannelMessageResponse struct {
	ID           string  `json:"id"`
	ChannelID    string  `json:"channel_id"`
	AuthorType   string  `json:"author_type"`
	AuthorID     string  `json:"author_id"`
	Content      string  `json:"content"`
	ThreadRootID *string `json:"thread_root_id"`
	TaskID       *string `json:"task_id,omitempty"`
	Status       string  `json:"status"`
	EditedAt     *string `json:"edited_at"`
	CreatedAt    string  `json:"created_at"`
}

func channelMessageToResponse(m db.ChannelMessage) ChannelMessageResponse {
	r := ChannelMessageResponse{
		ID:         uuidToString(m.ID),
		ChannelID:  uuidToString(m.ChannelID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToString(m.AuthorID),
		Content:    m.Content,
		Status:     m.Status,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
	if m.ThreadRootID.Valid {
		ts := uuidToString(m.ThreadRootID)
		r.ThreadRootID = &ts
	}
	if m.TaskID.Valid {
		ts := uuidToString(m.TaskID)
		r.TaskID = &ts
	}
	if m.EditedAt.Valid {
		ts := timestampToString(m.EditedAt)
		r.EditedAt = &ts
	}
	return r
}

func (h *Handler) SendChannelMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")

	var req SendChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Resolve actor (supports agent posting via X-Agent-ID header)
	// Map resolveActor "member" → channel schema "user"
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	channelActorType := actorType
	if channelActorType == "member" {
		channelActorType = "user"
	}

	// Verify channel membership
	isMember, err := h.Queries.IsChannelMember(r.Context(), db.IsChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberType: channelActorType,
		MemberID:   parseUUID(actorID),
	})
	if err != nil || !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}

	var threadRootIDStr *string
	if req.ThreadRootID != nil && *req.ThreadRootID != "" {
		threadRootIDStr = req.ThreadRootID
	}

	var threadRootUUID pgtype.UUID
	if threadRootIDStr != nil {
		threadRootUUID = parseUUID(*threadRootIDStr)
	}

	// If this message is posted by an agent on behalf of a task, record the task_id.
	var taskUUID pgtype.UUID
	if xTaskID := r.Header.Get("X-Task-ID"); xTaskID != "" {
		if parsed, err := util.ParseUUID(xTaskID); err == nil {
			taskUUID = parsed
		}
	}

	msg, err := h.Queries.CreateChannelMessage(r.Context(), db.CreateChannelMessageParams{
		ChannelID:    parseUUID(channelID),
		AuthorType:   channelActorType,
		AuthorID:     parseUUID(actorID),
		Content:      req.Content,
		ThreadRootID: threadRootUUID,
		TaskID:       taskUUID,
	})

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	// Touch channel updated_at (via a simple query or direct exec)
	h.Queries.TouchChannel(r.Context(), parseUUID(channelID))

	h.publish(protocol.EventChannelMessage, workspaceID, channelActorType, actorID, protocol.ChannelMessagePayload{
		ChannelID:    channelID,
		MessageID:    uuidToString(msg.ID),
		AuthorType:   channelActorType,
		Content:      req.Content,
		ThreadRootID: req.ThreadRootID,
		CreatedAt:    timestampToString(msg.CreatedAt),
	})

	// Trigger agent responses for @mentioned agents (and optionally all agent members).
	// Use context.Background() — r.Context() is cancelled as soon as the HTTP
	// response is written, which would abort the DB queries inside the goroutine.
	go h.triggerChannelAgentResponses(
		context.Background(),
		channelID,
		uuidToString(msg.ID),
		workspaceID,
		req.Content,
		actorID,
		channelActorType,
	)

	writeJSON(w, http.StatusCreated, channelMessageToResponse(msg))
}

func (h *Handler) ListChannelMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "member" {
		actorType = "user"
	}
	if !h.canAccessChannel(w, r, channelID, actorID, actorType) {
		return
	}

	messages, err := h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
		ChannelID: parseUUID(channelID),
		Limit:     50,
		Offset:    0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	resp := make([]ChannelMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = channelMessageToResponse(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

// triggerChannelAgentResponses finds agent members of the channel, determines
// which ones should respond (those @mentioned, plus optionally all agent members
// for open discussion), and enqueues a task for each.
//
// Mention rules:
//   - If the message contains @mentions, only mentioned agents respond.
//   - If the message contains no @mention, all agent members respond so agents
//     can discuss among themselves (useful for multi-agent channels).
//
// This runs in a background goroutine after the HTTP response has been sent.
func (h *Handler) triggerChannelAgentResponses(
	ctx context.Context,
	channelID, messageID, workspaceID, content, authorID, authorType string,
) {
	// Fetch all channel members.
	members, err := h.Queries.ListChannelMembers(ctx, parseUUID(channelID))
	if err != nil {
		slog.Error("channel agent responses: list members failed", "channel_id", channelID, "error", err)
		return
	}

	// Collect agent members (skip the author so agents don't reply to themselves).
	type agentMember struct {
		agentID pgtype.UUID
		name    string
	}
	var agentMembers []agentMember
	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		memberIDStr := uuidToString(m.MemberID)
		// Do not trigger the author agent.
		if authorType == "agent" && memberIDStr == authorID {
			continue
		}
		a, err := h.Queries.GetAgent(ctx, m.MemberID)
		if err != nil || a.ArchivedAt.Valid {
			continue
		}
		agentMembers = append(agentMembers, agentMember{agentID: m.MemberID, name: a.Name})
	}

	if len(agentMembers) == 0 {
		return
	}

	// Parse @mentions from the message content.
	mentionNames := parseTextMentions(content)
	hasMentions := len(mentionNames) > 0

	// Build a set of mentioned names for quick lookup (lowercased for case-insensitive match).
	mentioned := make(map[string]bool, len(mentionNames))
	for _, n := range mentionNames {
		mentioned[strings.ToLower(n)] = true
	}

	// Agent-authored messages without an explicit @mention must NOT broadcast to
	// all other agents — that causes exponential ping-pong loops where every
	// agent reply triggers every other agent to reply in turn.
	// Rule: agent → agent communication requires an explicit @mention.
	//       Only user → channel (no @mention) broadcasts to all agent members.
	agentAuthor := authorType == "agent"

	// Resolve author name for task context.
	authorName := ""
	if authorType == "user" || authorType == "member" {
		if u, err := h.Queries.GetUser(ctx, parseUUID(authorID)); err == nil {
			authorName = u.Name
		}
	} else if authorType == "agent" {
		if a, err := h.Queries.GetAgent(ctx, parseUUID(authorID)); err == nil {
			authorName = a.Name
		}
	}
	if authorName == "" {
		authorName = authorType
	}

	workspaceUUID := parseUUID(workspaceID)

	for _, am := range agentMembers {
		// Decide whether this agent should respond.
		shouldRespond := false
		if hasMentions {
			// Explicit @mention: always trigger the named agent, regardless of author type.
			if mentioned[strings.ToLower(am.name)] {
				shouldRespond = true
			}
		} else if !agentAuthor {
			// No @mention, but author is a human: broadcast to all agent members
			// so agents can participate in open discussion.
			shouldRespond = true
		}
		// If agentAuthor && !hasMentions: do NOT trigger anyone — prevents loops.
		if !shouldRespond {
			continue
		}

		if _, err := h.TaskService.EnqueueChannelMessageTask(
			ctx,
			am.agentID,
			workspaceUUID,
			channelID,
			messageID,
			content,
			authorName,
		); err != nil {
			slog.Warn("channel agent responses: enqueue task failed",
				"agent_id", uuidToString(am.agentID),
				"channel_id", channelID,
				"message_id", messageID,
				"error", err,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *Handler) canManageChannel(w http.ResponseWriter, r *http.Request, channelID, workspaceID, userID string) bool {
	if _, ok := requireUserID(w, r); !ok {
		return false
	}

	// Check channel membership and role
	members, err := h.Queries.ListChannelMembers(r.Context(), parseUUID(channelID))
	if err != nil {
		writeError(w, http.StatusForbidden, "failed to check channel membership")
		return false
	}

	for _, m := range members {
		if m.MemberType == "user" && uuidToString(m.MemberID) == userID {
			if m.Role == "owner" || m.Role == "admin" {
				return true
			}
			writeError(w, http.StatusForbidden, "only channel owner/admin can manage")
			return false
		}
	}

	writeError(w, http.StatusForbidden, "not a member of this channel")
	return false
}

// canAccessChannel checks if a user/agent is a member (read access).
func (h *Handler) canAccessChannel(w http.ResponseWriter, r *http.Request, channelID, userID, actorType string) bool {
	isMember, err := h.Queries.IsChannelMember(r.Context(), db.IsChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberType: actorType,
		MemberID:   parseUUID(userID),
	})
	if err != nil || !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Channel Read State
// ---------------------------------------------------------------------------

func (h *Handler) MarkChannelRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	_ = workspaceID
	channelID := chi.URLParam(r, "channelId")

	var req struct {
		LastReadMessageID string `json:"last_read_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.Queries.MarkChannelRead(r.Context(), db.MarkChannelReadParams{
		ChannelID:         parseUUID(channelID),
		MemberType:        "user",
		MemberID:          parseUUID(userID),
		LastReadMessageID: parseUUID(req.LastReadMessageID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Thread Replies
// ---------------------------------------------------------------------------

func (h *Handler) ListThreadReplies(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "channelId")
	threadID := chi.URLParam(r, "messageId")

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "member" {
		actorType = "user"
	}
	if !h.canAccessChannel(w, r, channelID, actorID, actorType) {
		return
	}

	replies, err := h.Queries.ListThreadReplies(r.Context(), parseUUID(threadID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list thread replies")
		return
	}

	resp := make([]ChannelMessageResponse, len(replies))
	for i, m := range replies {
		resp[i] = channelMessageToResponse(m)
	}
	// Verify channel matches
	if len(replies) > 0 && uuidToString(replies[0].ChannelID) != channelID {
		writeError(w, http.StatusNotFound, "thread not found in this channel")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
