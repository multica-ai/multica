package handler

// Cerebro scheduled messages (FIR-2873). The queue owns only deferred delivery;
// actual delivery re-enters CreateComment so mentions, subscriptions, events,
// attachments, and agent triggers keep their canonical semantics.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type scheduledMessageResponse struct {
	ID            string    `json:"id"`
	IssueID       string    `json:"issue_id"`
	Content       string    `json:"content"`
	ParentID      *string   `json:"parent_id"`
	AttachmentIDs []string  `json:"attachment_ids"`
	SendAt        time.Time `json:"send_at"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type createScheduledMessageRequest struct {
	Content       string    `json:"content"`
	ParentID      *string   `json:"parent_id"`
	AttachmentIDs []string  `json:"attachment_ids"`
	SendAt        time.Time `json:"send_at"`
}

type updateScheduledMessageRequest struct {
	Content *string    `json:"content"`
	SendAt  *time.Time `json:"send_at"`
}

func (h *Handler) CreateScheduledMessage(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if issue.Kind != "channel" && issue.Kind != "dm" {
		writeError(w, http.StatusBadRequest, "scheduled messages are only available in Channels and DMs")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req createScheduledMessageRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if !req.SendAt.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "send_at must be in the future")
		return
	}
	parentID := any(nil)
	if req.ParentID != nil {
		parsed, valid := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !valid {
			return
		}
		parentID = parsed
	}
	attachments, valid := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !valid {
		return
	}
	row := h.DB.QueryRow(r.Context(), `
		INSERT INTO cerebro_scheduled_message (workspace_id, issue_id, author_user_id, content, parent_id, attachment_ids, send_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id::text, issue_id::text, content, parent_id::text, attachment_ids::text[], send_at, status, created_at`,
		issue.WorkspaceID, issue.ID, parseUUID(userID), req.Content, parentID, attachments, req.SendAt)
	var out scheduledMessageResponse
	if err := row.Scan(&out.ID, &out.IssueID, &out.Content, &out.ParentID, &out.AttachmentIDs, &out.SendAt, &out.Status, &out.CreatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to schedule message")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) ListScheduledMessages(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT id::text, issue_id::text, content, parent_id::text, attachment_ids::text[], send_at, status, created_at FROM cerebro_scheduled_message WHERE workspace_id=$1 AND issue_id=$2 AND author_user_id=$3 AND status IN ('pending','processing','failed') ORDER BY send_at`, issue.WorkspaceID, issue.ID, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list scheduled messages")
		return
	}
	defer rows.Close()
	out := []scheduledMessageResponse{}
	for rows.Next() {
		var item scheduledMessageResponse
		if err := rows.Scan(&item.ID, &item.IssueID, &item.Content, &item.ParentID, &item.AttachmentIDs, &item.SendAt, &item.Status, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list scheduled messages")
			return
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) UpdateScheduledMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	var req updateScheduledMessageRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content != nil && strings.TrimSpace(*req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.SendAt != nil && !req.SendAt.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "send_at must be in the future")
		return
	}
	row := h.DB.QueryRow(r.Context(), `UPDATE cerebro_scheduled_message SET content=COALESCE($1,content), send_at=COALESCE($2,send_at), status='pending', last_error=NULL, updated_at=now() WHERE id=$3 AND workspace_id=$4 AND author_user_id=$5 AND status IN ('pending','failed') RETURNING id::text,issue_id::text,content,parent_id::text,attachment_ids::text[],send_at,status,created_at`, req.Content, req.SendAt, parseUUID(chi.URLParam(r, "scheduledMessageId")), parseUUID(workspaceID), parseUUID(userID))
	var out scheduledMessageResponse
	if err := row.Scan(&out.ID, &out.IssueID, &out.Content, &out.ParentID, &out.AttachmentIDs, &out.SendAt, &out.Status, &out.CreatedAt); err != nil {
		writeError(w, http.StatusNotFound, "scheduled message not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) DeleteScheduledMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	tag, err := h.DB.Exec(r.Context(), `DELETE FROM cerebro_scheduled_message WHERE id=$1 AND workspace_id=$2 AND author_user_id=$3 AND status IN ('pending','failed')`, parseUUID(chi.URLParam(r, "scheduledMessageId")), parseUUID(h.resolveWorkspaceID(r)), parseUUID(userID))
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "scheduled message not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SendScheduledMessageNow(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	message, ok := h.claimScheduledMessage(r.Context(), `id=$1 AND workspace_id=$2 AND author_user_id=$3`, parseUUID(chi.URLParam(r, "scheduledMessageId")), parseUUID(h.resolveWorkspaceID(r)), parseUUID(userID))
	if !ok {
		writeError(w, http.StatusNotFound, "scheduled message not found")
		return
	}
	if err := h.deliverScheduledMessage(r.Context(), message); err != nil {
		writeError(w, http.StatusBadGateway, "scheduled message could not be sent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type claimedScheduledMessage struct {
	ID, WorkspaceID, IssueID, AuthorUserID, Content string
	ParentID                                        *string
	AttachmentIDs                                   []string
}

func (h *Handler) claimScheduledMessage(ctx context.Context, predicate string, args ...any) (claimedScheduledMessage, bool) {
	query := fmt.Sprintf(`UPDATE cerebro_scheduled_message SET status='processing',updated_at=now() WHERE id=(SELECT id FROM cerebro_scheduled_message WHERE %s AND (status IN ('pending','failed') OR (status='processing' AND updated_at < now() - interval '5 minutes')) FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id::text,workspace_id::text,issue_id::text,author_user_id::text,content,parent_id::text,attachment_ids::text[]`, predicate)
	var m claimedScheduledMessage
	err := h.DB.QueryRow(ctx, query, args...).Scan(&m.ID, &m.WorkspaceID, &m.IssueID, &m.AuthorUserID, &m.Content, &m.ParentID, &m.AttachmentIDs)
	return m, err == nil
}

func (h *Handler) deliverScheduledMessage(ctx context.Context, m claimedScheduledMessage) error {
	body, _ := json.Marshal(CreateCommentRequest{Content: m.Content, ParentID: m.ParentID, AttachmentIDs: m.AttachmentIDs})
	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+m.IssueID+"/comments", bytes.NewReader(body)).WithContext(ctx)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", m.IssueID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("X-User-ID", m.AuthorUserID)
	req.Header.Set("X-Workspace-ID", m.WorkspaceID)
	recorder := httptest.NewRecorder()
	h.CreateComment(recorder, req)
	if recorder.Code != http.StatusCreated {
		errText := strings.TrimSpace(recorder.Body.String())
		_, _ = h.DB.Exec(ctx, `UPDATE cerebro_scheduled_message SET status='failed',last_error=$2,updated_at=now() WHERE id=$1`, parseUUID(m.ID), errText)
		return fmt.Errorf("comment delivery returned %d: %s", recorder.Code, errText)
	}
	var created struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(recorder.Body.Bytes(), &created) != nil || created.ID == "" {
		_, _ = h.DB.Exec(ctx, `UPDATE cerebro_scheduled_message SET status='failed',last_error='invalid comment response',updated_at=now() WHERE id=$1`, parseUUID(m.ID))
		return fmt.Errorf("invalid comment response")
	}
	_, err := h.DB.Exec(ctx, `UPDATE cerebro_scheduled_message SET status='sent',sent_comment_id=$2,last_error=NULL,updated_at=now() WHERE id=$1`, parseUUID(m.ID), parseUUID(created.ID))
	return err
}

func (h *Handler) RunCerebroScheduledMessageSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 50; i++ {
				m, ok := h.claimScheduledMessage(ctx, "send_at <= now()")
				if !ok {
					break
				}
				if err := h.deliverScheduledMessage(ctx, m); err != nil {
					slog.Error("scheduled message delivery failed", "scheduled_message_id", m.ID, "error", err)
				}
			}
		}
	}
}
