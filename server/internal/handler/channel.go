package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const channelNameMaxLen = 80
const channelMessageMaxLen = 20000
const channelContextMessageLimit = 30
const channelRunTriggerLimit = 10
const channelUserTypingExpiresInMS = 5000
const channelAgentTypingExpiresInMS = 10 * 60 * 1000

type ChannelResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	LarkChatID  *string `json:"lark_chat_id"`
	CreatedBy   string  `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type ChannelMemberResponse struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
}

type ChannelMessageResponse struct {
	ID                string  `json:"id"`
	ChannelID         string  `json:"channel_id"`
	WorkspaceID       string  `json:"workspace_id"`
	AuthorType        string  `json:"author_type"`
	AuthorID          *string `json:"author_id"`
	AuthorName        string  `json:"author_name"`
	Content           string  `json:"content"`
	Source            string  `json:"source"`
	ExternalMessageID *string `json:"external_message_id"`
	ThreadID          *string `json:"thread_id,omitempty"`
	TriggerDepth      int     `json:"trigger_depth"`
	CreatedAt         string  `json:"created_at"`
}

type CreateChannelRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	LarkChatID  *string `json:"lark_chat_id"`
}

type UpdateChannelRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	LarkChatID  *string `json:"lark_chat_id"`
}

type AddChannelMemberRequest struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
}

type SendChannelMessageRequest struct {
	Content string `json:"content"`
}

type ChannelTypingRequest struct {
	IsTyping bool `json:"is_typing"`
}

type ImportLarkChannelMessageRequest struct {
	LarkChatID        string `json:"lark_chat_id"`
	ExternalMessageID string `json:"external_message_id"`
	AuthorName        string `json:"author_name"`
	Content           string `json:"content"`
}

func (h *Handler) StartChannelBridge() {
	if h.Bus == nil {
		return
	}
	h.Bus.Subscribe(protocol.EventChatDone, h.handleChannelChatDone)
	h.Bus.Subscribe(protocol.EventTaskFailed, h.handleChannelChatStopped)
	h.Bus.Subscribe(protocol.EventTaskCancelled, h.handleChannelChatStopped)
}

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	rows, err := h.DB.Query(r.Context(), `
		SELECT ch.id, ch.workspace_id, ch.name, ch.description, ch.lark_chat_id, ch.created_by, ch.created_at, ch.updated_at
		FROM channel ch
		JOIN channel_member cm ON cm.channel_id = ch.id
		WHERE ch.workspace_id = $1 AND cm.member_type = 'user' AND cm.member_id = $2
		ORDER BY ch.updated_at DESC, ch.created_at DESC`, parseUUID(workspaceID), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	defer rows.Close()
	out := []ChannelResponse{}
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read channels")
			return
		}
		out = append(out, ch)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	var req CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len([]rune(name)) > channelNameMaxLen {
		writeError(w, http.StatusBadRequest, "name is too long")
		return
	}
	desc := trimTextPtr(req.Description)
	larkChatID := trimTextPtr(req.LarkChatID)
	row := h.DB.QueryRow(r.Context(), `
		INSERT INTO channel (workspace_id, name, description, lark_chat_id, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, workspace_id, name, description, lark_chat_id, created_by, created_at, updated_at`,
		parseUUID(workspaceID), name, desc, larkChatID, parseUUID(userID))
	ch, err := scanChannel(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}
	_, _ = h.DB.Exec(r.Context(), `
		INSERT INTO channel_member (channel_id, workspace_id, member_type, member_id)
		VALUES ($1, $2, 'user', $3)
		ON CONFLICT DO NOTHING`, parseUUID(ch.ID), parseUUID(workspaceID), parseUUID(userID))
	h.publish(protocol.EventChannelUpdated, workspaceID, "member", userID, ch)
	writeJSON(w, http.StatusCreated, ch)
}

func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	if !h.requireChannelUserMember(w, r.Context(), workspaceID, channelID, parseUUID(userID)) {
		return
	}
	var req UpdateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var name *string
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if len([]rune(trimmed)) > channelNameMaxLen {
			writeError(w, http.StatusBadRequest, "name is too long")
			return
		}
		name = &trimmed
	}
	row := h.DB.QueryRow(r.Context(), `
		UPDATE channel
		SET name = COALESCE($3, name), description = COALESCE($4, description), lark_chat_id = COALESCE($5, lark_chat_id), updated_at = now()
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, name, description, lark_chat_id, created_by, created_at, updated_at`,
		channelID, parseUUID(workspaceID), name, trimTextPtr(req.Description), trimTextPtr(req.LarkChatID))
	ch, err := scanChannel(row)
	if err != nil {
		if errorsIsNoRows(err) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	h.publish(protocol.EventChannelUpdated, workspaceID, "member", userID, ch)
	writeJSON(w, http.StatusOK, ch)
}

func (h *Handler) ListChannelMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	if !h.requireChannelUserMember(w, r.Context(), workspaceID, channelID, parseUUID(userID)) {
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT cm.member_type, cm.member_id,
		       COALESCE(u.name, u.email, a.name, ''), cm.created_at
		FROM channel_member cm
		LEFT JOIN "user" u ON cm.member_type = 'user' AND u.id = cm.member_id
		LEFT JOIN agent a ON cm.member_type = 'agent' AND a.id = cm.member_id
		WHERE cm.channel_id = $1 AND cm.workspace_id = $2
		ORDER BY cm.created_at ASC`, channelID, parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channel members")
		return
	}
	defer rows.Close()
	out := []ChannelMemberResponse{}
	for rows.Next() {
		var typ, name string
		var id pgtype.UUID
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&typ, &id, &name, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read channel members")
			return
		}
		out = append(out, ChannelMemberResponse{MemberType: typ, MemberID: uuidToString(id), Name: name, CreatedAt: timestampToString(createdAt)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) AddChannelMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	var req AddChannelMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	memberID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}
	if !h.requireChannelUserMember(w, r.Context(), workspaceID, channelID, parseUUID(userID)) {
		return
	}
	if !h.validateChannelMemberTarget(w, r, workspaceID, req.MemberType, memberID) {
		return
	}
	_, err := h.DB.Exec(r.Context(), `
		INSERT INTO channel_member (channel_id, workspace_id, member_type, member_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, channelID, parseUUID(workspaceID), req.MemberType, memberID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add channel member")
		return
	}
	h.publish(protocol.EventChannelUpdated, workspaceID, "member", userID, map[string]any{"id": uuidToString(channelID)})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handler) RemoveChannelMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	memberType := chi.URLParam(r, "memberType")
	memberID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member id")
	if !ok {
		return
	}
	if memberType != "user" && memberType != "agent" {
		writeError(w, http.StatusBadRequest, "member_type must be user or agent")
		return
	}
	if !h.requireChannelUserMember(w, r.Context(), workspaceID, channelID, parseUUID(userID)) {
		return
	}
	_, err := h.DB.Exec(r.Context(), `
		DELETE FROM channel_member
		WHERE channel_id = $1 AND workspace_id = $2 AND member_type = $3 AND member_id = $4`,
		channelID, parseUUID(workspaceID), memberType, memberID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove channel member")
		return
	}
	h.publish(protocol.EventChannelUpdated, workspaceID, "member", userID, map[string]any{"id": uuidToString(channelID)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListChannelMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	if !h.requireChannelUserMember(w, r.Context(), workspaceID, channelID, parseUUID(userID)) {
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, channel_id, workspace_id, author_type, author_id, author_name, content, source, external_message_id, thread_id, trigger_depth, created_at
		FROM channel_message
		WHERE channel_id = $1 AND workspace_id = $2
		ORDER BY created_at ASC`, channelID, parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channel messages")
		return
	}
	defer rows.Close()
	out := []ChannelMessageResponse{}
	for rows.Next() {
		msg, err := scanChannelMessage(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read channel messages")
			return
		}
		out = append(out, msg)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) SetChannelTyping(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	var req ChannelTypingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.requireChannelUserMember(w, r.Context(), workspaceID, channelID, parseUUID(userID)) {
		return
	}
	h.publish(protocol.EventChannelTyping, workspaceID, "member", userID, protocol.ChannelTypingPayload{
		ChannelID:   uuidToString(channelID),
		ActorType:   "user",
		ActorID:     userID,
		ActorName:   h.channelAuthorName(r.Context(), userID),
		IsTyping:    req.IsTyping,
		ExpiresInMS: channelUserTypingExpiresInMS,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) SendChannelMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	var req SendChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len([]rune(content)) > channelMessageMaxLen {
		writeError(w, http.StatusBadRequest, "content is too long")
		return
	}
	ch, found := h.getChannel(r.Context(), workspaceID, channelID)
	if !found {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if !h.requireChannelUserMember(w, r.Context(), workspaceID, channelID, parseUUID(userID)) {
		return
	}
	authorName := h.channelAuthorName(r.Context(), userID)
	threadID := uuid.NewString()
	msg, err := h.insertChannelMessage(r.Context(), channelID, parseUUID(workspaceID), "user", parseUUID(userID), authorName, content, "multica", nil, &threadID, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel message")
		return
	}
	_, _ = h.DB.Exec(r.Context(), `UPDATE channel SET updated_at = now() WHERE id = $1`, channelID)
	h.publish(protocol.EventChannelMessage, workspaceID, "member", userID, msg)
	h.dispatchChannelMentions(r.Context(), ch, msg, parseUUID(userID))
	h.sendChannelMessageToFeishu(r.Context(), ch, authorName, content)
	writeJSON(w, http.StatusCreated, msg)
}

func (h *Handler) ImportLarkChannelMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	var req ImportLarkChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	larkChatID := strings.TrimSpace(req.LarkChatID)
	content := strings.TrimSpace(req.Content)
	if larkChatID == "" || content == "" {
		writeError(w, http.StatusBadRequest, "lark_chat_id and content are required")
		return
	}
	if len([]rune(content)) > channelMessageMaxLen {
		writeError(w, http.StatusBadRequest, "content is too long")
		return
	}
	authorName := strings.TrimSpace(req.AuthorName)
	if authorName == "" {
		authorName = "Feishu"
	}
	externalID := strings.TrimSpace(req.ExternalMessageID)
	var external *string
	if externalID != "" {
		external = &externalID
	}

	ch, found := h.getChannelByLarkChatID(r.Context(), workspaceID, larkChatID)
	if !found {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	threadID := uuid.NewString()
	msg, err := h.insertChannelMessage(r.Context(), parseUUID(ch.ID), parseUUID(workspaceID), "lark", pgtype.UUID{}, authorName, content, "lark", external, &threadID, 0)
	if err != nil {
		if errorsIsNoRows(err) || isUniqueViolation(err) {
			writeError(w, http.StatusNotFound, "channel not found or message already imported")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to import lark message")
		return
	}
	_, _ = h.DB.Exec(r.Context(), `UPDATE channel SET updated_at = now() WHERE id = $1`, parseUUID(msg.ChannelID))
	h.publish(protocol.EventChannelMessage, workspaceID, "member", userID, msg)
	h.dispatchChannelMentions(r.Context(), ch, msg, parseUUID(userID))
	writeJSON(w, http.StatusCreated, msg)
}

func (h *Handler) validateChannelMemberTarget(w http.ResponseWriter, r *http.Request, workspaceID, memberType string, memberID pgtype.UUID) bool {
	switch memberType {
	case "user":
		if _, err := h.getWorkspaceMember(r.Context(), uuidToString(memberID), workspaceID); err != nil {
			writeError(w, http.StatusNotFound, "workspace member not found")
			return false
		}
		return true
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: memberID, WorkspaceID: parseUUID(workspaceID)})
		if err != nil || agent.ArchivedAt.Valid {
			writeError(w, http.StatusNotFound, "agent not found")
			return false
		}
		userID, _ := requireUserID(w, r)
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "you do not have access to this agent")
			return false
		}
		return true
	default:
		writeError(w, http.StatusBadRequest, "member_type must be user or agent")
		return false
	}
}

