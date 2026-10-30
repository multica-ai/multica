package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Team conversations (MUL-8357): channels where people and agents talk,
// member-to-member direct messages, and threads under a message of either.
// An agent replies in a channel only when @mentioned (or, in a thread, when it
// already holds the floor there); its reply runs as an ordinary chat task on a
// hidden chat_session per (conversation, agent) and is mirrored back here.
//
// Access has one rule, canAccessConversation: a thread follows its parent; a
// public channel is open to every workspace member; anything else needs a
// membership row.

const (
	conversationKindChannel = "channel"
	conversationKindDirect  = "direct"
	conversationKindThread  = "thread"

	conversationMessageMaxRunes = 20000
	conversationPageDefault     = 50
	conversationPageMax         = 200
)

type ConversationMemberResponse struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
}

type ConversationWorkingAgentResponse struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

type ConversationResponse struct {
	ID                   string                             `json:"id"`
	WorkspaceID          string                             `json:"workspace_id"`
	Kind                 string                             `json:"kind"`
	Name                 string                             `json:"name"`
	About                string                             `json:"about"`
	Visibility           string                             `json:"visibility"`
	ParentConversationID *string                            `json:"parent_conversation_id,omitempty"`
	RootMessageID        *string                            `json:"root_message_id,omitempty"`
	CreatedBy            string                             `json:"created_by"`
	LastMessageSeq       int64                              `json:"last_message_seq"`
	LastMessageAt        *string                            `json:"last_message_at"`
	ArchivedAt           *string                            `json:"archived_at"`
	CreatedAt            string                             `json:"created_at"`
	UpdatedAt            string                             `json:"updated_at"`
	IsMember             bool                               `json:"is_member"`
	LastReadSeq          int64                              `json:"last_read_seq"`
	UnreadCount          int64                              `json:"unread_count"`
	Members              []ConversationMemberResponse       `json:"members"`
	WorkingAgents        []ConversationWorkingAgentResponse `json:"working_agents,omitempty"`
	RootMessage          *ConversationMessageResponse       `json:"root_message,omitempty"`
}

type ConversationThreadSummary struct {
	ID          string  `json:"id"`
	ReplyCount  int64   `json:"reply_count"`
	LastReplyAt *string `json:"last_reply_at"`
}

type ConversationMessageResponse struct {
	ID              string                     `json:"id"`
	ConversationID  string                     `json:"conversation_id"`
	Seq             int64                      `json:"seq"`
	AuthorType      string                     `json:"author_type"`
	AuthorID        *string                    `json:"author_id"`
	Kind            string                     `json:"kind"`
	Content         string                     `json:"content"`
	TaskID          *string                    `json:"task_id"`
	ClientMessageID *string                    `json:"client_message_id"`
	CreatedAt       string                     `json:"created_at"`
	Thread          *ConversationThreadSummary `json:"thread,omitempty"`
}

func conversationToResponse(c db.Conversation) ConversationResponse {
	return ConversationResponse{
		ID:                   uuidToString(c.ID),
		WorkspaceID:          uuidToString(c.WorkspaceID),
		Kind:                 c.Kind,
		Name:                 c.Name,
		About:                c.About,
		Visibility:           c.Visibility,
		ParentConversationID: uuidToPtr(c.ParentConversationID),
		RootMessageID:        uuidToPtr(c.RootMessageID),
		CreatedBy:            uuidToString(c.CreatedBy),
		LastMessageSeq:       c.LastMessageSeq,
		LastMessageAt:        timestampToPtr(c.LastMessageAt),
		ArchivedAt:           timestampToPtr(c.ArchivedAt),
		CreatedAt:            timestampToString(c.CreatedAt),
		UpdatedAt:            timestampToString(c.UpdatedAt),
		Members:              []ConversationMemberResponse{},
	}
}

