package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const issueShareTokenHeader = "X-Issue-Share-Token"
const sharedIssueMessagePageSize = 200

type issueShareResponse struct {
	Token *string `json:"token"`
}

type sharedIssueComment struct {
	ID              string `json:"id"`
	AuthorType      string `json:"author_type"`
	AuthorName      string `json:"author_name,omitempty"`
	AuthorAvatarURL string `json:"author_avatar_url,omitempty"`
	ParentID        string `json:"parent_id,omitempty"`
	SourceTaskID    string `json:"source_task_id,omitempty"`
	Content         string `json:"content"`
	CreatedAt       string `json:"created_at"`
}

type sharedIssueRun struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at,omitempty"`
	CompletedAt    string `json:"completed_at"`
	AgentName      string `json:"agent_name,omitempty"`
	AgentAvatarURL string `json:"agent_avatar_url,omitempty"`
}

type sharedIssueDeliverable struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type sharedIssue struct {
	Identifier   string                   `json:"identifier"`
	Title        string                   `json:"title"`
	Description  *string                  `json:"description"`
	Status       string                   `json:"status"`
	StatusLabel  string                   `json:"status_label"`
	Comments     []sharedIssueComment     `json:"comments"`
	Runs         []sharedIssueRun         `json:"runs"`
	Deliverables []sharedIssueDeliverable `json:"deliverables"`
}

