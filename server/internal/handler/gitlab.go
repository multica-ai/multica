package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Config helpers ───────────────────────────────────────────────────────────

func gitlabBaseURL() string { return strings.TrimRight(os.Getenv("GITLAB_URL"), "/") }
func gitlabAPIURL() string  { return gitlabBaseURL() + "/api/v4" }

func isGitLabConfigured() bool {
	return os.Getenv("GITLAB_URL") != "" &&
		os.Getenv("GITLAB_APP_ID") != "" &&
		os.Getenv("GITLAB_APP_SECRET") != "" &&
		os.Getenv("GITLAB_WEBHOOK_SECRET") != ""
}

func gitlabWebhookSecret() string { return strings.TrimSpace(os.Getenv("GITLAB_WEBHOOK_SECRET")) }

// signGitLabState and verifyGitLabState mirror the GitHub state-token pattern,
// using GITLAB_WEBHOOK_SECRET as the HMAC key.
// Token format: "{payload}|{namespace}.{nonce}.{sig}"
// namespace may be empty (login flow); payload is workspaceID or "login".
func signGitLabState(payload, namespace string) (string, error) {
	secret := gitlabWebhookSecret()
	if secret == "" {
		return "", errors.New("gitlab webhook secret not configured")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	combined := payload + "|" + namespace
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(combined))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	sig := hex.EncodeToString(mac.Sum(nil))
	return combined + "." + nonce + "." + sig, nil
}

func verifyGitLabState(token string) (payload, namespace string, ok bool) {
	secret := gitlabWebhookSecret()
	if secret == "" {
		return "", "", false
	}

	// Split from the right so payload/namespace can contain '.' safely.
	lastDot := strings.LastIndex(token, ".")
	if lastDot < 0 {
		return "", "", false
	}
	beforeSig, sig := token[:lastDot], token[lastDot+1:]
	secondDot := strings.LastIndex(beforeSig, ".")
	if secondDot < 0 {
		return "", "", false
	}
	combined, nonce := beforeSig[:secondDot], beforeSig[secondDot+1:]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(combined))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", "", false
	}
	p, ns, _ := strings.Cut(combined, "|")
	return p, ns, true
}

// ── Response types ───────────────────────────────────────────────────────────

type GitLabConnectionResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Namespace     string  `json:"namespace"`
	NamespaceType string  `json:"namespace_type"`
	AvatarURL     *string `json:"avatar_url"`
	CreatedAt     string  `json:"created_at"`
}

type GitLabMergeRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	ProjectPath     string  `json:"project_path"`
	MRIID           int32   `json:"mr_iid"`
	Title           string  `json:"title"`
	State           string  `json:"state"`
	HtmlURL         string  `json:"html_url"`
	SourceBranch    *string `json:"source_branch"`
	AuthorUsername  *string `json:"author_username"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	MergedAt        *string `json:"merged_at"`
	ClosedAt        *string `json:"closed_at"`
	MRCreatedAt     string  `json:"mr_created_at"`
	MRUpdatedAt     string  `json:"mr_updated_at"`
}

type ListGitLabConnectionsResponse struct {
	Connections []GitLabConnectionResponse `json:"connections"`
	Configured  bool                       `json:"configured"`
	CanManage   bool                       `json:"can_manage"`
}

type GitLabIssueResponse struct {
	GlIssueIID         int32   `json:"gl_issue_iid"`
	ProjectPath        string  `json:"project_path"`
	URL                string  `json:"url"`
	GlAssigneeUsername *string `json:"gl_assignee_username"`
}

func gitlabConnectionToResponse(c db.GitlabConnection) GitLabConnectionResponse {
	return GitLabConnectionResponse{
		ID:            uuidToString(c.ID),
		WorkspaceID:   uuidToString(c.WorkspaceID),
		Namespace:     c.Namespace,
		NamespaceType: c.NamespaceType,
		AvatarURL:     textToPtr(c.AvatarUrl),
		CreatedAt:     timestampToString(c.CreatedAt),
	}
}

func gitlabMRToResponse(mr db.GitlabMergeRequest) GitLabMergeRequestResponse {
	return GitLabMergeRequestResponse{
		ID:              uuidToString(mr.ID),
		WorkspaceID:     uuidToString(mr.WorkspaceID),
		ProjectPath:     mr.ProjectPath,
		MRIID:           mr.MrIid,
		Title:           mr.Title,
		State:           mr.State,
		HtmlURL:         mr.HtmlUrl,
		SourceBranch:    textToPtr(mr.SourceBranch),
		AuthorUsername:  textToPtr(mr.AuthorUsername),
		AuthorAvatarURL: textToPtr(mr.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(mr.MergedAt),
		ClosedAt:        timestampToPtr(mr.ClosedAt),
		MRCreatedAt:     timestampToString(mr.MrCreatedAt),
		MRUpdatedAt:     timestampToString(mr.MrUpdatedAt),
	}
}

// ── Webhook ──────────────────────────────────────────────────────────────────

// HandleGitLabWebhook (POST /api/webhooks/gitlab) verifies X-Gitlab-Token and
// routes Merge Request / Issue / Note Hook events to the corresponding handlers.
func (h *Handler) HandleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	secret := gitlabWebhookSecret()
	if secret == "" {
		writeError(w, http.StatusServiceUnavailable, "gitlab integration not configured")
		return
	}
	if r.Header.Get("X-Gitlab-Token") != secret {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
		return
	}
	switch r.Header.Get("X-Gitlab-Event") {
	case "Merge Request Hook":
		h.handleGitLabMergeRequestEvent(r.Context(), body)
	case "Issue Hook":
		h.handleGitLabIssueEvent(r.Context(), body)
	case "Note Hook":
		h.handleGitLabNoteEvent(r.Context(), body)
	}
	w.WriteHeader(http.StatusNoContent)
}

// gitlabIssuePayload is the subset of GitLab's Issue Hook webhook we consume.
type gitlabIssuePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID         int32  `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Action      string `json:"action"`
	} `json:"object_attributes"`
	Project struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	Labels []struct {
		Title string `json:"title"`
	} `json:"labels"`
	Assignees []struct {
		Username string `json:"username"`
	} `json:"assignees"`
}

// gitlabNotePayload is the subset of GitLab's Note Hook webhook we consume.
type gitlabNotePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		NoteableType string `json:"noteable_type"`
		System      bool   `json:"system"`
		ID          int64  `json:"id"`
		Note        string `json:"note"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	Issue struct {
		IID int32 `json:"iid"`
	} `json:"issue"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
}

func (h *Handler) handleGitLabNoteEvent(ctx context.Context, body []byte) {
	var p gitlabNotePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse note payload", "err", err)
		return
	}

	// Only handle issue comments; skip system notes.
	if p.ObjectAttributes.NoteableType != "Issue" || p.ObjectAttributes.System {
		return
	}
	// Echo-loop prevention for notes created by Multica itself (see postCommentToGitLab).
	if strings.Contains(p.ObjectAttributes.Note, "<!-- multica:gitlab-relay -->") {
		return
	}

	namespace := p.Project.Namespace
	projectPath := p.Project.PathWithNamespace

	conn, err := h.resolveGitLabConnectionByNamespace(ctx, namespace)
	if err != nil {
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		return
	}

	if ws, err := h.Queries.GetWorkspace(ctx, conn.WorkspaceID); err == nil && !workspaceGitLabCommentSyncEnabled(ws.Settings) {
		return
	}

	// Find the linked gitlab_issue row.
	row, err := h.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: conn.WorkspaceID,
		ProjectPath: projectPath,
		GlIssueIid:  p.Issue.IID,
	})
	if err != nil {
		// Issue not synced — skip.
		return
	}

	noteID := pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true}

	// Echo prevention: skip if this note_id already exists.
	if _, err := h.Queries.GetCommentByGitLabNoteID(ctx, noteID); err == nil {
		return
	}

	// Build attributed content.
	content := "**" + p.User.Username + "** (GitLab):\n" + p.ObjectAttributes.Note

	// Resolve creator for author fields.
	authorID, ok := h.gitlabCreatorID(ctx, conn)
	if !ok {
		slog.Error("gitlab: no author available for note comment", "workspace", uuidToString(conn.WorkspaceID))
		return
	}

	issue, err := h.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		slog.Warn("gitlab: issue not found for note", "issue_id", uuidToString(row.IssueID))
		return
	}

	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    authorID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		slog.Error("gitlab: failed to create comment from note", "err", err)
		return
	}

	// Store the note_id for echo loop prevention.
	if err := h.Queries.SetCommentGitLabNoteID(ctx, db.SetCommentGitLabNoteIDParams{
		ID:           comment.ID,
		GitlabNoteID: noteID,
	}); err != nil {
		slog.Warn("gitlab: failed to set gitlab_note_id on comment", "err", err)
	}
}