func conversationMessageToResponse(m db.ConversationMessage) ConversationMessageResponse {
	return ConversationMessageResponse{
		ID:              uuidToString(m.ID),
		ConversationID:  uuidToString(m.ConversationID),
		Seq:             m.Seq,
		AuthorType:      m.AuthorType,
		AuthorID:        uuidToPtr(m.AuthorID),
		Kind:            m.Kind,
		Content:         m.Content,
		TaskID:          uuidToPtr(m.TaskID),
		ClientMessageID: textToPtr(m.ClientMessageID),
		CreatedAt:       timestampToString(m.CreatedAt),
	}
}

func conversationMembersToResponse(rows []db.ConversationMember) []ConversationMemberResponse {
	out := make([]ConversationMemberResponse, 0, len(rows))
	for _, m := range rows {
		out = append(out, ConversationMemberResponse{MemberType: m.MemberType, MemberID: uuidToString(m.MemberID)})
	}
	return out
}

// directConversationKey names the one direct conversation between two
// members, whichever of them opens it.
func directConversationKey(a, b string) string {
	ids := []string{a, b}
	sort.Strings(ids)
	return ids[0] + ":" + ids[1]
}

// conversationAccessRoot is the conversation whose membership governs c.
func (h *Handler) conversationAccessRoot(ctx context.Context, c db.Conversation) (db.Conversation, error) {
	if c.Kind == conversationKindThread {
		return h.Queries.GetConversation(ctx, c.ParentConversationID)
	}
	return c, nil
}

// canAccessConversation is the only access rule for a conversation. It
// returns the governing conversation (the parent for a thread) and whether
// the user is a member of it.
func (h *Handler) canAccessConversation(ctx context.Context, c db.Conversation, userID pgtype.UUID) (db.Conversation, bool, bool) {
	root, err := h.conversationAccessRoot(ctx, c)
	if err != nil {
		return db.Conversation{}, false, false
	}
	if _, err := h.Queries.GetConversationMember(ctx, db.GetConversationMemberParams{
		ConversationID: root.ID, MemberType: "member", MemberID: userID,
	}); err == nil {
		return root, true, true
	}
	if root.Kind == conversationKindChannel && root.Visibility == "public" {
		return root, false, true
	}
	return db.Conversation{}, false, false
}

type conversationRequest struct {
	userID      string
	workspaceID string
	conv        db.Conversation
	root        db.Conversation
	isMember    bool
}

func (h *Handler) loadConversationForRequest(w http.ResponseWriter, r *http.Request) (conversationRequest, bool) {
	if !h.requireTeamPage(w, r) {
		return conversationRequest{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return conversationRequest{}, false
	}
	workspaceID := ctxWorkspaceID(r.Context())
	convID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "conversationId"), "conversation id")
	if !ok {
		return conversationRequest{}, false
	}
	conv, err := h.Queries.GetConversationInWorkspace(r.Context(), db.GetConversationInWorkspaceParams{
		ID: convID, WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return conversationRequest{}, false
	}
	root, isMember, ok := h.canAccessConversation(r.Context(), conv, parseUUID(userID))
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found")
		return conversationRequest{}, false
	}
	return conversationRequest{userID: userID, workspaceID: workspaceID, conv: conv, root: root, isMember: isMember}, true
}

// conversationRecipients decides who hears about activity in a conversation.
func (h *Handler) conversationRecipients(ctx context.Context, root db.Conversation) (public bool, userIDs []string) {
	if root.Kind == conversationKindChannel && root.Visibility == "public" {
		return true, nil
	}
	members, err := h.Queries.ListConversationMembers(ctx, root.ID)
	if err != nil {
		slog.Warn("list conversation members for realtime failed", "conversation_id", uuidToString(root.ID), "error", err)
		return false, nil
	}
	for _, m := range members {
		if m.MemberType == "member" {
			userIDs = append(userIDs, uuidToString(m.MemberID))
		}
	}
	return false, userIDs
}

func (h *Handler) publishConversationEvent(ctx context.Context, eventType string, conv, root db.Conversation, actorType, actorID string, message any) {
	public, recipients := h.conversationRecipients(ctx, root)
	payload := protocol.ConversationEventPayload{
		ConversationID: uuidToString(conv.ID),
		Message:        message,
		RecipientIDs:   recipients,
		Public:         public,
	}
	if conv.ParentConversationID.Valid {
		payload.ParentConversationID = uuidToString(conv.ParentConversationID)
	}
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: uuidToString(conv.WorkspaceID),
		ActorType:   actorType,
		ActorID:     actorID,
		Payload:     payload,
	})
}

