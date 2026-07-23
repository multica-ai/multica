package handler

// CEREBRO-PATCH(ios-share-inbox): FIR-3545 token-bound iOS Share Sheet intake.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	iosShareTokenPrefix = "sit_"
	maxIOSShareBody     = 12 << 20
	maxIOSShareText     = 100_000
)

type createIOSShareInboxRequest struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

type iosShareInboxResponse struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	Name        string  `json:"name"`
	TokenPrefix string  `json:"token_prefix"`
	Token       string  `json:"token,omitempty"`
	CreatedAt   string  `json:"created_at"`
	LastUsedAt  *string `json:"last_used_at"`
}

type iosSharePayload struct {
	Title       string `json:"title"`
	Text        string `json:"text"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	FileBase64  string `json:"file_base64"`
}

func generateIOSShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return iosShareTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashIOSShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) CreateIOSShareInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	var req createIOSShareInboxRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: parseUUID(workspaceID)})
	member, memberErr := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil || memberErr != nil || !h.canAccessProject(r.Context(), member, project) {
		writeError(w, http.StatusBadRequest, "project not found in this workspace")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "iPhone Share Sheet"
	}
	token, err := generateIOSShareToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create share inbox")
		return
	}
	var id, createdAt string
	err = h.DB.QueryRow(r.Context(), `
		INSERT INTO cerebro_ios_share_inbox (workspace_id, project_id, owner_user_id, name, token_hash, token_prefix)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, created_at::text`, workspaceID, req.ProjectID, userID, name, hashIOSShareToken(token), token[:12]).Scan(&id, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create share inbox")
		return
	}
	writeJSON(w, http.StatusCreated, iosShareInboxResponse{ID: id, ProjectID: req.ProjectID, Name: name, TokenPrefix: token[:12], Token: token, CreatedAt: createdAt})
}

func (h *Handler) ListIOSShareInboxes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT id::text, project_id::text, name, token_prefix, created_at::text, last_used_at::text
		FROM cerebro_ios_share_inbox
		WHERE workspace_id=$1 AND owner_user_id=$2 AND revoked_at IS NULL
		ORDER BY created_at DESC`, h.resolveWorkspaceID(r), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list share inboxes")
		return
	}
	defer rows.Close()
	items := make([]iosShareInboxResponse, 0)
	for rows.Next() {
		var item iosShareInboxResponse
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Name, &item.TokenPrefix, &item.CreatedAt, &item.LastUsedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list share inboxes")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) RevokeIOSShareInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	tag, err := h.DB.Exec(r.Context(), `UPDATE cerebro_ios_share_inbox SET revoked_at=now() WHERE id=$1 AND workspace_id=$2 AND owner_user_id=$3 AND revoked_at IS NULL`, id, h.resolveWorkspaceID(r), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke share inbox")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "share inbox not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HandleIOSShareInbox(w http.ResponseWriter, r *http.Request) {
	if h.WebhookIPRateLimiter != nil {
		if ip := h.clientIPForRateLimit(r); ip != "" && !h.WebhookIPRateLimiter.Allow(r.Context(), ip) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIOSShareBody)
	var payload iosSharePayload
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(payload.Text) > maxIOSShareText || len(payload.Title) > 500 || len(payload.URL) > 8_000 {
		writeError(w, http.StatusRequestEntityTooLarge, "shared content is too large")
		return
	}
	token := chi.URLParam(r, "token")
	if !strings.HasPrefix(token, iosShareTokenPrefix) || len(token) < 40 {
		writeError(w, http.StatusNotFound, "share inbox not found")
		return
	}
	var inboxID, workspaceID, projectID, ownerUserID string
	err := h.DB.QueryRow(r.Context(), `
		SELECT i.id::text, i.workspace_id::text, i.project_id::text, i.owner_user_id::text
		FROM cerebro_ios_share_inbox i
		JOIN member wm ON wm.workspace_id=i.workspace_id AND wm.user_id=i.owner_user_id
		WHERE i.token_hash=$1 AND i.revoked_at IS NULL`, hashIOSShareToken(token)).Scan(&inboxID, &workspaceID, &projectID, &ownerUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "share inbox not found")
		return
	}
	if err != nil {
		slog.Error("ios share inbox lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to process shared content")
		return
	}
	if h.WebhookRateLimiter != nil && !h.WebhookRateLimiter.Allow(r.Context(), "ios-share:"+inboxID) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	project, projectErr := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: parseUUID(projectID), WorkspaceID: parseUUID(workspaceID)})
	member, memberErr := h.getWorkspaceMember(r.Context(), ownerUserID, workspaceID)
	if projectErr != nil || memberErr != nil || !h.canAccessProject(r.Context(), member, project) {
		writeError(w, http.StatusNotFound, "share inbox not found")
		return
	}
	if payload.FileBase64 != "" {
		if _, err := decodeIOSShareImage(payload.FileBase64); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = firstShareLine(payload.Text)
	}
	if title == "" {
		title = strings.TrimSpace(payload.URL)
	}
	if title == "" && payload.FileBase64 != "" {
		title = "Shared image"
	}
	if title == "" {
		writeError(w, http.StatusBadRequest, "shared content is empty")
		return
	}
	titleRunes := []rune(title)
	if len(titleRunes) > 200 {
		title = string(titleRunes[:200])
	}
	description := strings.TrimSpace(payload.Text)
	if payload.URL != "" && !strings.Contains(description, payload.URL) {
		if description != "" {
			description += "\n\n"
		}
		description += payload.URL
	}
	res, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID: parseUUID(workspaceID), Title: title, Description: pgtype.Text{String: description, Valid: description != ""},
		Status: "todo", Priority: "none", CreatorType: "member", CreatorID: parseUUID(ownerUserID),
		ProjectID: parseUUID(projectID), AllowDuplicate: true,
	}, service.IssueCreateOpts{ActorID: ownerUserID, Platform: "ios_share_sheet"})
	if err != nil {
		slog.Error("ios share issue create failed", "error", err, "inbox_id", inboxID)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	attachmentWarning := ""
	if payload.FileBase64 != "" {
		if err := h.attachIOSShareFile(r, res.Issue, payload); err != nil {
			slog.Error("ios share attachment failed", "error", err, "issue_id", uuidToString(res.Issue.ID))
			attachmentWarning = "issue created, but the attachment could not be saved"
		}
	}
	_, _ = h.DB.Exec(r.Context(), `UPDATE cerebro_ios_share_inbox SET last_used_at=now() WHERE id=$1`, inboxID)
	writeJSON(w, http.StatusCreated, map[string]any{"issue_id": uuidToString(res.Issue.ID), "identifier": fmt.Sprintf("%s-%d", h.getIssuePrefix(r.Context(), res.Issue.WorkspaceID), res.Issue.Number), "warning": attachmentWarning})
}

func firstShareLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func (h *Handler) attachIOSShareFile(r *http.Request, issue db.Issue, payload iosSharePayload) error {
	if h.Storage == nil {
		return errors.New("storage not configured")
	}
	data, err := decodeIOSShareImage(payload.FileBase64)
	if err != nil {
		return err
	}
	contentType := http.DetectContentType(data[:min(len(data), 512)])
	filename := path.Base(strings.TrimSpace(payload.Filename))
	if filename == "." || filename == "" {
		filename = "shared-image.jpg"
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	key := "workspaces/" + uuidToString(issue.WorkspaceID) + "/" + id.String() + path.Ext(filename)
	link, err := h.Storage.Upload(r.Context(), key, data, contentType, filename)
	if err != nil {
		return err
	}
	_, err = h.Queries.CreateAttachment(r.Context(), db.CreateAttachmentParams{
		ID: pgtype.UUID{Bytes: id, Valid: true}, WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		UploaderType: "member", UploaderID: issue.CreatorID, Filename: filename, Url: link,
		ContentType: contentType, SizeBytes: int64(len(data)),
	})
	if err != nil {
		h.Storage.Delete(r.Context(), key)
	}
	return err
}

func decodeIOSShareImage(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("invalid file_base64")
	}
	if len(data) == 0 || len(data) > maxUploadSize {
		return nil, errors.New("invalid attachment size")
	}
	if contentType := http.DetectContentType(data[:min(len(data), 512)]); !strings.HasPrefix(contentType, "image/") {
		return nil, errors.New("only image attachments are accepted")
	}
	return data, nil
}
