package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type chatSessionPageCursor struct {
	Pin      time.Time `json:"pin"`
	Activity time.Time `json:"activity"`
	ID       string    `json:"id"`
	Status   string    `json:"status"`
}

type ChatSessionsPageResponse struct {
	Sessions   []ChatSessionResponse `json:"sessions"`
	NextCursor *string               `json:"next_cursor"`
	HasMore    bool                  `json:"has_more"`
}

type ChatSessionSummaryResponse struct {
	ActiveCount     int            `json:"active_count"`
	ArchivedCount   int            `json:"archived_count"`
	UnreadBySession map[string]int `json:"unread_by_session"`
}

func parseChatSessionPage(r *http.Request) (db.ListChatSessionsPageParams, error) {
	p := db.ListChatSessionsPageParams{Status: "active", PageLimit: 51}
	if status := r.URL.Query().Get("status"); status != "" {
		p.Status = status
	}
	if p.Status != "active" && p.Status != "archived" {
		return p, fmt.Errorf("status must be active or archived")
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return p, fmt.Errorf("limit must be between 1 and 100")
		}
		p.PageLimit = int32(n + 1)
	}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if len(raw) > 1024 {
			return p, fmt.Errorf("invalid cursor")
		}
		data, err := base64.RawURLEncoding.DecodeString(raw)
		var cursor chatSessionPageCursor
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Pin.IsZero() || cursor.Activity.IsZero() || cursor.Status != p.Status {
			return p, fmt.Errorf("invalid cursor")
		}
		id, err := util.ParseUUID(cursor.ID)
		if err != nil {
			return p, fmt.Errorf("invalid cursor")
		}
		p.HasCursor = true
		p.CursorID = id
		p.CursorPin = pgtype.Timestamptz{Time: cursor.Pin, Valid: true}
		p.CursorActivity = pgtype.Timestamptz{Time: cursor.Activity, Valid: true}
	}
	return p, nil
}

// Match the legacy list's workspace, creator, and private-agent boundary.
// Filtering before LIMIT prevents inaccessible rows from consuming a page.
func (h *Handler) chatSessionListScope(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, []pgtype.UUID, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, nil, false
	}
	workspaceID := ctxWorkspaceID(r.Context())
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, nil, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return pgtype.UUID{}, pgtype.UUID{}, nil, false
	}
	agents := make([]pgtype.UUID, 0, len(allowed))
	for id := range allowed {
		agents = append(agents, parseUUID(id))
	}
	return parseUUID(workspaceID), parseUUID(userID), agents, true
}

func (h *Handler) ListChatSessionsPage(w http.ResponseWriter, r *http.Request) {
	p, err := parseChatSessionPage(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workspace, creator, agents, ok := h.chatSessionListScope(w, r)
	if !ok {
		return
	}
	p.WorkspaceID, p.CreatorID, p.AgentIds = workspace, creator, agents
	rows, err := h.Queries.ListChatSessionsPage(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
		return
	}
	response := ChatSessionsPageResponse{Sessions: make([]ChatSessionResponse, 0, len(rows)), HasMore: len(rows) == int(p.PageLimit)}
	if response.HasMore {
		rows = rows[:len(rows)-1]
	}
	for _, row := range rows {
		response.Sessions = append(response.Sessions, ChatSessionResponse{
			ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), AgentID: uuidToString(row.AgentID),
			CreatorID: uuidToString(row.CreatorID), ProjectID: uuidToPtr(row.ProjectID), Title: row.Title, Status: row.Status,
			HasUnread: row.UnreadCount > 0, UnreadCount: int(row.UnreadCount), Pinned: row.PinnedAt.Valid,
			LastMessage: buildChatLastMessage(row.LastMessageAt, row.LastMessageContent, row.LastMessageRole, row.LastMessageFailureReason, row.LastMessageKind),
			CreatedAt:   timestampToString(row.CreatedAt), UpdatedAt: timestampToString(row.UpdatedAt),
		})
	}
	if response.HasMore {
		last := rows[len(rows)-1]
		data, err := json.Marshal(chatSessionPageCursor{Pin: last.SortPin.Time, Activity: last.SortActivity.Time, ID: uuidToString(last.ID), Status: p.Status})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode cursor")
			return
		}
		cursor := base64.RawURLEncoding.EncodeToString(data)
		response.NextCursor = &cursor
	}
	if err := h.hydrateChatSessionChannelMetadata(r.Context(), response.Sessions); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat channel metadata")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetChatSessionSummary(w http.ResponseWriter, r *http.Request) {
	workspace, creator, agents, ok := h.chatSessionListScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListChatSessionSummary(r.Context(), db.ListChatSessionSummaryParams{WorkspaceID: workspace, CreatorID: creator, AgentIds: agents})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat summary")
		return
	}
	response := ChatSessionSummaryResponse{UnreadBySession: make(map[string]int)}
	for _, row := range rows {
		if row.Status == "archived" {
			response.ArchivedCount++
			continue
		}
		response.ActiveCount++
		if row.UnreadCount > 0 {
			response.UnreadBySession[uuidToString(row.ID)] = int(row.UnreadCount)
		}
	}
	writeJSON(w, http.StatusOK, response)
}
