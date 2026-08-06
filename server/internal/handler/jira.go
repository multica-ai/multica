package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/jira"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ─────────────────────────────────────────────────────────

// JiraConnectionResponse is the JSON shape for a stored Jira connection.
// Secrets are never included; the webhook secret is returned exactly once at
// create time via JiraConnectResponse. Mirrors VCSConnectionResponse.
type JiraConnectionResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	BaseURL      string `json:"base_url"`
	AccountEmail string `json:"account_email"`
	WebhookURL   string `json:"webhook_url"`
	WebhookPath  string `json:"webhook_path"`
	// JQL filter used by the pull-based sync. Empty means the default
	// `assignee = currentUser()` is applied at sync time.
	JQL       string `json:"jql"`
	CreatedAt string `json:"created_at"`
}

// JiraConnectResponse embeds the stored connection plus the one-time
// plaintext webhook secret the user must configure on the Jira webhook
// (as the X-Multica-Webhook-Secret header or a `secret` query parameter on
// the webhook URL). Not retrievable after.
type JiraConnectResponse struct {
	JiraConnectionResponse
	WebhookSecret string `json:"webhook_secret"`
}

const jiraWebhookPathPrefix = "/api/webhooks/jira/"

// isJiraConfigured reports whether the at-rest encryption key is wired.
// Unlike VCS there is no separate deployment-level availability flag: Jira
// Cloud is reachable from any deployment, so setting MULTICA_JIRA_SECRET_KEY
// is the operator's opt-in (same model as the Lark/Slack secret keys).
func (h *Handler) isJiraConfigured() bool { return h.JiraSecretBox != nil }

func (h *Handler) jiraWebhookPath(connID string) string { return jiraWebhookPathPrefix + connID }

func (h *Handler) jiraWebhookURL(connID string) string {
	base := strings.TrimRight(h.cfg.PublicURL, "/")
	if base == "" {
		return ""
	}
	return base + h.jiraWebhookPath(connID)
}

func (h *Handler) jiraConnectionToResponse(c db.JiraConnection) JiraConnectionResponse {
	id := uuidToString(c.ID)
	return JiraConnectionResponse{
		ID:           id,
		WorkspaceID:  uuidToString(c.WorkspaceID),
		BaseURL:      c.BaseUrl,
		AccountEmail: c.AccountEmail,
		WebhookURL:   h.jiraWebhookURL(id),
		WebhookPath:  h.jiraWebhookPath(id),
		JQL:          c.Jql.String,
		CreatedAt:    timestampToString(c.CreatedAt),
	}
}