type sharedIssueMessage struct {
	Seq       int32  `json:"seq"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

type sharedIssueRunMessages struct {
	Messages  []sharedIssueMessage `json:"messages"`
	NextAfter *int32               `json:"next_after"`
}

func (h *Handler) loadIssueShareManager(w http.ResponseWriter, r *http.Request) (db.Issue, pgtype.UUID, bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Issue{}, pgtype.UUID{}, false
	}
	member, ok := h.workspaceMember(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return db.Issue{}, pgtype.UUID{}, false
	}
	switch member.Role {
	case "owner", "admin":
		return issue, member.UserID, true
	case "member":
		if (issue.CreatorType == "member" && issue.CreatorID == member.UserID) ||
			(issue.AssigneeType.Valid && issue.AssigneeType.String == "member" && issue.AssigneeID == member.UserID) {
			return issue, member.UserID, true
		}
	}
	writeError(w, http.StatusForbidden, "insufficient permissions to manage issue sharing")
	return db.Issue{}, pgtype.UUID{}, false
}

func (h *Handler) GetIssueShare(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := h.loadIssueShareManager(w, r)
	if !ok {
		return
	}

	token, err := h.Queries.GetIssueShareToken(r.Context(), db.GetIssueShareTokenParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	switch {
	case isNotFound(err):
		writeJSON(w, http.StatusOK, issueShareResponse{})
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to load issue share")
	default:
		writeJSON(w, http.StatusOK, issueShareResponse{Token: &token})
	}
}

func (h *Handler) CreateIssueShare(w http.ResponseWriter, r *http.Request) {
	issue, userID, ok := h.loadIssueShareManager(w, r)
	if !ok {
		return
	}

	token, err := generateShareCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue share")
		return
	}
	token, err = h.Queries.CreateIssueShare(r.Context(), db.CreateIssueShareParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, ProjectID: issue.ProjectID,
		UserID: userID, Token: token,
	})
	switch {
	case isNotFound(err):
		writeError(w, http.StatusConflict, "issue sharing permissions or scope changed")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to create issue share")
	default:
		writeJSON(w, http.StatusOK, issueShareResponse{Token: &token})
	}
}

func (h *Handler) RevokeIssueShare(w http.ResponseWriter, r *http.Request) {
	issue, userID, ok := h.loadIssueShareManager(w, r)
	if !ok {
		return
	}
	token := r.Header.Get(issueShareTokenHeader)
	if !validIssueShareToken(token) {
		writeError(w, http.StatusBadRequest, "invalid issue share token")
		return
	}

	count, err := h.Queries.RevokeIssueShare(r.Context(), db.RevokeIssueShareParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, UserID: userID, Token: token,
	})
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to revoke issue share")
	case count == 0:
		writeError(w, http.StatusConflict, "issue share has changed")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) GetSharedIssue(w http.ResponseWriter, r *http.Request) {
	viewerID, token, ok := readIssueShare(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	data, err := h.Queries.GetSharedIssue(ctx, db.GetSharedIssueParams{
		ViewerID: viewerID, Token: token,
	})
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "issue share not found")
		return
	}
	var issue sharedIssue
	if err != nil || json.Unmarshal(data, &issue) != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shared issue")
		return
	}
	files, err := h.Queries.ListSharedIssueDeliverables(ctx, db.ListSharedIssueDeliverablesParams{
		ViewerID: viewerID, Token: token,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shared issue deliverables")
		return
	}
	issue.Deliverables = make([]sharedIssueDeliverable, 0, len(files))
	if h.Storage != nil {
		for _, file := range files {
			if h.ownedStorageKey(file.Url) != "" {
				issue.Deliverables = append(issue.Deliverables, sharedIssueDeliverable{
					ID: uuidToString(file.ID), Filename: file.Filename, ContentType: file.ContentType, Size: file.SizeBytes,
				})
			}
		}
	}
	avatars := make(map[string]string)
	resolveAvatar := func(value string) string {
		if resolved, ok := avatars[value]; ok {
			return resolved
		}
		resolved := h.sharedIssueAvatarURL(ctx, value)
		avatars[value] = resolved
		return resolved
	}
	for index := range issue.Comments {
		issue.Comments[index].AuthorAvatarURL = resolveAvatar(issue.Comments[index].AuthorAvatarURL)
	}
	for index := range issue.Runs {
		issue.Runs[index].AgentAvatarURL = resolveAvatar(issue.Runs[index].AgentAvatarURL)
	}
	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) sharedIssueAvatarURL(ctx context.Context, value string) string {
	if strings.HasPrefix(value, "emoji:") {
		return value
	}
	if h.Storage != nil {
		if key, ok := avatarKeyFromServedURL(value); ok {
			if h.avatarKeyIsPublishable(ctx, key) {
				return h.resolveAvatarURL(value)
			}
			return ""
		}
		if key := h.ownedStorageKey(value); key != "" {
			if h.avatarKeyIsPublishable(ctx, key) {
				return h.resolveAvatarURL(value)
			}
			return ""
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		strings.HasPrefix(path.Clean(u.Path), "/api/") || shouldProxyAttachmentURL(value) {
		return ""
	}
	return value
}

func (h *Handler) ListSharedIssueRunMessages(w http.ResponseWriter, r *http.Request) {
	viewerID, token, ok := readIssueShare(w, r)
	if !ok {
		return
	}
	runID, err := util.ParseUUID(chi.URLParam(r, "runId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "shared issue run not found")
		return
	}
	afterSeq := int32(-1)
	if after := r.URL.Query().Get("after"); after != "" {
		value, err := strconv.ParseInt(after, 10, 32)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "invalid message cursor")
			return
		}
		afterSeq = int32(value)
	}
	rows, err := h.Queries.ListSharedIssueRunMessages(r.Context(), db.ListSharedIssueRunMessagesParams{
		ViewerID: viewerID, Token: token, RunID: runID, AfterSeq: afterSeq, PageSize: sharedIssueMessagePageSize + 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shared issue run messages")
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "shared issue run not found")
		return
	}
	var nextAfter *int32
	if rows[0].Seq == -1 {
		rows = rows[:0]
	} else if len(rows) > sharedIssueMessagePageSize {
		rows = rows[:sharedIssueMessagePageSize]
		nextAfter = &rows[len(rows)-1].Seq
	}
	messages := make([]sharedIssueMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, sharedIssueMessage{
			Seq: row.Seq, Type: row.Type, Content: row.Content, CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, sharedIssueRunMessages{Messages: messages, NextAfter: nextAfter})
}

func (h *Handler) DownloadSharedIssueDeliverable(w http.ResponseWriter, r *http.Request) {
	viewerID, token, ok := readIssueShare(w, r)
	if !ok {
		return
	}
	attachmentID, err := util.ParseUUID(chi.URLParam(r, "attachmentId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "shared issue deliverable not found")
		return
	}
	files, err := h.Queries.ListSharedIssueDeliverables(r.Context(), db.ListSharedIssueDeliverablesParams{
		ViewerID: viewerID, Token: token, AttachmentID: attachmentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shared issue deliverable")
		return
	}
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "shared issue deliverable not found")
		return
	}
	if h.Storage == nil {
		writeFeatureDisabled(w, "storage_not_configured", "storage not configured")
		return
	}
	file := files[0]
	key := h.ownedStorageKey(file.Url)
	if key == "" {
		writeError(w, http.StatusNotFound, "shared issue deliverable not found")
		return
	}
	h.proxyAttachmentDownload(w, r, file, key, true)
}

func readIssueShare(w http.ResponseWriter, r *http.Request) (pgtype.UUID, string, bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	user, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, "", false
	}
	userID, ok := parseUUIDOrBadRequest(w, user, "user id")
	if !ok {
		return pgtype.UUID{}, "", false
	}
	token := r.Header.Get(issueShareTokenHeader)
	if !validIssueShareToken(token) {
		writeError(w, http.StatusNotFound, "issue share not found")
		return pgtype.UUID{}, "", false
	}
	return userID, token, true
}

func validIssueShareToken(token string) bool {
	if len(token) != 24 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}