// ---------------------------------------------------------------------------
// List / create / open
// ---------------------------------------------------------------------------

func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	if !h.requireTeamPage(w, r) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	rows, err := h.Queries.ListConversationsForMember(r.Context(), db.ListConversationsForMemberParams{
		UserID: parseUUID(userID), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list conversations")
		return
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	membersByConv := map[string][]db.ConversationMember{}
	if len(ids) > 0 {
		members, err := h.Queries.ListConversationMembersFor(r.Context(), ids)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list conversation members")
			return
		}
		for _, m := range members {
			key := uuidToString(m.ConversationID)
			membersByConv[key] = append(membersByConv[key], m)
		}
	}
	out := make([]ConversationResponse, 0, len(rows))
	for _, row := range rows {
		resp := conversationToResponse(db.Conversation{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Kind: row.Kind, Name: row.Name, About: row.About,
			Visibility: row.Visibility, DirectKey: row.DirectKey, ParentConversationID: row.ParentConversationID,
			RootMessageID: row.RootMessageID, CreatedBy: row.CreatedBy, LastMessageSeq: row.LastMessageSeq,
			LastMessageAt: row.LastMessageAt, ArchivedAt: row.ArchivedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
		resp.IsMember = row.IsMember
		resp.LastReadSeq = row.MyLastReadSeq
		if row.IsMember {
			resp.UnreadCount = max(row.LastMessageSeq-row.MyLastReadSeq, 0)
		}
		resp.Members = conversationMembersToResponse(membersByConv[resp.ID])
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

type conversationMemberInput struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
}

type CreateConversationRequest struct {
	Name       string                    `json:"name"`
	About      string                    `json:"about"`
	Visibility string                    `json:"visibility"`
	Members    []conversationMemberInput `json:"members"`
}

// normalizeChannelName keeps a channel name to one readable token, the way
// people type it after "#".
func normalizeChannelName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "#")))
	name = strings.Join(strings.Fields(name), "-")
	return name
}

// validateConversationMembers checks every input names a member or an
// unarchived agent of the workspace.
func (h *Handler) validateConversationMembers(ctx context.Context, workspaceID string, inputs []conversationMemberInput) ([]conversationMemberInput, string) {
	seen := map[string]bool{}
	out := make([]conversationMemberInput, 0, len(inputs))
	for _, in := range inputs {
		id, err := parseUUIDStrict(in.MemberID)
		if err != nil {
			return nil, "invalid member id"
		}
		key := in.MemberType + ":" + uuidToString(id)
		if seen[key] {
			continue
		}
		seen[key] = true
		switch in.MemberType {
		case "member":
			if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID: id, WorkspaceID: parseUUID(workspaceID),
			}); err != nil {
				return nil, "member not found"
			}
		case "agent":
			agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: parseUUID(workspaceID)})
			if err != nil {
				return nil, "agent not found"
			}
			if agent.ArchivedAt.Valid {
				return nil, "agent is archived"
			}
		default:
			return nil, "member_type must be member or agent"
		}
		out = append(out, conversationMemberInput{MemberType: in.MemberType, MemberID: uuidToString(id)})
	}
	return out, ""
}