func (h *Handler) sealJiraSecret(plaintext string) (string, error) {
	sealed, err := h.JiraSecretBox.Seal([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (h *Handler) openJiraSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	plaintext, err := h.JiraSecretBox.Open(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

// ListJiraConnections (GET /workspaces/{id}/jira/connections) is
// member-visible; connect/disconnect are admin-gated by the router. No
// secrets returned.
func (h *Handler) ListJiraConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	rows, err := h.Queries.ListJiraConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	out := make([]JiraConnectionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.jiraConnectionToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": out,
		"configured":  h.isJiraConfigured(),
		"can_manage":  canManage,
	})
}

type connectJiraRequest struct {
	BaseURL      string `json:"base_url"`
	AccountEmail string `json:"account_email"`
	APIToken     string `json:"api_token"`
	// Optional JQL filter for the pull-based sync; empty falls back to
	// `assignee = currentUser()` at sync time.
	JQL string `json:"jql"`
}

// ConnectJira (POST /workspaces/{id}/jira/connections) validates the supplied
// site URL + email + API token against the live Jira site, mints a webhook
// secret, stores both secrets encrypted, and returns the connection plus the
// one-time webhook secret. Reconnecting the same site rotates it.
func (h *Handler) ConnectJira(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.isJiraConfigured() {
		writeError(w, http.StatusServiceUnavailable, "jira integration not configured (MULTICA_JIRA_SECRET_KEY unset)")
		return
	}

	var req connectJiraRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	baseURL := jira.NormalizeBaseURL(req.BaseURL)
	email := strings.TrimSpace(req.AccountEmail)
	token := strings.TrimSpace(req.APIToken)
	if baseURL == "" || email == "" || token == "" {
		writeError(w, http.StatusBadRequest, "base_url, account_email and api_token are required")
		return
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeError(w, http.StatusBadRequest, "base_url must be an absolute http(s) URL")
		return
	}

	if _, err := h.JiraClient.ValidateCredentials(r.Context(), baseURL, email, token); err != nil {
		if errors.Is(err, jira.ErrUnauthorized) {
			writeError(w, http.StatusBadRequest, "jira rejected the email/API token pair")
			return
		}
		writeError(w, http.StatusBadGateway, "could not reach the jira site")
		return
	}

	webhookSecret, err := newVCSWebhookSecret() // same 32-byte hex mint as VCS
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint webhook secret")
		return
	}
	tokenEnc, err := h.sealJiraSecret(token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}
	secretEnc, err := h.sealJiraSecret(webhookSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt webhook secret")
		return
	}

	var connectedBy pgtype.UUID
	if member, ok := middleware.MemberFromContext(r.Context()); ok {
		connectedBy = member.UserID
	}

	conn, err := h.Queries.UpsertJiraConnection(r.Context(), db.UpsertJiraConnectionParams{
		WorkspaceID:            wsUUID,
		BaseUrl:                baseURL,
		AccountEmail:           email,
		ApiTokenEncrypted:      tokenEnc,
		WebhookSecretEncrypted: secretEnc,
		ConnectedByID:          connectedBy,
		Jql:                    ptrToText(strPtrOrNil(strings.TrimSpace(req.JQL))),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save connection")
		return
	}

	resp := h.jiraConnectionToResponse(conn)
	h.publish(protocol.EventJiraConnectionCreated, workspaceID, "system", "", map[string]any{"id": resp.ID})
	writeJSON(w, http.StatusOK, JiraConnectResponse{
		JiraConnectionResponse: resp,
		WebhookSecret:          webhookSecret,
	})
}

// defaultJiraSyncJQL is applied when a connection has no stored JQL: pull
// the issues assigned to the account whose API token is stored.
const defaultJiraSyncJQL = "assignee = currentUser()"

// jiraSyncMaxIssues caps how many issues one manual sync will pull.
const jiraSyncMaxIssues = 100

// JiraSyncResponse summarizes one pull-based sync run.
type JiraSyncResponse struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	// Total is how many Jira issues the JQL search returned (created +
	// updated + skipped).
	Total int `json:"total"`
}

// SyncJiraConnection (POST /workspaces/{id}/jira/connections/{connectionId}/sync)
// pulls issues from the Jira site via JQL search and runs the same
// create-or-sync path the webhook uses for each result. This is the
// import route for users who cannot register webhooks on the Jira site
// (webhooks need Jira admin rights; the REST search only needs the stored
// API token). Admin-gated like the other Jira mutations.
func (h *Handler) SyncJiraConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	connUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	if !h.isJiraConfigured() {
		writeError(w, http.StatusServiceUnavailable, "jira integration not configured (MULTICA_JIRA_SECRET_KEY unset)")
		return
	}
	conn, err := h.Queries.GetJiraConnectionByID(r.Context(), connUUID)
	if err != nil || uuidToString(conn.WorkspaceID) != uuidToString(wsUUID) {
		writeError(w, http.StatusNotFound, "jira connection not found")
		return
	}

	token, err := h.openJiraSecret(conn.ApiTokenEncrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt api token")
		return
	}
	jql := strings.TrimSpace(conn.Jql.String)
	if jql == "" {
		jql = defaultJiraSyncJQL
	}

	issues, err := h.JiraClient.SearchIssues(r.Context(), conn.BaseUrl, conn.AccountEmail, token, jql, jiraSyncMaxIssues)
	if err != nil {
		switch {
		case errors.Is(err, jira.ErrUnauthorized):
			writeError(w, http.StatusBadRequest, "jira rejected the stored credentials; reconnect the site")
		case errors.Is(err, jira.ErrBadRequest):
			writeError(w, http.StatusBadRequest, "jira rejected the JQL query")
		default:
			writeError(w, http.StatusBadGateway, "could not reach the jira site")
		}
		return
	}

	var resp JiraSyncResponse
	resp.Total = len(issues)
	for _, issue := range issues {
		ev := jira.IssueEvent{
			Kind:        jira.EventIssueUpdated,
			IssueID:     issue.ID,
			IssueKey:    issue.Key,
			Summary:     issue.Summary,
			Description: issue.Description,
		}
		switch h.syncJiraIssue(r.Context(), conn, ev) {
		case jiraSyncCreated:
			resp.Created++
		case jiraSyncUpdated:
			resp.Updated++
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetJiraConnection (GET /workspaces/{id}/jira/connections/{connectionId}).
func (h *Handler) GetJiraConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	connUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	conn, err := h.Queries.GetJiraConnectionByID(r.Context(), connUUID)
	if err != nil || uuidToString(conn.WorkspaceID) != uuidToString(wsUUID) {
		writeError(w, http.StatusNotFound, "jira connection not found")
		return
	}
	writeJSON(w, http.StatusOK, h.jiraConnectionToResponse(conn))
}

// DeleteJiraConnection (DELETE /workspaces/{id}/jira/connections/{connectionId}).
// The query sweeps the connection's jira_issue_link rows in the same atomic
// statement (no DB cascades per migration rules); mirrored Multica issues
// are kept — disconnecting stops the sync, it does not delete work.
func (h *Handler) DeleteJiraConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteJiraConnection(r.Context(), db.DeleteJiraConnectionParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove connection")
		return
	}
	h.publish(protocol.EventJiraConnectionDeleted, workspaceID, "system", "", map[string]any{
		"id": chi.URLParam(r, "connectionId"),
	})
	w.WriteHeader(http.StatusNoContent)
}