// refreshGitLabToken exchanges the stored refresh token for a new access token,
// encrypts and persists both, and returns the plain new access token.
func (h *Handler) refreshGitLabToken(ctx context.Context, conn db.GitlabConnection) (string, error) {
	if !conn.RefreshToken.Valid || conn.RefreshToken.String == "" {
		return "", errors.New("no refresh token stored")
	}
	resp, err := http.PostForm(gitlabBaseURL()+"/oauth/token", url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"client_secret": {os.Getenv("GITLAB_APP_SECRET")},
		"refresh_token": {conn.RefreshToken.String},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()
	tok, expiresAt, err := parseGitLabTokenResponse(resp.Body)
	if err != nil {
		return "", fmt.Errorf("refresh response parse: %w", err)
	}
	sealed, err := h.GitLabBox.Seal([]byte(tok.AccessToken))
	if err != nil {
		return "", fmt.Errorf("encrypt refreshed token: %w", err)
	}
	if dbErr := h.Queries.UpdateGitLabConnectionTokens(ctx, db.UpdateGitLabConnectionTokensParams{
		ID:             conn.ID,
		AccessToken:    base64.StdEncoding.EncodeToString(sealed),
		RefreshToken:   pgtype.Text{String: tok.RefreshToken, Valid: tok.RefreshToken != ""},
		TokenExpiresAt: expiresAt,
	}); dbErr != nil {
		slog.Warn("gitlab: failed to persist refreshed token", "err", dbErr)
	}
	return tok.AccessToken, nil
}

// postCommentToGitLab posts a newly-created Multica comment to the linked
// GitLab issue via the API, then stores the returned note ID on the comment
// row for echo loop prevention. It is called as a goroutine from CreateComment
// and from TaskService.createAgentComment.
func (h *Handler) postCommentToGitLab(ctx context.Context, comment db.Comment, issue db.Issue) {
	// Look up the gitlab_issue link.
	glIssue, err := h.Queries.GetGitLabIssueByIssueID(ctx, issue.ID)
	if err != nil {
		slog.Debug("gitlab: comment relay skipped — issue not linked to GitLab", "issue_id", uuidToString(issue.ID))
		return
	}

	// Load the connection for the access token.
	conn, err := h.Queries.GetGitLabConnectionByID(ctx, glIssue.ConnectionID)
	if err != nil {
		slog.Warn("gitlab: connection not found for comment post", "connection_id", uuidToString(glIssue.ConnectionID))
		return
	}

	if ws, err := h.Queries.GetWorkspace(ctx, conn.WorkspaceID); err == nil && !workspaceGitLabCommentSyncEnabled(ws.Settings) {
		return
	}

	if h.GitLabBox == nil {
		slog.Warn("gitlab: comment post skipped — GITLAB_SECRET_KEY not configured")
		return
	}

	// Refresh the access token if it has expired.
	var plainToken string
	if conn.TokenExpiresAt.Valid && conn.TokenExpiresAt.Time.Before(time.Now()) {
		refreshed, err := h.refreshGitLabToken(ctx, conn)
		if err != nil {
			slog.Warn("gitlab: token refresh failed, skipping comment post",
				"connection_id", uuidToString(conn.ID), "err", err)
			return
		}
		plainToken = refreshed
	} else {
		tokenBytes, err := base64.StdEncoding.DecodeString(conn.AccessToken)
		if err != nil {
			slog.Error("gitlab: failed to base64-decode token", "err", err)
			return
		}
		plain, err := h.GitLabBox.Open(tokenBytes)
		if err != nil {
			slog.Error("gitlab: failed to decrypt token", "err", err)
			return
		}
		plainToken = string(plain)
	}

	// POST to GitLab notes API.
	apiURL := gitlabAPIURL() + fmt.Sprintf("/projects/%d/issues/%d/notes",
		glIssue.GlProjectID, glIssue.GlIssueIid)
	const sentinel = "<!-- multica:gitlab-relay -->"
	body, err := json.Marshal(map[string]string{"body": comment.Content + "\n\n" + sentinel})
	if err != nil {
		slog.Error("gitlab: failed to marshal note body", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("gitlab: failed to build note request", "err", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+plainToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("gitlab: failed to post note", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("gitlab: note post returned error status", "status", resp.StatusCode)
		return
	}

	var noteResp struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&noteResp); err != nil || noteResp.ID == 0 {
		slog.Error("gitlab: failed to decode note response", "err", err)
		return
	}

	// Store note_id on comment for echo loop prevention.
	if err := h.Queries.SetCommentGitLabNoteID(ctx, db.SetCommentGitLabNoteIDParams{
		ID:           comment.ID,
		GitlabNoteID: pgtype.Int8{Int64: noteResp.ID, Valid: true},
	}); err != nil {
		slog.Warn("gitlab: failed to store gitlab_note_id", "err", err)
	}
}

// containsLabel reports whether the labels slice contains a label with the given title.
func containsLabel(labels []struct{ Title string `json:"title"` }, title string) bool {
	for _, l := range labels {
		if l.Title == title {
			return true
		}
	}
	return false
}

func (h *Handler) handleGitLabIssueEvent(ctx context.Context, body []byte) {
	var p gitlabIssuePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse issue payload", "err", err)
		return
	}

	namespace := p.Project.Namespace
	projectPath := p.Project.PathWithNamespace
	action := p.ObjectAttributes.Action

	conn, err := h.resolveGitLabConnectionByNamespace(ctx, namespace)
	if err != nil {
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		return
	}
	workspaceID := uuidToString(conn.WorkspaceID)

	hasAgent := containsLabel(p.Labels, "agent")

	assigneeUsername := ""
	if len(p.Assignees) > 0 {
		assigneeUsername = p.Assignees[0].Username
	}

	// Look up existing gitlab_issue row.
	row, rowErr := h.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: conn.WorkspaceID,
		ProjectPath: projectPath,
		GlIssueIid:  p.ObjectAttributes.IID,
	})
	rowExists := rowErr == nil

	// Agent label removed while row exists → delete Multica issue (cascade deletes row).
	if !hasAgent && rowExists {
		if err := h.Queries.DeleteIssue(ctx, db.DeleteIssueParams{
			ID:          row.IssueID,
			WorkspaceID: conn.WorkspaceID,
		}); err != nil {
			slog.Error("gitlab: failed to delete issue on label removal", "err", err)
			return
		}
		h.publish(protocol.EventIssueDeleted, workspaceID, "system", "", map[string]any{
			"issue_id": uuidToString(row.IssueID),
		})
		return
	}

	if hasAgent {
		if !rowExists && (action == "open" || action == "update") {
			// Create Multica issue.
			creatorID, ok := h.gitlabCreatorID(ctx, conn)
			if !ok {
				slog.Error("gitlab: no creator available, skipping issue creation", "workspace", workspaceID)
				return
			}

			res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
				WorkspaceID:    conn.WorkspaceID,
				Title:          p.ObjectAttributes.Title,
				Description:    pgtype.Text{String: p.ObjectAttributes.Description, Valid: p.ObjectAttributes.Description != ""},
				Status:         "todo",
				Priority:       "none",
				CreatorType:    "member",
				CreatorID:      creatorID,
				AllowDuplicate: true,
			}, service.IssueCreateOpts{})
			if err != nil {
				slog.Error("gitlab: failed to create issue", "err", err)
				return
			}

			glRow, err := h.Queries.InsertGitLabIssue(ctx, db.InsertGitLabIssueParams{
				WorkspaceID:        conn.WorkspaceID,
				ConnectionID:       conn.ID,
				ProjectPath:        projectPath,
				GlIssueIid:         p.ObjectAttributes.IID,
				GlProjectID:        p.Project.ID,
				IssueID:            res.Issue.ID,
				GlAssigneeUsername: pgtype.Text{String: assigneeUsername, Valid: assigneeUsername != ""},
			})
			if err != nil {
				slog.Error("gitlab: failed to insert gitlab_issue row", "err", err)
				return
			}
			row = glRow
			rowExists = true

		} else if rowExists {
			// Sync description.
			if err := h.Queries.UpdateIssueDescription(ctx, db.UpdateIssueDescriptionParams{
				ID:          row.IssueID,
				Description: pgtype.Text{String: p.ObjectAttributes.Description, Valid: p.ObjectAttributes.Description != ""},
			}); err != nil {
				slog.Warn("gitlab: failed to sync description", "err", err)
			}
			// Sync assignee.
			if err := h.Queries.UpdateGitLabIssueAssignee(ctx, db.UpdateGitLabIssueAssigneeParams{
				ID:                 row.ID,
				GlAssigneeUsername: pgtype.Text{String: assigneeUsername, Valid: assigneeUsername != ""},
			}); err != nil {
				slog.Warn("gitlab: failed to sync assignee", "err", err)
			}
		}
	}

	// Status transitions — applied after the create/sync block.
	if rowExists {
		issue, err := h.Queries.GetIssue(ctx, row.IssueID)
		if err != nil {
			slog.Warn("gitlab: issue not found for status transition", "issue_id", uuidToString(row.IssueID))
			return
		}
		switch action {
		case "close":
			h.advanceIssueToDone(ctx, issue, workspaceID, "gitlab_issue_closed")
		case "reopen":
			updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:          issue.ID,
				Status:      "in_progress",
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				slog.Warn("gitlab: failed to reopen issue", "err", err)
				return
			}
			prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
			h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", map[string]any{
				"issue":          issueToResponse(updated, prefix),
				"status_changed": true,
				"prev_status":    issue.Status,
				"source":         "gitlab_issue_reopened",
			})
		}
	}
}

