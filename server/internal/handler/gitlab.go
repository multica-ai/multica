package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ─────────────────────────────────────────────────────────

// GitlabMergeRequestResponse is the JSON shape returned by the merge request
// list endpoint and broadcast on MR-related WS events.
type GitlabMergeRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	RepoOwner       string  `json:"repo_owner"`
	RepoName        string  `json:"repo_name"`
	MrNumber        int32   `json:"mr_number"`
	MrID            int64   `json:"mr_id"`
	ProjectID       int64   `json:"project_id"`
	Title           string  `json:"title"`
	State           string  `json:"state"`
	HtmlURL         string  `json:"html_url"`
	SourceBranch    *string `json:"source_branch"`
	TargetBranch    *string `json:"target_branch"`
	AuthorLogin     *string `json:"author_login"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	MergedAt        *string `json:"merged_at"`
	ClosedAt        *string `json:"closed_at"`
	MrCreatedAt     string  `json:"mr_created_at"`
	MrUpdatedAt     string  `json:"mr_updated_at"`
}

type GitlabSettingsResponse struct {
	GitlabEnabled          bool `json:"gitlab_enabled"`
	Configured             bool `json:"configured"`
	MrSidebarEnabled       bool `json:"gitlab_mr_sidebar_enabled"`
	AutoLinkEnabled        bool `json:"gitlab_auto_link_enabled"`
}

func gitlabMRToResponse(mr db.MulticaGitlabMergeRequest) GitlabMergeRequestResponse {
	return GitlabMergeRequestResponse{
		ID:              uuidToString(mr.ID),
		WorkspaceID:     uuidToString(mr.WorkspaceID),
		RepoOwner:       mr.RepoOwner,
		RepoName:        mr.RepoName,
		MrNumber:        mr.MrNumber,
		MrID:            mr.MrID,
		ProjectID:       mr.ProjectID,
		Title:           mr.Title,
		State:           mr.State,
		HtmlURL:         mr.HtmlUrl,
		SourceBranch:    textToPtr(mr.SourceBranch),
		TargetBranch:    textToPtr(mr.TargetBranch),
		AuthorLogin:     textToPtr(mr.AuthorLogin),
		AuthorAvatarURL: textToPtr(mr.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(mr.MergedAt),
		ClosedAt:        timestampToPtr(mr.ClosedAt),
		MrCreatedAt:     timestampToString(mr.MrCreatedAt),
		MrUpdatedAt:     timestampToString(mr.MrUpdatedAt),
	}
}

// ── Webhook ─────────────────────────────────────────────────────────────────

// HandleGitlabWebhook (POST /api/webhooks/gitlab) is GitLab's destination for
// webhook events from configured GitLab projects. We match the X-Gitlab-Token
// header against per-workspace webhook tokens stored in workspace settings,
// route on object_kind, and upsert MR rows + auto-link to issues.
func (h *Handler) HandleGitlabWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	token := r.Header.Get("X-Gitlab-Token")
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing gitlab token")
		return
	}

	// Match token against workspace settings.
	workspaceID := h.matchGitlabWebhookToken(r.Context(), token)
	if workspaceID == "" {
		writeError(w, http.StatusUnauthorized, "invalid gitlab token")
		return
	}

	// Determine event type from object_kind (GitLab uses object_kind, not event_type).
	var kind struct {
		ObjectKind string `json:"object_kind"`
	}
	if err := json.Unmarshal(body, &kind); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	ctx := r.Context()
	switch kind.ObjectKind {
	case "merge_request":
		h.handleMergeRequestEvent(ctx, body, workspaceID)
	default:
		// Acknowledge every event so GitLab doesn't mark the endpoint
		// as failing, but ignore types we don't model.
	}
	w.WriteHeader(http.StatusAccepted)
}

// matchGitlabWebhookToken scans workspace settings for a matching
// gitlab_webhook_token. Returns the workspace UUID string on match,
// empty string otherwise.
func (h *Handler) matchGitlabWebhookToken(ctx context.Context, token string) string {
	rows, err := h.Queries.ListWorkspacesForGitlabWebhookLookup(ctx)
	if err != nil {
		slog.Warn("gitlab: workspace lookup failed for webhook", "err", err)
		return ""
	}
	for _, row := range rows {
		var s struct {
			GitlabWebhookToken string `json:"gitlab_webhook_token"`
		}
		if err := json.Unmarshal(row.Settings, &s); err != nil {
			continue
		}
		if s.GitlabWebhookToken != "" && s.GitlabWebhookToken == token {
			return uuidToString(row.ID)
		}
	}
	return ""
}

type glMergeRequestPayload struct {
	User struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	Project struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	ObjectAttributes struct {
		ID           int64  `json:"id"`
		IID          int32  `json:"iid"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		State        string `json:"state"`
		URL          string `json:"url"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		MergedAt     string `json:"merged_at"`
		ClosedAt     string `json:"closed_at"`
	} `json:"object_attributes"`
}

func (h *Handler) handleMergeRequestEvent(ctx context.Context, body []byte, workspaceID string) {
	var p glMergeRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("gitlab: bad merge_request payload", "err", err)
		return
	}

	wsUUID := parseUUID(workspaceID)

	// Parse repo owner and repo name from path_with_namespace (e.g. "group/repo").
	repoOwner, repoName := splitGitlabNamespace(p.Project.PathWithNamespace)

	state := deriveGitlabMRState(p.ObjectAttributes.State)

	mr, err := h.Queries.UpsertGitlabMergeRequest(ctx, db.UpsertGitlabMergeRequestParams{
		WorkspaceID:     wsUUID,
		RepoOwner:       repoOwner,
		RepoName:        repoName,
		MrNumber:        p.ObjectAttributes.IID,
		MrID:            p.ObjectAttributes.ID,
		ProjectID:       p.Project.ID,
		Title:           p.ObjectAttributes.Title,
		Description:     strToText(p.ObjectAttributes.Description),
		State:           state,
		HtmlUrl:         p.ObjectAttributes.URL,
		SourceBranch:    strToText(p.ObjectAttributes.SourceBranch),
		TargetBranch:    strToText(p.ObjectAttributes.TargetBranch),
		AuthorLogin:     strToText(p.User.Username),
		AuthorAvatarUrl: strToText(p.User.AvatarURL),
		MergedAt:        parseGitLabTime(p.ObjectAttributes.MergedAt),
		ClosedAt:        parseGitLabTime(p.ObjectAttributes.ClosedAt),
		MrCreatedAt:     parseGitLabTimeRequired(p.ObjectAttributes.CreatedAt),
		MrUpdatedAt:     parseGitLabTimeRequired(p.ObjectAttributes.UpdatedAt),
	})
	if err != nil {
		slog.Warn("gitlab: upsert mr failed", "err", err)
		return
	}

	resp := gitlabMRToResponse(mr)

	// Auto-link: scan title/description/source_branch for issue identifiers,
	// look them up in this workspace, attach the link rows.
	linkedIssueIDs := make([]string, 0)
	if h.workspaceGitlabAutoLinkEnabled(ctx, wsUUID) {
		idents := extractIdentifiers(p.ObjectAttributes.Title, p.ObjectAttributes.Description, p.ObjectAttributes.SourceBranch)
		prefix := h.getIssuePrefix(ctx, wsUUID)
		for _, id := range idents {
			issue, ok := h.lookupIssueByIdentifier(ctx, wsUUID, prefix, id)
			if !ok {
				continue
			}
			if err := h.Queries.LinkIssueToMergeRequest(ctx, db.LinkIssueToMergeRequestParams{
				IssueID:        issue.ID,
				MergeRequestID: mr.ID,
				LinkedByType:   strToText("system"),
				LinkedByID:     pgtype.UUID{},
			}); err != nil {
				slog.Warn("gitlab: link failed", "err", err)
				continue
			}
			linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))

			// Auto-close: a terminal MR event (merged/closed) may be the moment
			// the last in-flight sibling resolves. Same logic as GitHub PRs.
			if (state == "merged" || state == "closed") && issue.Status != "done" && issue.Status != "cancelled" {
				counts, err := h.Queries.GetSiblingMergeRequestStateCountsForIssue(ctx, db.GetSiblingMergeRequestStateCountsForIssueParams{
					IssueID: issue.ID,
					ID:      mr.ID,
				})
				if err != nil {
					slog.Warn("gitlab: count sibling mr states failed", "err", err, "issue_id", uuidToString(issue.ID))
					continue
				}
				anyMerged := state == "merged" || counts.MergedCount > 0
				if counts.OpenCount == 0 && anyMerged {
					h.advanceIssueToDone(ctx, issue, workspaceID)
				}
			}
		}
	}

	// Broadcast MR change to the workspace so any open issue detail page
	// re-queries its MR list.
	h.publish(protocol.EventMergeRequestUpdated, workspaceID, "system", "", map[string]any{
		"merge_request":    resp,
		"linked_issue_ids": linkedIssueIDs,
	})
}

// deriveGitlabMRState normalises GitLab MR states to our internal representation.
// GitLab states: "opened", "merged", "closed", "locked".
func deriveGitlabMRState(state string) string {
	switch state {
	case "merged":
		return "merged"
	case "closed", "locked":
		return "closed"
	case "opened":
		return "opened"
	default:
		return state
	}
}

// splitGitlabNamespace splits a GitLab path_with_namespace (e.g. "gitlab-org/gitlab")
// into owner and repo name. Single-segment paths (no slash) map to empty owner.
func splitGitlabNamespace(path string) (owner, repo string) {
	idx := strings.Index(path, "/")
	if idx < 0 {
		return "", path
	}
	return path[:idx], path[idx+1:]
}

func parseGitLabTime(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func parseGitLabTimeRequired(s string) pgtype.Timestamptz {
	t := parseGitLabTime(s)
	if !t.Valid {
		return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	return t
}

// workspaceGitlabAutoLinkEnabled reports whether the workspace allows the
// GitLab webhook to create issue ↔ MR link rows. Defaults to true so existing
// workspaces keep auto-link on, and short-circuits to false when the master
// gitlab_enabled switch is explicitly off.
func (h *Handler) workspaceGitlabAutoLinkEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil || len(ws.Settings) == 0 {
		return true
	}
	var s struct {
		GitlabEnabled          *bool `json:"gitlab_enabled"`
		GitlabAutoLinkEnabled  *bool `json:"gitlab_auto_link_enabled"`
	}
	if err := json.Unmarshal(ws.Settings, &s); err != nil {
		return true
	}
	if s.GitlabEnabled != nil && !*s.GitlabEnabled {
		return false
	}
	if s.GitlabAutoLinkEnabled == nil {
		return true
	}
	return *s.GitlabAutoLinkEnabled
}

// ── List merge requests for an issue ────────────────────────────────────────

// HandleListMergeRequests (GET /api/issues/{id}/merge-requests) returns all
// GitLab merge requests linked to an issue.
func (h *Handler) HandleListMergeRequests(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListMergeRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list merge requests")
		return
	}
	out := make([]GitlabMergeRequestResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, gitlabMRToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"merge_requests": out})
}

// ── Settings ────────────────────────────────────────────────────────────────

// gitlabSettings represents the GitLab-related keys stored in workspace.settings JSONB.
type gitlabSettings struct {
	GitlabEnabled           *bool   `json:"gitlab_enabled"`
	GitlabAccessToken       *string `json:"gitlab_access_token"`
	GitlabMrSidebarEnabled  *bool   `json:"gitlab_mr_sidebar_enabled"`
	GitlabAutoLinkEnabled   *bool   `json:"gitlab_auto_link_enabled"`
	GitlabWebhookToken      *string `json:"gitlab_webhook_token"`
}

func parseGitlabSettings(raw []byte) (gitlabSettings, error) {
	var s gitlabSettings
	if len(raw) == 0 {
		return s, nil
	}
	err := json.Unmarshal(raw, &s)
	return s, err
}

// HandleGetGitlabSettings (GET /api/workspaces/{id}/gitlab/settings) returns
// the workspace's GitLab configuration. The actual access_token value is never
// exposed — the response uses a `configured` boolean instead.
func (h *Handler) HandleGetGitlabSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	settings, err := parseGitlabSettings(ws.Settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse settings")
		return
	}

	resp := GitlabSettingsResponse{
		GitlabEnabled:    boolVal(settings.GitlabEnabled, false),
		Configured:       settings.GitlabAccessToken != nil && *settings.GitlabAccessToken != "",
		MrSidebarEnabled: boolVal(settings.GitlabMrSidebarEnabled, false),
		AutoLinkEnabled:  boolVal(settings.GitlabAutoLinkEnabled, true),
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleUpdateGitlabSettings (PUT /api/workspaces/{id}/gitlab/settings) updates
// the GitLab integration settings in workspace.settings JSONB. Admin/owner only.
// On first enable, a webhook token is generated automatically.
func (h *Handler) HandleUpdateGitlabSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req struct {
		GitlabEnabled          *bool   `json:"gitlab_enabled"`
		GitlabAccessToken      *string `json:"gitlab_access_token"`
		GitlabMrSidebarEnabled *bool   `json:"gitlab_mr_sidebar_enabled"`
		GitlabAutoLinkEnabled  *bool   `json:"gitlab_auto_link_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	// Merge incoming GitLab settings into the full workspace settings map so
	// non-GitLab keys (e.g. GitHub settings) are preserved.
	settingsMap := make(map[string]any)
	if len(ws.Settings) > 0 {
		if err := json.Unmarshal(ws.Settings, &settingsMap); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse settings")
			return
		}
	}

	wasEnabled := boolFromMap(settingsMap, "gitlab_enabled")

	if req.GitlabEnabled != nil {
		settingsMap["gitlab_enabled"] = *req.GitlabEnabled
	}
	if req.GitlabAccessToken != nil {
		settingsMap["gitlab_access_token"] = *req.GitlabAccessToken
	}
	if req.GitlabMrSidebarEnabled != nil {
		settingsMap["gitlab_mr_sidebar_enabled"] = *req.GitlabMrSidebarEnabled
	}
	if req.GitlabAutoLinkEnabled != nil {
		settingsMap["gitlab_auto_link_enabled"] = *req.GitlabAutoLinkEnabled
	}

	nowEnabled := boolFromMap(settingsMap, "gitlab_enabled")

	// Generate webhook token on first enable.
	if !wasEnabled && nowEnabled {
		if _, hasToken := settingsMap["gitlab_webhook_token"]; !hasToken || settingsMap["gitlab_webhook_token"] == "" {
			settingsMap["gitlab_webhook_token"] = generateGitlabToken()
		}
	}

	raw, err := json.Marshal(settingsMap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal settings")
		return
	}

	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{
		ID:       wsUUID,
		Settings: raw,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}

	h.publish(protocol.EventGitlabSettingsChanged, workspaceID, "system", "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "updated"})
}