func (h *Handler) dispatchChannelMentions(ctx context.Context, ch ChannelResponse, trigger ChannelMessageResponse, initiatorUserID pgtype.UUID) {
	if trigger.TriggerDepth >= channelRunTriggerLimit {
		slog.Warn("channel mention: trigger limit reached", "channel", ch.ID, "thread_id", ptrString(trigger.ThreadID), "depth", trigger.TriggerDepth)
		return
	}
	agents := h.channelMentionedAgents(ctx, ch.WorkspaceID, ch.ID, trigger.Content)
	for _, agent := range agents {
		if trigger.AuthorType == "agent" && trigger.AuthorID != nil && *trigger.AuthorID == uuidToString(agent.ID) {
			continue
		}
		h.publishChannelAgentTyping(ch, agent, true)
		session, err := h.ensureChannelAgentSession(ctx, ch, agent.ID, initiatorUserID)
		if err != nil {
			slog.Warn("channel mention: ensure chat session failed", "channel", ch.ID, "agent", uuidToString(agent.ID), "error", err)
			continue
		}
		prompt := h.buildChannelMentionPrompt(ctx, ch, trigger)
		promptMsg, err := h.createChannelAgentPromptMessage(ctx, session.ID, prompt, trigger)
		if err != nil {
			slog.Warn("channel mention: create chat message failed", "channel", ch.ID, "agent", uuidToString(agent.ID), "error", err)
			continue
		}
		task, err := h.TaskService.EnqueueChatTask(ctx, session, initiatorUserID)
		if err != nil {
			slog.Warn("channel mention: enqueue chat task failed", "channel", ch.ID, "agent", uuidToString(agent.ID), "error", err)
			continue
		}
		if _, err := h.DB.Exec(ctx, `UPDATE chat_message SET task_id = $1 WHERE id = $2`, task.ID, promptMsg.ID); err != nil {
			slog.Warn("channel mention: tag prompt with task failed", "channel", ch.ID, "agent", uuidToString(agent.ID), "task", uuidToString(task.ID), "error", err)
		}
	}
}