// gitlabCreatorID returns the user UUID to use as creator for webhook-triggered
// issue creation. Prefers the connection's connected_by_id; falls back to the
// first workspace member.
func (h *Handler) gitlabCreatorID(ctx context.Context, conn db.GitlabConnection) (pgtype.UUID, bool) {
	if conn.ConnectedByID.Valid {
		return conn.ConnectedByID, true
	}
	members, err := h.Queries.ListMembers(ctx, conn.WorkspaceID)
	if err != nil || len(members) == 0 {
		return pgtype.UUID{}, false
	}
	return members[0].UserID, true
}

// gitlabMRPayload is the subset of GitLab's merge_request webhook we consume.
type gitlabMRPayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID          int32   `json:"iid"`
		Title        string  `json:"title"`
		Description  string  `json:"description"`
		State        string  `json:"state"`
		Action       string  `json:"action"`
		URL          string  `json:"url"`
		SourceBranch string  `json:"source_branch"`
		MergedAt     *string `json:"merged_at"`
		UpdatedAt    string  `json:"updated_at"`
		CreatedAt    string  `json:"created_at"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	User struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

func (h *Handler) handleGitLabMergeRequestEvent(ctx context.Context, body []byte) {
	var p gitlabMRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse MR payload", "err", err)
		return
	}

	namespace := p.Project.Namespace
	projectPath := p.Project.PathWithNamespace

	// Resolve workspace via connection namespace.
	conn, err := h.resolveGitLabConnectionByNamespace(ctx, namespace)
	if err != nil {
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		return
	}
	workspaceID := uuidToString(conn.WorkspaceID)

	// Parse timestamps.
	mrCreatedAt, err := time.Parse(time.RFC3339, p.ObjectAttributes.CreatedAt)
	if err != nil {
		mrCreatedAt = time.Now()
	}
	mrUpdatedAt, err := time.Parse(time.RFC3339, p.ObjectAttributes.UpdatedAt)
	if err != nil {
		mrUpdatedAt = time.Now()
	}

	// Normalize state: GitLab sends "opened" for open MRs.
	state := p.ObjectAttributes.State
	if state == "opened" {
		state = "open"
	}

	var mergedAt pgtype.Timestamptz
	if p.ObjectAttributes.MergedAt != nil && *p.ObjectAttributes.MergedAt != "" {
		if t, err := time.Parse(time.RFC3339, *p.ObjectAttributes.MergedAt); err == nil {
			mergedAt = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	avatarURL := p.User.AvatarURL

	mr, err := h.Queries.UpsertGitLabMergeRequest(ctx, db.UpsertGitLabMergeRequestParams{
		WorkspaceID:     conn.WorkspaceID,
		ConnectionID:    conn.ID,
		ProjectPath:     projectPath,
		MrIid:           p.ObjectAttributes.IID,
		Title:           p.ObjectAttributes.Title,
		State:           state,
		HtmlUrl:         p.ObjectAttributes.URL,
		SourceBranch:    pgtype.Text{String: p.ObjectAttributes.SourceBranch, Valid: p.ObjectAttributes.SourceBranch != ""},
		AuthorUsername:  pgtype.Text{String: p.User.Username, Valid: p.User.Username != ""},
		AuthorAvatarUrl: pgtype.Text{String: avatarURL, Valid: avatarURL != ""},
		MergedAt:        mergedAt,
		ClosedAt:        pgtype.Timestamptz{},
		MrCreatedAt:     pgtype.Timestamptz{Time: mrCreatedAt, Valid: true},
		MrUpdatedAt:     pgtype.Timestamptz{Time: mrUpdatedAt, Valid: true},
	})
	if err != nil {
		slog.Error("gitlab: failed to upsert MR", "err", err, "project", projectPath, "iid", p.ObjectAttributes.IID)
		return
	}

	// Extract and link closing identifiers.
	closingIdents := map[string]struct{}{}
	for _, c := range extractClosingIdentifiers(p.ObjectAttributes.Title, p.ObjectAttributes.Description) {
		closingIdents[c] = struct{}{}
	}

	// Get workspace issue prefix for identifier lookup.
	prefix := h.getIssuePrefix(ctx, conn.WorkspaceID)

	for _, ident := range extractIdentifiers(p.ObjectAttributes.Title, p.ObjectAttributes.Description) {
		issue, found := h.lookupIssueByIdentifier(ctx, conn.WorkspaceID, prefix, ident)
		if !found {
			continue
		}
		_, hasCloseIntent := closingIdents[ident]
		if err := h.Queries.LinkIssueToMergeRequest(ctx, db.LinkIssueToMergeRequestParams{
			IssueID:        issue.ID,
			MergeRequestID: mr.ID,
			CloseIntent:    hasCloseIntent,
		}); err != nil {
			slog.Warn("gitlab: failed to link issue to MR", "issue", issue.ID, "mr", mr.ID, "err", err)
		}
	}

	// Auto-advance issues when MR merges with close intent and no open MRs remain.
	// Gate on action=="merge" to avoid re-running on subsequent update webhooks
	// for an already-merged MR.
	if state == "merged" && p.ObjectAttributes.Action == "merge" {
		h.maybeAdvanceIssuesOnGitLabMerge(ctx, mr, workspaceID)
	}

	// Publish realtime event.
	h.publish(protocol.EventGitLabMergeRequestUpdated, workspaceID, "system", "", map[string]any{
		"merge_request": gitlabMRToResponse(mr),
	})
}

func (h *Handler) maybeAdvanceIssuesOnGitLabMerge(ctx context.Context, mr db.GitlabMergeRequest, workspaceID string) {
	issueIDs, err := h.Queries.ListIssueIDsForMergeRequest(ctx, mr.ID)
	if err != nil {
		return
	}
	for _, issueID := range issueIDs {
		agg, err := h.Queries.GetIssueMergeRequestCloseAggregate(ctx, issueID)
		if err != nil {
			continue
		}
		if agg.OpenCount == 0 && agg.MergedWithCloseIntentCount > 0 {
			issue, err := h.Queries.GetIssue(ctx, issueID)
			if err != nil {
				continue
			}
			h.advanceIssueToDone(ctx, issue, workspaceID, "gitlab_mr_merged")
		}
	}
}

// resolveGitLabConnectionByNamespace finds the first workspace connection whose
// namespace matches the project's top-level group/user.
func (h *Handler) resolveGitLabConnectionByNamespace(ctx context.Context, namespace string) (db.GitlabConnection, error) {
	return h.Queries.GetGitLabConnectionByNamespaceGlobal(ctx, namespace)
}

// workspaceGitLabCommentSyncEnabled returns true (sync on) unless the workspace
// has explicitly set gitlab_comment_sync_enabled=false in its settings JSON.
func workspaceGitLabCommentSyncEnabled(settings []byte) bool {
	if len(settings) == 0 {
		return true
	}
	var s struct {
		CommentSync *bool `json:"gitlab_comment_sync_enabled"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.CommentSync == nil {
		return true
	}
	return *s.CommentSync
}