// HandleRegenerateGitlabToken (POST /api/workspaces/{id}/gitlab/regenerate-token)
// generates a new webhook token for GitLab webhook authentication. Admin/owner only.
func (h *Handler) HandleRegenerateGitlabToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	settingsMap := make(map[string]any)
	if len(ws.Settings) > 0 {
		if err := json.Unmarshal(ws.Settings, &settingsMap); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse settings")
			return
		}
	}

	tok := generateGitlabToken()
	settingsMap["gitlab_webhook_token"] = tok

	raw, err := json.Marshal(settingsMap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal settings")
		return
	}

	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{
		ID:       wsUUID,
		Settings: raw,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}

	h.publish(protocol.EventGitlabSettingsChanged, workspaceID, "system", "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

// generateGitlabToken creates a UUID v4 string for use as a GitLab
// webhook token, stored in workspace settings and verified in the webhook handler.
func generateGitlabToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	// Set UUID v4 bits: version (4) and variant (RFC 4122).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// boolFromMap reads a boolean value from a settings map, defaulting to false.
func boolFromMap(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func boolVal(p *bool, defaultVal bool) bool {
	if p == nil {
		return defaultVal
	}
	return *p
}

// HandleGitlabCredential (GET /api/gitlab/credential) returns the GitLab
// personal access token for the workspace associated with the authenticated
// daemon. Used by the CLI credential helper (gitlab-credential-multica).
func (h *Handler) HandleGitlabCredential(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.DaemonWorkspaceIDFromContext(r.Context())
	if workspaceID == "" {
		workspaceID = r.Header.Get("X-Workspace-ID")
	}
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "daemon workspace context missing")
		return
	}
	wsUUID := parseUUID(workspaceID)

	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	settings, err := parseGitlabSettings(ws.Settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse settings")
		return
	}

	token := ""
	if settings.GitlabAccessToken != nil {
		token = *settings.GitlabAccessToken
	}
	if token == "" {
		writeError(w, http.StatusNotFound, "gitlab access token not configured")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"base_url": "https://gitlab.com",
		"token":    token,
	})
}