func (h *Handler) publishChannelAgentTyping(ch ChannelResponse, agent db.Agent, isTyping bool) {
	h.publish(protocol.EventChannelTyping, ch.WorkspaceID, "agent", uuidToString(agent.ID), protocol.ChannelTypingPayload{
		ChannelID:   ch.ID,
		ActorType:   "agent",
		ActorID:     uuidToString(agent.ID),
		ActorName:   agent.Name,
		IsTyping:    isTyping,
		ExpiresInMS: channelAgentTypingExpiresInMS,
	})
}

func (h *Handler) channelMentionedAgents(ctx context.Context, workspaceID, channelID, content string) []db.Agent {
	mentionAll := contentMentionsAll(content)
	rows, err := h.DB.Query(ctx, `
		SELECT a.id, a.workspace_id, a.name, a.avatar_url, a.runtime_mode, a.runtime_config, a.visibility, a.status,
		       a.max_concurrent_tasks, a.owner_id, a.created_at, a.updated_at, a.description, a.runtime_id,
		       a.archived_at
		FROM channel_member cm
		JOIN agent a ON cm.member_type = 'agent' AND a.id = cm.member_id
		WHERE cm.channel_id = $1 AND cm.workspace_id = $2 AND a.archived_at IS NULL`, parseUUID(channelID), parseUUID(workspaceID))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []db.Agent
	for rows.Next() {
		var a db.Agent
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.Name, &a.AvatarUrl, &a.RuntimeMode, &a.RuntimeConfig, &a.Visibility, &a.Status, &a.MaxConcurrentTasks, &a.OwnerID, &a.CreatedAt, &a.UpdatedAt, &a.Description, &a.RuntimeID, &a.ArchivedAt); err != nil {
			continue
		}
		if mentionAll || contentMentionsAgent(content, a.Name) {
			out = append(out, a)
		}
	}
	return out
}

