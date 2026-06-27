package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/forgejo"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ─────────────────────────────────────────────────────────

// ForgejoConnectionResponse is the JSON shape for a stored connection. Secrets
// (access token, webhook secret) are NEVER included — the webhook secret is
// returned exactly once at create time via ForgejoConnectResponse.
type ForgejoConnectionResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	InstanceURL  string `json:"instance_url"`
	AccountLogin string `json:"account_login"`
	// WebhookURL is the absolute endpoint to register on the Forgejo repo/org
	// webhook settings. Empty when the server has no MULTICA_PUBLIC_URL, in
	// which case the UI must prefix WebhookPath with its own origin.
	WebhookURL  string `json:"webhook_url"`
	WebhookPath string `json:"webhook_path"`
	CreatedAt   string `json:"created_at"`
}

// ForgejoConnectResponse is returned by the connect endpoint. It embeds the
// stored connection and adds the one-time plaintext webhook secret the user
// must paste into Forgejo's webhook configuration. The secret is not
// retrievable afterwards (stored encrypted); reconnecting rotates it.
type ForgejoConnectResponse struct {
	ForgejoConnectionResponse
	WebhookSecret string `json:"webhook_secret"`
}

const forgejoWebhookPathPrefix = "/api/webhooks/forgejo/"

// isForgejoConfigured reports whether the at-rest secret box is wired. Without
// it the server cannot store tokens safely, so connect/webhook return 503.
func (h *Handler) isForgejoConfigured() bool { return h.ForgejoSecretBox != nil }

func (h *Handler) forgejoWebhookPath(connID string) string {
	return forgejoWebhookPathPrefix + connID
}

func (h *Handler) forgejoWebhookURL(connID string) string {
	base := strings.TrimRight(h.cfg.PublicURL, "/")
	if base == "" {
		return ""
	}
	return base + h.forgejoWebhookPath(connID)
}

func (h *Handler) forgejoConnectionToResponse(c db.ForgejoConnection) ForgejoConnectionResponse {
	id := uuidToString(c.ID)
	return ForgejoConnectionResponse{
		ID:           id,
		WorkspaceID:  uuidToString(c.WorkspaceID),
		InstanceURL:  c.InstanceUrl,
		AccountLogin: c.AccountLogin,
		WebhookURL:   h.forgejoWebhookURL(id),
		WebhookPath:  h.forgejoWebhookPath(id),
		CreatedAt:    timestampToString(c.CreatedAt),
	}
}

// forgejoPullRequestToResponse maps a stored Forgejo PR onto the shared PR
// response shape so web/desktop render Forgejo and GitHub PRs through one card
// pipeline. Forgejo has no check-suite model yet, so check fields are zero and
// ChecksConclusion is nil (the frontend hides the checks bar); mergeable_state
// is likewise unset.
func forgejoPullRequestToResponse(p db.ForgejoPullRequest) GitHubPullRequestResponse {
	return GitHubPullRequestResponse{
		ID:               uuidToString(p.ID),
		Provider:         "forgejo",
		WorkspaceID:      uuidToString(p.WorkspaceID),
		RepoOwner:        p.RepoOwner,
		RepoName:         p.RepoName,
		Number:           p.PrNumber,
		Title:            p.Title,
		State:            p.State,
		HtmlURL:          p.HtmlUrl,
		Branch:           textToPtr(p.Branch),
		AuthorLogin:      textToPtr(p.AuthorLogin),
		AuthorAvatarURL:  textToPtr(p.AuthorAvatarUrl),
		MergedAt:         timestampToPtr(p.MergedAt),
		ClosedAt:         timestampToPtr(p.ClosedAt),
		PRCreatedAt:      timestampToString(p.PrCreatedAt),
		PRUpdatedAt:      timestampToString(p.PrUpdatedAt),
		MergeableState:   nil,
		ChecksConclusion: nil,
		Additions:        p.Additions,
		Deletions:        p.Deletions,
		ChangedFiles:     p.ChangedFiles,
	}
}