// ── Workspace OAuth ──────────────────────────────────────────────────────────

// GitLabConnect (GET /api/workspaces/{id}/gitlab/connect) begins the OAuth flow.
func (h *Handler) GitLabConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if !isGitLabConfigured() {
		writeJSON(w, http.StatusOK, map[string]bool{"configured": false})
		return
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("ns"))
	state, err := signGitLabState(workspaceID, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	}
	if serverURL == "" {
		serverURL = "http://localhost:3000"
	}
	callbackURL := serverURL + "/api/gitlab/setup"
	params := url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"redirect_uri":  {callbackURL},
		"response_type": {"code"},
		"scope":         {"api"},
		"state":         {state},
	}
	oauthURL := gitlabBaseURL() + "/oauth/authorize?" + params.Encode()
	http.Redirect(w, r, oauthURL, http.StatusFound)
}

// GitLabSetupCallback (GET /api/gitlab/setup) handles the OAuth redirect.
func (h *Handler) GitLabSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	frontend := strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := frontend + "/settings?tab=gitlab"

	if code == "" || state == "" {
		http.Redirect(w, r, settingsURL+"&gitlab_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, nsFromState, ok := verifyGitLabState(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"&gitlab_error=invalid_state", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=bad_workspace", http.StatusFound)
		return
	}

	tokenResp, expiresAt, err := gitlabExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("gitlab: token exchange failed", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=token_exchange_failed", http.StatusFound)
		return
	}

	userInfo, err := gitlabFetchUser(r.Context(), tokenResp.AccessToken)
	if err != nil {
		slog.Error("gitlab: fetch user failed", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=user_fetch_failed", http.StatusFound)
		return
	}

	// Encrypt token before storing.
	if h.GitLabBox == nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=not_configured", http.StatusFound)
		return
	}
	sealed, err := h.GitLabBox.Seal([]byte(tokenResp.AccessToken))
	if err != nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=encrypt_failed", http.StatusFound)
		return
	}

	// Best-effort capture of the connecting user (may be nil if the public
	// callback was hit without a session — X-User-ID is set by auth middleware
	// which is not applied to this public route). Either way we save the row
	// so the workspace owner sees the connection on next reload.
	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}

	resolvedNamespace := nsFromState
	resolvedType := "group"
	if resolvedNamespace == "" {
		resolvedNamespace = userInfo.Namespace
		resolvedType = userInfo.NamespaceType
	}
	conn, err := h.Queries.CreateGitLabConnection(r.Context(), db.CreateGitLabConnectionParams{
		WorkspaceID:    wsUUID,
		Namespace:      resolvedNamespace,
		NamespaceType:  resolvedType,
		AvatarUrl:      pgtype.Text{String: userInfo.AvatarURL, Valid: resolvedType == "user" && userInfo.AvatarURL != ""},
		AccessToken:    base64.StdEncoding.EncodeToString(sealed),
		RefreshToken:   pgtype.Text{String: tokenResp.RefreshToken, Valid: tokenResp.RefreshToken != ""},
		TokenExpiresAt: expiresAt,
		ConnectedByID:  connectedBy,
	})
	if err != nil {
		slog.Error("gitlab: failed to persist connection", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=persist_failed", http.StatusFound)
		return
	}

	h.publish(protocol.EventGitLabConnectionCreated, workspaceID, "system", "", map[string]any{
		"connection": gitlabConnectionToResponse(conn),
	})
	http.Redirect(w, r, settingsURL+"&gitlab_connected=1", http.StatusFound)
}

