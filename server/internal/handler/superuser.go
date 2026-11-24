package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workspaceSuperuserGrant struct {
	MemberID string `json:"member_id"`
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// A superuser grant belongs to a membership, not a role. Every read checks
// current membership in the database so removal and guest demotion take effect
// without waiting for a cache to expire.
func (h *Handler) humanSuperuser(ctx context.Context, r *http.Request, workspaceID, userID string) (bool, error) {
	source := r.Header.Get("X-Actor-Source")
	principal, delegated := middleware.FullDelegationFromContext(r.Context())
	if source != "" && !(source == "task_token" && delegated && principal.GrantorUserID == userID) {
		return false, nil
	}
	return h.Queries.HasActiveWorkspaceSuperuserGrant(ctx, db.HasActiveWorkspaceSuperuserGrantParams{
		WorkspaceID: parseUUID(workspaceID), UserID: parseUUID(userID),
	})
}

func (h *Handler) ListWorkspaceSuperusers(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	var enabled bool
	if err := h.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM workspace_superuser_policy WHERE workspace_id = $1)`, parseUUID(workspaceID)).Scan(&enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load superuser policy")
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT m.id, m.user_id, u.name, u.email
		FROM workspace_superuser_grant g JOIN member m ON m.id = g.member_id
		JOIN "user" u ON u.id = m.user_id
		WHERE g.workspace_id = $1 AND m.workspace_id = g.workspace_id AND m.user_id = g.user_id AND m.role <> 'guest'
		ORDER BY g.granted_at`, parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load superuser grants")
		return
	}
	defer rows.Close()
	grants := make([]workspaceSuperuserGrant, 0)
	for rows.Next() {
		var memberID, userID pgtype.UUID
		var grant workspaceSuperuserGrant
		if err := rows.Scan(&memberID, &userID, &grant.Name, &grant.Email); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load superuser grants")
			return
		}
		grant.MemberID, grant.UserID = uuidToString(memberID), uuidToString(userID)
		grants = append(grants, grant)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load superuser grants")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "grants": grants})
}

func (h *Handler) ChangeWorkspaceSuperuser(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if requester.Role != "owner" || h.resolveActorType(r, uuidToString(requester.UserID), workspaceID) != "member" {
		writeError(w, http.StatusForbidden, "workspace owner required")
		return
	}
	memberID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change superuser grant")
		return
	}
	defer tx.Rollback(r.Context())
	var requesterRole string
	if err := tx.QueryRow(r.Context(), `SELECT role FROM member WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, requester.ID, requester.WorkspaceID).Scan(&requesterRole); err != nil || requesterRole != "owner" {
		writeError(w, http.StatusForbidden, "workspace owner required")
		return
	}
	var userID pgtype.UUID
	var role string
	err = tx.QueryRow(r.Context(), `SELECT user_id, role FROM member WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, memberID, requester.WorkspaceID).Scan(&userID, &role)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change superuser grant")
		return
	}
	if r.Method == http.MethodPut && role == "guest" {
		writeError(w, http.StatusBadRequest, "guest cannot be superuser")
		return
	}
	var action string
	var changed, policyEnabled bool
	if r.Method == http.MethodPut {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(r.Context(), `INSERT INTO workspace_superuser_policy (workspace_id, enabled_by) VALUES ($1, $2) ON CONFLICT DO NOTHING`, requester.WorkspaceID, requester.UserID)
		policyEnabled = tag.RowsAffected() > 0
		if err == nil {
			tag, err = tx.Exec(r.Context(), `INSERT INTO workspace_superuser_grant (member_id, workspace_id, user_id, granted_by)
				VALUES ($1, $2, $3, $4) ON CONFLICT (member_id) DO NOTHING`, memberID, requester.WorkspaceID, userID, requester.UserID)
			changed = tag.RowsAffected() > 0
		}
		action = "grant"
	} else {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(r.Context(), `DELETE FROM workspace_superuser_grant WHERE member_id = $1 AND workspace_id = $2`, memberID, requester.WorkspaceID)
		changed = tag.RowsAffected() > 0
		action = "revoke"
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change superuser grant")
		return
	}
	if changed {
		if _, err = tx.Exec(r.Context(), `INSERT INTO workspace_superuser_audit (workspace_id, actor_user_id, target_user_id, action)
		VALUES ($1, $2, $3, $4)`, requester.WorkspaceID, requester.UserID, userID, action); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to audit superuser grant")
			return
		}
	}
	if policyEnabled {
		_, err = tx.Exec(r.Context(), `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title, body)
			SELECT m.workspace_id, 'member', m.user_id, 'workspace_superuser_policy', 'Workspace superuser access enabled',
			'Trusted superusers may manage workspace resources, including private projects and agents, inspect past and future member chats, and delete the workspace.'
			FROM member m WHERE m.workspace_id = $1`, requester.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to notify members")
			return
		}
	}
	if changed && action == "grant" {
		_, err = tx.Exec(r.Context(), `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title, body)
			SELECT m.workspace_id, 'member', m.user_id, 'workspace_superuser_grant', 'Workspace superuser granted',
			(SELECT name FROM "user" WHERE id = $2) || ' can now manage workspace resources, inspect all member chats, and delete the workspace.'
			FROM member m WHERE m.workspace_id = $1`, requester.WorkspaceID, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to notify members")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change superuser grant")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"granted": action == "grant"})
}

func (h *Handler) resolveActorType(r *http.Request, userID, workspaceID string) string {
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	return actorType
}