func contentMentionsAll(content string) bool {
	return strings.Contains(strings.ToLower(content), "@all")
}

func contentMentionsAgent(content, name string) bool {
	needle := "@" + strings.ToLower(strings.TrimSpace(name))
	if needle == "@" {
		return false
	}
	return strings.Contains(strings.ToLower(content), needle)
}

func (h *Handler) buildChannelMentionPrompt(ctx context.Context, ch ChannelResponse, trigger ChannelMessageResponse) string {
	members := h.channelMemberSummaries(ctx, ch.WorkspaceID, ch.ID)
	messages := h.recentChannelMessages(ctx, ch.WorkspaceID, ch.ID, channelContextMessageLimit)

	var b strings.Builder
	fmt.Fprintf(&b, "You are participating in the Multica group chat #%s.\n", ch.Name)
	b.WriteString("Only respond as yourself. Do not impersonate other agents or users.\n")
	b.WriteString("Use the recent channel context below, but answer the current mention directly.\n")
	b.WriteString("Only @ another agent when you explicitly need them to continue. Avoid unnecessary @ mentions.\n")
	fmt.Fprintf(&b, "This channel run is limited to %d automatic agent turns; current trigger depth is %d.\n\n", channelRunTriggerLimit, trigger.TriggerDepth)
	if len(members) > 0 {
		b.WriteString("Channel members:\n")
		for _, member := range members {
			fmt.Fprintf(&b, "- %s: %s\n", member.MemberType, member.Name)
		}
		b.WriteString("\n")
	}
	if len(messages) > 0 {
		b.WriteString("Recent channel messages:\n")
		for _, msg := range messages {
			fmt.Fprintf(&b, "[%s] %s (%s): %s\n", msg.CreatedAt, msg.AuthorName, msg.AuthorType, msg.Content)
		}
		b.WriteString("\n")
	}
	b.WriteString("Current message to respond to:\n")
	fmt.Fprintf(&b, "%s (%s): %s", trigger.AuthorName, trigger.AuthorType, trigger.Content)
	return b.String()
}