// sealForgejoSecret encrypts plaintext and returns base64 ciphertext, matching
// the at-rest encoding used by the Slack/Feishu adapters.
func (h *Handler) sealForgejoSecret(plaintext string) (string, error) {
	sealed, err := h.ForgejoSecretBox.Seal([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

// ListForgejoConnections (GET /workspaces/{id}/forgejo/connections) is
// member-visible so the Integrations tab renders the workspace's Forgejo
// connection state for everyone; connect/disconnect remain admin-gated by the
// router. No secrets are returned.
func (h *Handler) ListForgejoConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	rows, err := h.Queries.ListForgejoConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	out := make([]ForgejoConnectionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.forgejoConnectionToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": out,
		"configured":  h.isForgejoConfigured(),
		"can_manage":  canManage,
	})
}

type connectForgejoRequest struct {
	InstanceURL string `json:"instance_url"`
	AccessToken string `json:"access_token"`
}

// ConnectForgejo (POST /workspaces/{id}/forgejo/connections) validates the
// supplied instance URL + access token against the live instance, mints a
// webhook secret, stores both secrets encrypted, and returns the connection
// plus the one-time webhook secret. Reconnecting the same instance rotates the
// token and secret in place.
func (h *Handler) ConnectForgejo(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.isForgejoConfigured() {
		writeError(w, http.StatusServiceUnavailable, "forgejo integration not configured (MULTICA_FORGEJO_SECRET_KEY unset)")
		return
	}

	var req connectForgejoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	instanceURL := forgejo.NormalizeInstanceURL(req.InstanceURL)
	token := strings.TrimSpace(req.AccessToken)
	if instanceURL == "" || token == "" {
		writeError(w, http.StatusBadRequest, "instance_url and access_token are required")
		return
	}
	parsed, err := url.Parse(instanceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeError(w, http.StatusBadRequest, "instance_url must be an absolute http(s) URL")
		return
	}

	// Validate the token against the live instance before persisting anything.
	user, err := forgejo.NewClient(instanceURL, token).CurrentUser(r.Context())
	if err != nil {
		if errors.Is(err, forgejo.ErrUnauthorized) {
			writeError(w, http.StatusBadRequest, "forgejo rejected the access token")
			return
		}
		writeError(w, http.StatusBadGateway, "could not reach the forgejo instance")
		return
	}

	webhookSecret, err := newForgejoWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint webhook secret")
		return
	}
	tokenEnc, err := h.sealForgejoSecret(token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}
	secretEnc, err := h.sealForgejoSecret(webhookSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt webhook secret")
		return
	}

	var connectedBy pgtype.UUID
	if member, ok := middleware.MemberFromContext(r.Context()); ok {
		connectedBy = member.UserID
	}

	conn, err := h.Queries.UpsertForgejoConnection(r.Context(), db.UpsertForgejoConnectionParams{
		WorkspaceID:            wsUUID,
		InstanceUrl:            instanceURL,
		AccountLogin:           user.Login,
		AccessTokenEncrypted:   tokenEnc,
		WebhookSecretEncrypted: secretEnc,
		ConnectedByID:          connectedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save connection")
		return
	}

	resp := h.forgejoConnectionToResponse(conn)
	h.publish(protocol.EventForgejoConnectionCreated, workspaceID, "system", "", map[string]any{
		"id": resp.ID,
	})
	writeJSON(w, http.StatusOK, ForgejoConnectResponse{
		ForgejoConnectionResponse: resp,
		WebhookSecret:             webhookSecret,
	})
}

// DeleteForgejoConnection (DELETE /workspaces/{id}/forgejo/connections/{connectionId}).
func (h *Handler) DeleteForgejoConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	id := chi.URLParam(r, "connectionId")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "connection id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteForgejoConnection(r.Context(), db.DeleteForgejoConnectionParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove connection")
		return
	}
	h.publish(protocol.EventForgejoConnectionDeleted, workspaceID, "system", "", map[string]any{
		"id": id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// newForgejoWebhookSecret returns a 32-byte random secret as hex (64 chars),
// suitable for pasting into Forgejo's webhook "Secret" field.
func newForgejoWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