// ListGitLabConnections (GET /api/workspaces/{id}/gitlab/connections)
func (h *Handler) ListGitLabConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	conns, err := h.Queries.ListGitLabConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	resp := make([]GitLabConnectionResponse, len(conns))
	for i, c := range conns {
		resp[i] = gitlabConnectionToResponse(c)
	}
	writeJSON(w, http.StatusOK, ListGitLabConnectionsResponse{
		Connections: resp,
		Configured:  isGitLabConfigured(),
		CanManage:   canManage,
	})
}

// DeleteGitLabConnection (DELETE /api/workspaces/{id}/gitlab/connections/{connectionId})
func (h *Handler) DeleteGitLabConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	connectionID := chi.URLParam(r, "connectionId")
	connUUID, ok := parseUUIDOrBadRequest(w, connectionID, "connection id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteGitLabConnection(r.Context(), db.DeleteGitLabConnectionParams{
		ID:          connUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete connection")
		return
	}
	h.publish(protocol.EventGitLabConnectionDeleted, workspaceID, "system", "", map[string]any{
		"connection_id": connectionID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── GitLab API helpers ───────────────────────────────────────────────────────

type gitlabUserInfo struct {
	Namespace     string
	NamespaceType string
	AvatarURL     string
}

type gitlabTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func parseGitLabTokenResponse(r io.Reader) (gitlabTokenResponse, pgtype.Timestamptz, error) {
	var body gitlabTokenResponse
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return body, pgtype.Timestamptz{}, err
	}
	if body.AccessToken == "" {
		return body, pgtype.Timestamptz{}, errors.New("empty access_token in response")
	}
	exp := pgtype.Timestamptz{}
	if body.ExpiresIn > 0 {
		exp = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second), Valid: true}
	}
	return body, exp, nil
}