func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	if !h.requireTeamPage(w, r) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	var req CreateConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := normalizeChannelName(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		writeError(w, http.StatusBadRequest, "name is required (at most 80 characters)")
		return
	}
	about := strings.TrimSpace(req.About)
	if utf8.RuneCountInString(about) > 500 {
		writeError(w, http.StatusBadRequest, "about is at most 500 characters")
		return
	}
	visibility := req.Visibility
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" {
		writeError(w, http.StatusBadRequest, "visibility must be public or private")
		return
	}
	members, msg := h.validateConversationMembers(r.Context(), workspaceID, append(req.Members, conversationMemberInput{MemberType: "member", MemberID: userID}))
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	conv, err := qtx.CreateConversation(r.Context(), db.CreateConversationParams{
		WorkspaceID: parseUUID(workspaceID), Kind: conversationKindChannel, Name: name, About: about,
		Visibility: visibility, CreatedBy: parseUUID(userID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a channel with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}
	for _, m := range members {
		if err := qtx.AddConversationMember(r.Context(), db.AddConversationMemberParams{
			ConversationID: conv.ID, WorkspaceID: conv.WorkspaceID, MemberType: m.MemberType, MemberID: parseUUID(m.MemberID),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add channel member")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}
	resp := h.conversationDetail(r.Context(), conv, parseUUID(userID))
	h.publishConversationEvent(r.Context(), protocol.EventConversationUpdated, conv, conv, "member", userID, nil)
	writeJSON(w, http.StatusCreated, resp)
}

type OpenDirectConversationRequest struct {
	UserID string `json:"user_id"`
}

// OpenDirectConversation returns the one direct conversation between the
// caller and another member, creating it on first use.
func (h *Handler) OpenDirectConversation(w http.ResponseWriter, r *http.Request) {
	if !h.requireTeamPage(w, r) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	var req OpenDirectConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	members, msg := h.validateConversationMembers(r.Context(), workspaceID, []conversationMemberInput{
		{MemberType: "member", MemberID: userID}, {MemberType: "member", MemberID: req.UserID},
	})
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	other := members[len(members)-1].MemberID
	key := directConversationKey(userID, other)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	conv, err := qtx.CreateDirectConversation(r.Context(), db.CreateDirectConversationParams{
		WorkspaceID: parseUUID(workspaceID), DirectKey: pgtype.Text{String: key, Valid: true}, CreatedBy: parseUUID(userID),
	})
	created := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		tx.Rollback(r.Context())
		conv, err = h.Queries.GetDirectConversation(r.Context(), db.GetDirectConversationParams{
			WorkspaceID: parseUUID(workspaceID), DirectKey: pgtype.Text{String: key, Valid: true},
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open direct conversation")
		return
	}
	if created {
		for _, m := range members {
			if err := qtx.AddConversationMember(r.Context(), db.AddConversationMemberParams{
				ConversationID: conv.ID, WorkspaceID: conv.WorkspaceID, MemberType: "member", MemberID: parseUUID(m.MemberID),
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to open direct conversation")
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to open direct conversation")
			return
		}
		h.publishConversationEvent(r.Context(), protocol.EventConversationUpdated, conv, conv, "member", userID, nil)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, h.conversationDetail(r.Context(), conv, parseUUID(userID)))
}

// conversationDetail is the full response for one conversation as the user
// sees it: membership, read state, members, agents replying right now and,
// for a thread, the message it replies to.
func (h *Handler) conversationDetail(ctx context.Context, conv db.Conversation, userID pgtype.UUID) ConversationResponse {
	resp := conversationToResponse(conv)
	if member, err := h.Queries.GetConversationMember(ctx, db.GetConversationMemberParams{
		ConversationID: conv.ID, MemberType: "member", MemberID: userID,
	}); err == nil {
		resp.IsMember = true
		resp.LastReadSeq = member.LastReadSeq
		resp.UnreadCount = max(conv.LastMessageSeq-member.LastReadSeq, 0)
	}
	if members, err := h.Queries.ListConversationMembers(ctx, conv.ID); err == nil {
		resp.Members = conversationMembersToResponse(members)
	}
	if working, err := h.Queries.ListConversationWorkingAgents(ctx, conv.ID); err == nil {
		for _, row := range working {
			resp.WorkingAgents = append(resp.WorkingAgents, ConversationWorkingAgentResponse{
				AgentID: uuidToString(row.AgentID), TaskID: uuidToString(row.TaskID),
			})
		}
	}
	if conv.RootMessageID.Valid {
		if root, err := h.Queries.GetConversationMessage(ctx, conv.RootMessageID); err == nil {
			m := conversationMessageToResponse(root)
			resp.RootMessage = &m
		}
	}
	return resp
}

func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.conversationDetail(r.Context(), cr.conv, parseUUID(cr.userID)))
}

type UpdateConversationRequest struct {
	Name     *string `json:"name"`
	About    *string `json:"about"`
	Archived *bool   `json:"archived"`
}

func (h *Handler) UpdateConversation(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	if cr.conv.Kind != conversationKindChannel || !cr.isMember {
		writeError(w, http.StatusForbidden, "only channel members can edit a channel")
		return
	}
	var req UpdateConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateConversationParams{ID: cr.conv.ID, WorkspaceID: cr.conv.WorkspaceID}
	if req.Name != nil {
		name := normalizeChannelName(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > 80 {
			writeError(w, http.StatusBadRequest, "name is required (at most 80 characters)")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.About != nil {
		about := strings.TrimSpace(*req.About)
		if utf8.RuneCountInString(about) > 500 {
			writeError(w, http.StatusBadRequest, "about is at most 500 characters")
			return
		}
		params.About = pgtype.Text{String: about, Valid: true}
	}
	if req.Archived != nil {
		params.Archived = pgtype.Bool{Bool: *req.Archived, Valid: true}
	}
	conv, err := h.Queries.UpdateConversation(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a channel with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	h.publishConversationEvent(r.Context(), protocol.EventConversationUpdated, conv, conv, "member", cr.userID, nil)
	writeJSON(w, http.StatusOK, h.conversationDetail(r.Context(), conv, parseUUID(cr.userID)))
}

type AddConversationMembersRequest struct {
	Members []conversationMemberInput `json:"members"`
}

// AddConversationMembers adds people or agents to a channel. Anyone may join
// a public channel themselves; adding others, or joining a private channel,
// takes an existing member.
func (h *Handler) AddConversationMembers(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	if cr.conv.Kind != conversationKindChannel {
		writeError(w, http.StatusBadRequest, "only channels have editable members")
		return
	}
	if cr.conv.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "channel is archived")
		return
	}
	var req AddConversationMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	members, msg := h.validateConversationMembers(r.Context(), cr.workspaceID, req.Members)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	selfJoinOnly := len(members) == 1 && members[0].MemberType == "member" && members[0].MemberID == cr.userID
	if !cr.isMember && !selfJoinOnly {
		writeError(w, http.StatusForbidden, "join the channel to add others")
		return
	}
	for _, m := range members {
		if err := h.Queries.AddConversationMember(r.Context(), db.AddConversationMemberParams{
			ConversationID: cr.conv.ID, WorkspaceID: cr.conv.WorkspaceID, MemberType: m.MemberType, MemberID: parseUUID(m.MemberID),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add channel member")
			return
		}
	}
	h.publishConversationEvent(r.Context(), protocol.EventConversationUpdated, cr.conv, cr.conv, "member", cr.userID, nil)
	writeJSON(w, http.StatusOK, h.conversationDetail(r.Context(), cr.conv, parseUUID(cr.userID)))
}

// RemoveConversationMember removes someone from a channel; members may
// remove themselves (leave) or anyone else.
func (h *Handler) RemoveConversationMember(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	if cr.conv.Kind != conversationKindChannel || !cr.isMember {
		writeError(w, http.StatusForbidden, "only channel members can change members")
		return
	}
	memberType := chi.URLParam(r, "memberType")
	if memberType != "member" && memberType != "agent" {
		writeError(w, http.StatusBadRequest, "member_type must be member or agent")
		return
	}
	memberID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member id")
	if !ok {
		return
	}
	// Tell everyone who could see the channel before the removal, including
	// the person removed, so their list drops it.
	public, recipients := h.conversationRecipients(r.Context(), cr.conv)
	if err := h.Queries.RemoveConversationMember(r.Context(), db.RemoveConversationMemberParams{
		ConversationID: cr.conv.ID, MemberType: memberType, MemberID: memberID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove channel member")
		return
	}
	h.Bus.Publish(events.Event{
		Type: protocol.EventConversationUpdated, WorkspaceID: cr.workspaceID, ActorType: "member", ActorID: cr.userID,
		Payload: protocol.ConversationEventPayload{ConversationID: uuidToString(cr.conv.ID), RecipientIDs: recipients, Public: public},
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

func (h *Handler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	limit := conversationPageDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = min(n, conversationPageMax)
		}
	}
	params := db.ListConversationMessagesParams{ConversationID: cr.conv.ID, PageLimit: int32(limit + 1)}
	if raw := r.URL.Query().Get("before_seq"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before_seq")
			return
		}
		params.BeforeSeq = pgtype.Int8{Int64: n, Valid: true}
	}
	rows, err := h.Queries.ListConversationMessages(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]ConversationMessageResponse, len(rows))
	rootIDs := make([]pgtype.UUID, 0, len(rows))
	for i, row := range rows {
		out[len(rows)-1-i] = conversationMessageToResponse(row)
		rootIDs = append(rootIDs, row.ID)
	}
	if cr.conv.Kind != conversationKindThread && len(rootIDs) > 0 {
		threads, err := h.Queries.ListConversationThreadsByRoots(r.Context(), db.ListConversationThreadsByRootsParams{
			ParentID: cr.conv.ID, RootIds: rootIDs,
		})
		if err == nil {
			byRoot := map[string]ConversationThreadSummary{}
			for _, t := range threads {
				byRoot[uuidToString(t.RootMessageID)] = ConversationThreadSummary{
					ID: uuidToString(t.ID), ReplyCount: t.LastMessageSeq, LastReplyAt: timestampToPtr(t.LastMessageAt),
				}
			}
			for i := range out {
				if t, ok := byRoot[out[i].ID]; ok {
					out[i].Thread = &t
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "has_more": hasMore})
}

type SendConversationMessageRequest struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"client_message_id"`
}

func (h *Handler) SendConversationMessage(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	var req SendConversationMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if utf8.RuneCountInString(content) > conversationMessageMaxRunes {
		writeError(w, http.StatusBadRequest, "message is too long")
		return
	}
	if cr.root.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "channel is archived")
		return
	}
	userUUID := parseUUID(cr.userID)
	clientID := strings.TrimSpace(req.ClientMessageID)
	if clientID != "" {
		if existing, err := h.Queries.GetConversationMessageByClientID(r.Context(), db.GetConversationMessageByClientIDParams{
			ConversationID: cr.conv.ID, AuthorID: userUUID, ClientMessageID: pgtype.Text{String: clientID, Valid: true},
		}); err == nil {
			writeJSON(w, http.StatusOK, conversationMessageToResponse(existing))
			return
		}
	}
	// Posting in a public channel joins it, as in any chat app.
	if !cr.isMember {
		if err := h.Queries.AddConversationMember(r.Context(), db.AddConversationMemberParams{
			ConversationID: cr.root.ID, WorkspaceID: cr.root.WorkspaceID, MemberType: "member", MemberID: userUUID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to join channel")
			return
		}
	}
	// A thread's followers are the people who replied in it; their row holds
	// the thread's read cursor.
	if cr.conv.Kind == conversationKindThread {
		if err := h.Queries.AddConversationMember(r.Context(), db.AddConversationMemberParams{
			ConversationID: cr.conv.ID, WorkspaceID: cr.conv.WorkspaceID, MemberType: "member", MemberID: userUUID,
		}); err != nil {
			slog.Warn("follow conversation thread failed", "conversation_id", uuidToString(cr.conv.ID), "error", err)
		}
	}
	msg, err := h.postConversationMessage(r.Context(), cr.conv, cr.root, "member", cr.userID, "message", content, pgtype.UUID{}, clientID)
	if err != nil {
		if isUniqueViolation(err) && clientID != "" {
			if existing, gerr := h.Queries.GetConversationMessageByClientID(r.Context(), db.GetConversationMessageByClientIDParams{
				ConversationID: cr.conv.ID, AuthorID: userUUID, ClientMessageID: pgtype.Text{String: clientID, Valid: true},
			}); gerr == nil {
				writeJSON(w, http.StatusOK, conversationMessageToResponse(existing))
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}
	if err := h.Queries.MarkConversationRead(r.Context(), db.MarkConversationReadParams{
		Seq: msg.Seq, ConversationID: cr.conv.ID, UserID: userUUID,
	}); err != nil {
		slog.Warn("mark own conversation message read failed", "conversation_id", uuidToString(cr.conv.ID), "error", err)
	}
	h.triggerConversationAgents(r.Context(), cr.conv, cr.root, msg, "member", cr.userID, cr.userID)
	writeJSON(w, http.StatusCreated, conversationMessageToResponse(msg))
}

// postConversationMessage appends one message under the conversation's seq
// lock and tells everyone who can read it.
func (h *Handler) postConversationMessage(ctx context.Context, conv, root db.Conversation, authorType, authorID, kind, content string, taskID pgtype.UUID, clientID string) (db.ConversationMessage, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ConversationMessage{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	seq, err := qtx.NextConversationMessageSeq(ctx, conv.ID)
	if err != nil {
		return db.ConversationMessage{}, err
	}
	params := db.CreateConversationMessageParams{
		ConversationID: conv.ID, WorkspaceID: conv.WorkspaceID, Seq: seq,
		AuthorType: authorType, Kind: kind, Content: content, TaskID: taskID,
	}
	if authorID != "" {
		params.AuthorID = parseUUID(authorID)
	}
	if clientID != "" {
		params.ClientMessageID = pgtype.Text{String: clientID, Valid: true}
	}
	msg, err := qtx.CreateConversationMessage(ctx, params)
	if err != nil {
		return db.ConversationMessage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ConversationMessage{}, err
	}
	resp := conversationMessageToResponse(msg)
	h.publishConversationEvent(ctx, protocol.EventConversationMessage, conv, root, authorType, authorID, resp)
	return msg, nil
}

// postConversationEvent writes a system line, e.g. why an agent did not reply.
func (h *Handler) postConversationEvent(ctx context.Context, conv, root db.Conversation, content string) {
	if _, err := h.postConversationMessage(ctx, conv, root, "system", "", "event", content, pgtype.UUID{}, ""); err != nil {
		slog.Warn("post conversation event failed", "conversation_id", uuidToString(conv.ID), "error", err)
	}
}

type MarkConversationReadRequest struct {
	Seq int64 `json:"seq"`
}

func (h *Handler) MarkConversationRead(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	var req MarkConversationReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.Queries.MarkConversationRead(r.Context(), db.MarkConversationReadParams{
		Seq: req.Seq, ConversationID: cr.conv.ID, UserID: parseUUID(cr.userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type OpenConversationThreadRequest struct {
	RootMessageID string `json:"root_message_id"`
}

// OpenConversationThread returns the thread under a message, creating it on
// first use.
func (h *Handler) OpenConversationThread(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.loadConversationForRequest(w, r)
	if !ok {
		return
	}
	if cr.conv.Kind == conversationKindThread {
		writeError(w, http.StatusBadRequest, "threads cannot have threads")
		return
	}
	var req OpenConversationThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rootID, ok := parseUUIDOrBadRequest(w, req.RootMessageID, "root_message_id")
	if !ok {
		return
	}
	root, err := h.Queries.GetConversationMessage(r.Context(), rootID)
	if err != nil || root.ConversationID != cr.conv.ID {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if root.Kind != "message" {
		writeError(w, http.StatusBadRequest, "only messages can have threads")
		return
	}
	thread, err := h.Queries.CreateConversationThread(r.Context(), db.CreateConversationThreadParams{
		WorkspaceID: cr.conv.WorkspaceID, Visibility: cr.conv.Visibility, ParentConversationID: cr.conv.ID,
		RootMessageID: rootID, CreatedBy: parseUUID(cr.userID),
	})
	status := http.StatusCreated
	if errors.Is(err, pgx.ErrNoRows) {
		status = http.StatusOK
		thread, err = h.Queries.GetConversationThreadByRoot(r.Context(), rootID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open thread")
		return
	}
	writeJSON(w, status, h.conversationDetail(r.Context(), thread, parseUUID(cr.userID)))
}