func (h *Handler) auditSuperuserChatRead(r *http.Request, workspaceID, actorUserID, targetUserID, sessionID string) error {
	if actorUserID == targetUserID {
		return nil
	}
	var agentID, taskID any
	if principal, ok := middleware.FullDelegationFromContext(r.Context()); ok {
		agentID, taskID = parseUUID(principal.AgentID), parseUUID(principal.TaskID)
	}
	_, err := h.DB.Exec(r.Context(), `INSERT INTO workspace_superuser_audit
		(workspace_id, actor_user_id, actor_agent_id, actor_task_id, target_user_id, chat_session_id, action)
		VALUES ($1, $2, $3, $4, $5, $6, 'read_chat')`,
		parseUUID(workspaceID), parseUUID(actorUserID), agentID, taskID, parseUUID(targetUserID), parseUUID(sessionID))
	return err
}

// Superuser chat endpoints are read-only. Ordinary chat update, send, archive,
// delete and read-cursor handlers retain the creator gate.
func (h *Handler) superuserChat(w http.ResponseWriter, r *http.Request) (db.ChatSession, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.ChatSession{}, false
	}
	workspaceID := ctxWorkspaceID(r.Context())
	allowed, err := h.humanSuperuser(r.Context(), r, workspaceID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check superuser grant")
		return db.ChatSession{}, false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "superuser grant required")
		return db.ChatSession{}, false
	}
	sessionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "chat session id")
	if !ok {
		return db.ChatSession{}, false
	}
	session, err := h.Queries.GetPublicChatSessionInWorkspace(r.Context(), db.GetPublicChatSessionInWorkspaceParams{ID: sessionID, WorkspaceID: parseUUID(workspaceID)})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return db.ChatSession{}, false
	}
	if session.CreatorID != parseUUID(userID) {
		if err := h.auditSuperuserChatRead(r, workspaceID, userID, uuidToString(session.CreatorID), uuidToString(session.ID)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to audit chat read")
			return db.ChatSession{}, false
		}
	}
	return session, true
}

// readableChatSession preserves the creator's existing private-agent gate.
// Only a foreign session uses the explicit, audited superuser read path.
func (h *Handler) readableChatSession(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) (db.ChatSession, bool) {
	id, ok := parseUUIDOrBadRequest(w, sessionID, "chat session id")
	if !ok {
		return db.ChatSession{}, false
	}
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID: id, WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return db.ChatSession{}, false
	}
	if uuidToString(session.CreatorID) == userID {
		return h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	}
	return h.superuserChat(w, r)
}

func (h *Handler) GetSuperuserChat(w http.ResponseWriter, r *http.Request) {
	session, ok := h.superuserChat(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, chatSessionToResponse(session))
}

func (h *Handler) ListSuperuserChatMessages(w http.ResponseWriter, r *http.Request) {
	session, ok := h.superuserChat(w, r)
	if !ok {
		return
	}
	messages, err := h.Queries.ListChatMessages(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat messages")
		return
	}
	messages = visibleChatMessages(messages)
	messageIDs := make([]pgtype.UUID, len(messages))
	for i, message := range messages {
		messageIDs[i] = message.ID
	}
	attachments := h.groupChatMessageAttachments(r.Context(), uuidToString(session.WorkspaceID), messageIDs)
	resp := make([]ChatMessageResponse, len(messages))
	for i, message := range messages {
		resp[i] = chatMessageToResponse(message, attachments[uuidToString(message.ID)])
	}
	writeJSON(w, http.StatusOK, resp)
}

type superuserChatSummary struct {
	ID        string `json:"id"`
	CreatorID string `json:"creator_id"`
	AgentID   string `json:"agent_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

func (h *Handler) ListSuperuserChats(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	allowed, err := h.humanSuperuser(r.Context(), r, workspaceID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check superuser grant")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "superuser grant required")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 200 {
		writeError(w, http.StatusBadRequest, "search query too long")
		return
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 1_000_000 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		offset = parsed
	}
	rows, err := h.DB.Query(r.Context(), `SELECT cs.id, cs.creator_id, cs.agent_id, cs.title, cs.status, cs.updated_at
		FROM chat_session cs WHERE cs.workspace_id = $1
		AND (cs.explicitly_created_at IS NOT NULL OR EXISTS (
			SELECT 1 FROM chat_message public_message WHERE public_message.chat_session_id = cs.id AND public_message.message_kind != 'channel_command'))
		AND ($2 = '' OR cs.title ILIKE '%' || $2 || '%' OR EXISTS (
			SELECT 1 FROM chat_message m WHERE m.chat_session_id = cs.id AND m.message_kind != 'channel_command' AND m.content ILIKE '%' || $2 || '%'))
		ORDER BY cs.updated_at DESC, cs.id DESC LIMIT 100 OFFSET $3`, parseUUID(workspaceID), query, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chats")
		return
	}
	defer rows.Close()
	result := make([]superuserChatSummary, 0)
	for rows.Next() {
		var id, creator, agent pgtype.UUID
		var updated time.Time
		var item superuserChatSummary
		if err := rows.Scan(&id, &creator, &agent, &item.Title, &item.Status, &updated); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list chats")
			return
		}
		item.ID, item.CreatorID, item.AgentID, item.UpdatedAt = uuidToString(id), uuidToString(creator), uuidToString(agent), updated.Format(time.RFC3339)
		result = append(result, item)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chats")
		return
	}
	rows.Close()
	for _, item := range result {
		if item.CreatorID == userID {
			continue
		}
		if err := h.auditSuperuserChatRead(r, workspaceID, userID, item.CreatorID, item.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to audit chat list")
			return
		}
	}
	writeJSON(w, http.StatusOK, result)
}