func gitlabExchangeCode(ctx context.Context, code string) (tok gitlabTokenResponse, expiresAt pgtype.Timestamptz, err error) {
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	}
	callbackURL := serverURL + "/api/gitlab/setup"

	resp, err := http.PostForm(gitlabBaseURL()+"/oauth/token", url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"client_secret": {os.Getenv("GITLAB_APP_SECRET")},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {callbackURL},
	})
	if err != nil {
		return tok, pgtype.Timestamptz{}, err
	}
	defer resp.Body.Close()
	return parseGitLabTokenResponse(resp.Body)
}

func gitlabFetchUser(ctx context.Context, token string) (gitlabUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gitlabAPIURL()+"/user", nil)
	if err != nil {
		return gitlabUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return gitlabUserInfo{}, err
	}
	defer resp.Body.Close()

	var body struct {
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return gitlabUserInfo{}, err
	}
	return gitlabUserInfo{
		Namespace:     body.Username,
		NamespaceType: "user",
		AvatarURL:     body.AvatarURL,
	}, nil
}

// ListMergeRequestsForIssue (GET /api/issues/{id}/merge-requests)
func (h *Handler) ListMergeRequestsForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue id")
	if !ok {
		return
	}
	mrs, err := h.Queries.ListMergeRequestsByIssue(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list merge requests")
		return
	}
	resp := make([]GitLabMergeRequestResponse, len(mrs))
	for i, mr := range mrs {
		resp[i] = gitlabMRToResponse(mr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"merge_requests": resp})
}

// GetGitLabIssueForIssue (GET /api/issues/{id}/gitlab-issue) returns the linked
// GitLab issue info for display in the sidebar, or 404 if none.
func (h *Handler) GetGitLabIssueForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue id")
	if !ok {
		return
	}

	glIssue, err := h.Queries.GetGitLabIssueByIssueID(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no gitlab issue linked")
		return
	}

	issueURL := gitlabBaseURL() + "/" + glIssue.ProjectPath + "/-/issues/" + strconv.Itoa(int(glIssue.GlIssueIid))
	writeJSON(w, http.StatusOK, GitLabIssueResponse{
		GlIssueIID:         glIssue.GlIssueIid,
		ProjectPath:        glIssue.ProjectPath,
		URL:                issueURL,
		GlAssigneeUsername: textToPtr(glIssue.GlAssigneeUsername),
	})
}