func (h *Handler) channelMemberSummaries(ctx context.Context, workspaceID, channelID string) []ChannelMemberResponse {
	rows, err := h.DB.Query(ctx, `
		SELECT cm.member_type, cm.member_id,
		       COALESCE(u.name, u.email, a.name, ''), cm.created_at
		FROM channel_member cm
		LEFT JOIN "user" u ON cm.member_type = 'user' AND u.id = cm.member_id
		LEFT JOIN agent a ON cm.member_type = 'agent' AND a.id = cm.member_id
		WHERE cm.channel_id = $1 AND cm.workspace_id = $2
		ORDER BY cm.created_at ASC`, parseUUID(channelID), parseUUID(workspaceID))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ChannelMemberResponse
	for rows.Next() {
		var typ, name string
		var id pgtype.UUID
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&typ, &id, &name, &createdAt); err != nil {
			continue
		}
		out = append(out, ChannelMemberResponse{MemberType: typ, MemberID: uuidToString(id), Name: name, CreatedAt: timestampToString(createdAt)})
	}
	return out
}

func (h *Handler) recentChannelMessages(ctx context.Context, workspaceID, channelID string, limit int) []ChannelMessageResponse {
	rows, err := h.DB.Query(ctx, `
		SELECT id, channel_id, workspace_id, author_type, author_id, author_name, content, source, external_message_id, thread_id, trigger_depth, created_at
		FROM (
			SELECT id, channel_id, workspace_id, author_type, author_id, author_name, content, source, external_message_id, thread_id, trigger_depth, created_at
			FROM channel_message
			WHERE channel_id = $1 AND workspace_id = $2
			ORDER BY created_at DESC
			LIMIT $3
		) recent
		ORDER BY created_at ASC`, parseUUID(channelID), parseUUID(workspaceID), limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ChannelMessageResponse
	for rows.Next() {
		msg, err := scanChannelMessage(rows)
		if err != nil {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func (h *Handler) createChannelAgentPromptMessage(ctx context.Context, chatSessionID pgtype.UUID, prompt string, trigger ChannelMessageResponse) (db.ChatMessage, error) {
	threadID := trigger.ThreadID
	if threadID == nil || strings.TrimSpace(*threadID) == "" {
		fresh := uuid.NewString()
		threadID = &fresh
	}
	row := h.DB.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, thread_id, trigger_depth)
		VALUES ($1, 'user', $2, $3, $4)
		RETURNING id, chat_session_id, role, content, task_id, created_at, failure_reason, elapsed_ms`, chatSessionID, prompt, threadID, trigger.TriggerDepth)
	var msg db.ChatMessage
	err := row.Scan(&msg.ID, &msg.ChatSessionID, &msg.Role, &msg.Content, &msg.TaskID, &msg.CreatedAt, &msg.FailureReason, &msg.ElapsedMs)
	return msg, err
}

func (h *Handler) ensureChannelAgentSession(ctx context.Context, ch ChannelResponse, agentID pgtype.UUID, creatorID pgtype.UUID) (db.ChatSession, error) {
	var sessionID pgtype.UUID
	err := h.DB.QueryRow(ctx, `SELECT chat_session_id FROM channel_agent_session WHERE channel_id = $1 AND agent_id = $2`, parseUUID(ch.ID), agentID).Scan(&sessionID)
	if err == nil {
		return h.Queries.GetChatSession(ctx, sessionID)
	}
	if !errorsIsNoRows(err) {
		return db.ChatSession{}, err
	}
	session, err := h.Queries.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID: parseUUID(ch.WorkspaceID),
		AgentID:     agentID,
		CreatorID:   creatorID,
		Title:       "#" + ch.Name,
	})
	if err != nil {
		return db.ChatSession{}, err
	}
	_, err = h.DB.Exec(ctx, `
		INSERT INTO channel_agent_session (channel_id, agent_id, chat_session_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (channel_id, agent_id) DO NOTHING`, parseUUID(ch.ID), agentID, session.ID)
	if err != nil {
		return db.ChatSession{}, err
	}
	return session, nil
}

func (h *Handler) handleChannelChatStopped(e events.Event) {
	chatSessionID, _ := e.Payload.(map[string]any)["chat_session_id"].(string)
	if chatSessionID == "" {
		return
	}
	channelID, workspaceID, agentID, ok := h.channelAgentForChatSession(context.Background(), chatSessionID)
	if !ok {
		return
	}
	h.publish(protocol.EventChannelTyping, uuidToString(workspaceID), "agent", uuidToString(agentID), protocol.ChannelTypingPayload{
		ChannelID: uuidToString(channelID),
		ActorType: "agent",
		ActorID:   uuidToString(agentID),
		ActorName: h.agentName(context.Background(), agentID),
		IsTyping:  false,
	})
}

func (h *Handler) handleChannelChatDone(e events.Event) {
	payload, ok := e.Payload.(protocol.ChatDonePayload)
	if !ok || payload.ChatSessionID == "" || strings.TrimSpace(payload.Content) == "" {
		return
	}
	channelID, workspaceID, agentID, ok := h.channelAgentForChatSession(context.Background(), payload.ChatSessionID)
	if !ok {
		return
	}
	var taskID pgtype.UUID
	if strings.TrimSpace(payload.TaskID) != "" {
		taskID = parseUUID(payload.TaskID)
	}
	threadID, triggerDepth := h.channelThreadForChatTask(context.Background(), parseUUID(payload.ChatSessionID), taskID)
	nextDepth := triggerDepth + 1
	agentName := h.agentName(context.Background(), agentID)
	h.publish(protocol.EventChannelTyping, uuidToString(workspaceID), "agent", uuidToString(agentID), protocol.ChannelTypingPayload{
		ChannelID: uuidToString(channelID),
		ActorType: "agent",
		ActorID:   uuidToString(agentID),
		ActorName: agentName,
		IsTyping:  false,
	})
	msg, err := h.insertChannelMessage(context.Background(), channelID, workspaceID, "agent", agentID, agentName, payload.Content, "multica", nil, threadID, nextDepth)
	if err != nil {
		slog.Warn("channel bridge: insert agent reply failed", "chat_session_id", payload.ChatSessionID, "error", err)
		return
	}
	_, _ = h.DB.Exec(context.Background(), `UPDATE channel SET updated_at = now() WHERE id = $1`, channelID)
	h.publish(protocol.EventChannelMessage, uuidToString(workspaceID), "agent", uuidToString(agentID), msg)
	ch, found := h.getChannel(context.Background(), uuidToString(workspaceID), channelID)
	if found {
		h.dispatchChannelMentions(context.Background(), ch, msg, h.channelInitiatorForChatSession(context.Background(), parseUUID(payload.ChatSessionID)))
		h.sendChannelMessageToFeishu(context.Background(), ch, agentName, payload.Content)
	}
}

func (h *Handler) channelAgentForChatSession(ctx context.Context, chatSessionID string) (pgtype.UUID, pgtype.UUID, pgtype.UUID, bool) {
	var channelID, workspaceID, agentID pgtype.UUID
	err := h.DB.QueryRow(ctx, `
		SELECT cas.channel_id, ch.workspace_id, cas.agent_id
		FROM channel_agent_session cas
		JOIN channel ch ON ch.id = cas.channel_id
		WHERE cas.chat_session_id = $1`, parseUUID(chatSessionID)).Scan(&channelID, &workspaceID, &agentID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	return channelID, workspaceID, agentID, true
}

func (h *Handler) channelThreadForChatTask(ctx context.Context, chatSessionID, taskID pgtype.UUID) (*string, int) {
	if taskID.Valid {
		if threadID, depth, ok := h.channelThreadFromQuery(ctx, `
			SELECT thread_id, trigger_depth
			FROM chat_message
			WHERE chat_session_id = $1 AND task_id = $2 AND role = 'user'
			ORDER BY created_at DESC
			LIMIT 1`, chatSessionID, taskID); ok {
			return threadID, depth
		}
	}
	if threadID, depth, ok := h.channelThreadFromQuery(ctx, `
		SELECT thread_id, trigger_depth
		FROM chat_message
		WHERE chat_session_id = $1 AND role = 'user'
		ORDER BY created_at DESC
		LIMIT 1`, chatSessionID); ok {
		return threadID, depth
	}
	fresh := uuid.NewString()
	return &fresh, 0
}

func (h *Handler) channelThreadFromQuery(ctx context.Context, query string, args ...any) (*string, int, bool) {
	var thread pgtype.Text
	var depth int
	err := h.DB.QueryRow(ctx, query, args...).Scan(&thread, &depth)
	if err != nil || !thread.Valid || strings.TrimSpace(thread.String) == "" {
		return nil, 0, false
	}
	return &thread.String, depth, true
}

func (h *Handler) channelInitiatorForChatSession(ctx context.Context, chatSessionID pgtype.UUID) pgtype.UUID {
	var initiator pgtype.UUID
	err := h.DB.QueryRow(ctx, `
		SELECT initiator_user_id
		FROM agent_task_queue
		WHERE chat_session_id = $1 AND initiator_user_id IS NOT NULL
		ORDER BY created_at DESC
		LIMIT 1`, chatSessionID).Scan(&initiator)
	if err == nil && initiator.Valid {
		return initiator
	}
	var creator pgtype.UUID
	if err := h.DB.QueryRow(ctx, `SELECT creator_id FROM chat_session WHERE id = $1`, chatSessionID).Scan(&creator); err == nil {
		return creator
	}
	return pgtype.UUID{}
}

func (h *Handler) insertChannelMessage(ctx context.Context, channelID, workspaceID pgtype.UUID, authorType string, authorID pgtype.UUID, authorName, content, source string, externalID *string, threadID *string, triggerDepth int) (ChannelMessageResponse, error) {
	row := h.DB.QueryRow(ctx, `
		INSERT INTO channel_message (channel_id, workspace_id, author_type, author_id, author_name, content, source, external_message_id, thread_id, trigger_depth)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, channel_id, workspace_id, author_type, author_id, author_name, content, source, external_message_id, thread_id, trigger_depth, created_at`,
		channelID, workspaceID, authorType, nullableUUID(authorID), authorName, content, source, externalID, threadID, triggerDepth)
	return scanChannelMessage(row)
}

func (h *Handler) sendChannelMessageToFeishu(ctx context.Context, ch ChannelResponse, authorName, content string) {
	if ch.LarkChatID == nil || h.LarkAPIClient == nil || h.LarkInstallations == nil || !h.LarkAPIClient.IsConfigured() {
		return
	}
	inst, ok := h.firstActiveFeishuInstallation(ctx, ch.WorkspaceID)
	if !ok {
		return
	}
	secret, err := h.LarkInstallations.DecryptAppSecret(inst)
	if err != nil {
		slog.Warn("channel feishu sync: decrypt app secret failed", "error", err)
		return
	}
	creds := lark.InstallationCredentials{AppID: inst.AppID, AppSecret: secret, TenantKey: inst.TenantKey.String, Region: lark.RegionOrDefault(inst.Region)}
	text := strings.TrimSpace(authorName + ": " + content)
	_, err = h.LarkAPIClient.SendTextMessage(ctx, lark.SendTextParams{InstallationID: creds, ChatID: lark.ChatID(*ch.LarkChatID), Text: text})
	if err != nil {
		slog.Warn("channel feishu sync: send text failed", "channel", ch.ID, "error", err)
	}
}

func (h *Handler) firstActiveFeishuInstallation(ctx context.Context, workspaceID string) (db.LarkInstallation, bool) {
	rows, err := h.Queries.ListLarkInstallationsByWorkspace(ctx, parseUUID(workspaceID))
	if err != nil {
		return db.LarkInstallation{}, false
	}
	for _, row := range rows {
		if row.Status == "active" && lark.RegionOrDefault(row.Region) == lark.RegionFeishu {
			return row, true
		}
	}
	return db.LarkInstallation{}, false
}

func (h *Handler) channelExists(ctx context.Context, workspaceID string, channelID pgtype.UUID) bool {
	var id pgtype.UUID
	return h.DB.QueryRow(ctx, `SELECT id FROM channel WHERE id = $1 AND workspace_id = $2`, channelID, parseUUID(workspaceID)).Scan(&id) == nil
}

func (h *Handler) channelUserIsMember(ctx context.Context, workspaceID string, channelID, userID pgtype.UUID) bool {
	var exists bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM channel_member
			WHERE channel_id = $1 AND workspace_id = $2 AND member_type = 'user' AND member_id = $3
		)`, channelID, parseUUID(workspaceID), userID).Scan(&exists)
	return err == nil && exists
}

func (h *Handler) requireChannelUserMember(w http.ResponseWriter, ctx context.Context, workspaceID string, channelID, userID pgtype.UUID) bool {
	if !h.channelExists(ctx, workspaceID, channelID) {
		writeError(w, http.StatusNotFound, "channel not found")
		return false
	}
	if !h.channelUserIsMember(ctx, workspaceID, channelID, userID) {
		writeError(w, http.StatusForbidden, "not a channel member")
		return false
	}
	return true
}

func (h *Handler) getChannel(ctx context.Context, workspaceID string, channelID pgtype.UUID) (ChannelResponse, bool) {
	row := h.DB.QueryRow(ctx, `SELECT id, workspace_id, name, description, lark_chat_id, created_by, created_at, updated_at FROM channel WHERE id = $1 AND workspace_id = $2`, channelID, parseUUID(workspaceID))
	ch, err := scanChannel(row)
	return ch, err == nil
}

func (h *Handler) getChannelByLarkChatID(ctx context.Context, workspaceID, larkChatID string) (ChannelResponse, bool) {
	row := h.DB.QueryRow(ctx, `SELECT id, workspace_id, name, description, lark_chat_id, created_by, created_at, updated_at FROM channel WHERE workspace_id = $1 AND lark_chat_id = $2 LIMIT 1`, parseUUID(workspaceID), larkChatID)
	ch, err := scanChannel(row)
	return ch, err == nil
}

func (h *Handler) channelAuthorName(ctx context.Context, userID string) string {
	user, err := h.Queries.GetUser(ctx, parseUUID(userID))
	if err == nil && strings.TrimSpace(user.Name) != "" {
		return user.Name
	}
	if err == nil && strings.TrimSpace(user.Email) != "" {
		return user.Email
	}
	return "You"
}

func (h *Handler) agentName(ctx context.Context, agentID pgtype.UUID) string {
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err == nil && strings.TrimSpace(agent.Name) != "" {
		return agent.Name
	}
	return "Agent"
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanChannel(row rowScanner) (ChannelResponse, error) {
	var id, wsID, createdBy pgtype.UUID
	var name string
	var desc, lark pgtype.Text
	var createdAt, updatedAt pgtype.Timestamptz
	if err := row.Scan(&id, &wsID, &name, &desc, &lark, &createdBy, &createdAt, &updatedAt); err != nil {
		return ChannelResponse{}, err
	}
	return ChannelResponse{ID: uuidToString(id), WorkspaceID: uuidToString(wsID), Name: name, Description: textToPtr(desc), LarkChatID: textToPtr(lark), CreatedBy: uuidToString(createdBy), CreatedAt: timestampToString(createdAt), UpdatedAt: timestampToString(updatedAt)}, nil
}

func scanChannelMessage(row rowScanner) (ChannelMessageResponse, error) {
	var id, channelID, wsID, authorID pgtype.UUID
	var authorType, authorName, content, source string
	var external, thread pgtype.Text
	var triggerDepth int
	var createdAt pgtype.Timestamptz
	if err := row.Scan(&id, &channelID, &wsID, &authorType, &authorID, &authorName, &content, &source, &external, &thread, &triggerDepth, &createdAt); err != nil {
		return ChannelMessageResponse{}, err
	}
	return ChannelMessageResponse{ID: uuidToString(id), ChannelID: uuidToString(channelID), WorkspaceID: uuidToString(wsID), AuthorType: authorType, AuthorID: uuidToPtr(authorID), AuthorName: authorName, Content: content, Source: source, ExternalMessageID: textToPtr(external), ThreadID: textToPtr(thread), TriggerDepth: triggerDepth, CreatedAt: timestampToString(createdAt)}, nil
}

func trimTextPtr(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableUUID(u pgtype.UUID) any {
	if !u.Valid {
		return nil
	}
	return u
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func errorsIsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
