package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/pkg/redact"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Gitea has no GitHub-App "installation" concept, so a connection is a plain
// workspace ↔ (instance base URL, PAT) binding. This file owns the backend
// connection management — connect (validate the PAT + persist encrypted),
// list, and disconnect — plus the neutral Gitea REST client used to validate
// the token. The PAT never leaves the server: list/broadcast responses carry
// only display metadata, and the stored token is sealed with the Gitea
// secretbox (MUL-2671 §4.4).

// GiteaConnectionResponse is the JSON shape returned by the list endpoint and
// broadcast on connection events. The PAT is deliberately absent — it is
// write-only from the API's perspective.
type GiteaConnectionResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	BaseURL          string  `json:"base_url"`
	AccountLogin     string  `json:"account_login"`
	AccountAvatarURL *string `json:"account_avatar_url"`
	CreatedAt        string  `json:"created_at"`
}

func giteaConnectionToResponse(c db.GiteaConnection) GiteaConnectionResponse {
	return GiteaConnectionResponse{
		ID:               uuidToString(c.ID),
		WorkspaceID:      uuidToString(c.WorkspaceID),
		BaseURL:          c.BaseUrl,
		AccountLogin:     c.AccountLogin,
		AccountAvatarURL: textToPtr(c.AccountAvatarUrl),
		CreatedAt:        timestampToString(c.CreatedAt),
	}
}

// giteaConnectRequest is the connect payload: the instance web base URL plus a
// personal access token to authenticate against it.
type giteaConnectRequest struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

// normalizeGiteaBaseURL trims and validates the instance URL, returning a
// canonical form (scheme + host + optional path, no trailing slash) so that
// `https://gitea.example.com/` and `https://gitea.example.com` map to the same
// UNIQUE(workspace_id, base_url) row.
func normalizeGiteaBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("base_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("base_url is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("base_url must start with http:// or https://")
	}
	if u.Host == "" {
		return "", errors.New("base_url must include a host")
	}
	// Drop any query/fragment and the trailing slash on the path.
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// giteaAccount is the subset of Gitea's `GET /api/v1/user` response we keep.
type giteaAccount struct {
	Login     string
	AvatarURL string
}

// verifyGiteaPAT calls the instance's `GET /api/v1/user` with the PAT to
// confirm both that the base URL is reachable and that the token is valid,
// returning the authenticated account for display. Any failure is surfaced as
// a user-actionable error; the token is never included in the error text.
func verifyGiteaPAT(ctx context.Context, baseURL, token string) (giteaAccount, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/user"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return giteaAccount{}, fmt.Errorf("could not reach the Gitea instance: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return giteaAccount{}, errors.New("could not reach the Gitea instance at that base URL")
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return giteaAccount{}, errors.New("the personal access token was rejected by the Gitea instance")
	case resp.StatusCode != http.StatusOK:
		return giteaAccount{}, fmt.Errorf("the Gitea instance returned an unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	// Cap the read so a non-Gitea endpoint at that URL can't stream an
	// unbounded body into the decoder.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return giteaAccount{}, errors.New("the base URL did not respond with a Gitea API payload")
	}
	if strings.TrimSpace(body.Login) == "" {
		return giteaAccount{}, errors.New("the base URL did not respond with a Gitea API payload")
	}
	return giteaAccount{Login: body.Login, AvatarURL: body.AvatarURL}, nil
}

// splitOwnerRepo parses "owner/repo" into its two non-empty segments. It rejects
// anything with a different segment count so a pasted URL or blank does not slip
// through as an owner or repo.
func splitOwnerRepo(s string) (owner, repo string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(s), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

type giteaHook struct {
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

// createGiteaRepoWebhook adds a pull_request webhook on {owner}/{repo} pointing
// at targetURL, using the connection's PAT. It is idempotent: if a hook already
// targets the same URL it returns created=false rather than adding a duplicate
// (Gitea does not dedupe hooks itself).
func createGiteaRepoWebhook(ctx context.Context, baseURL, token, owner, repo, targetURL, secret string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	hooksAPI := strings.TrimRight(baseURL, "/") + "/api/v1/repos/" + owner + "/" + repo + "/hooks"

	existing, err := giteaRepoHooks(ctx, hooksAPI, token)
	if err != nil {
		return false, err
	}
	for _, hook := range existing {
		if hook.Config.URL == targetURL {
			return false, nil
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"type":   "gitea",
		"active": true,
		"events": []string{"pull_request"},
		"config": map[string]string{
			"url":          targetURL,
			"content_type": "json",
			"secret":       secret,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hooksAPI, bytes.NewReader(payload))
	if err != nil {
		return false, errors.New("could not reach the Gitea instance")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, errors.New("could not reach the Gitea instance")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK:
		return true, nil
	default:
		return false, giteaHookStatusError(resp.StatusCode)
	}
}

func giteaRepoHooks(ctx context.Context, hooksAPI, token string) ([]giteaHook, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hooksAPI+"?limit=50", nil)
	if err != nil {
		return nil, errors.New("could not reach the Gitea instance")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("could not reach the Gitea instance")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, giteaHookStatusError(resp.StatusCode)
	}
	var hooks []giteaHook
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&hooks); err != nil {
		return nil, errors.New("the base URL did not respond with a Gitea API payload")
	}
	return hooks, nil
}

func giteaHookStatusError(status int) error {
	switch status {
	case http.StatusNotFound:
		return errors.New("repository not found, or the token cannot access it")
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("the token lacks permission to manage this repository's webhooks")
	default:
		return fmt.Errorf("the Gitea instance returned an unexpected status %d", status)
	}
}

// ── Connect ─────────────────────────────────────────────────────────────────

// GiteaConnect (POST /api/workspaces/{id}/gitea/connections) validates the PAT
// against the instance, then persists an encrypted connection row. Re-posting
// the same base_url refreshes the token in place.
func (h *Handler) GiteaConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if h.GiteaSecretBox == nil {
		writeError(w, http.StatusServiceUnavailable, "gitea integration is not configured on this server")
		return
	}

	var req giteaConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	baseURL, err := normalizeGiteaBaseURL(req.BaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	account, err := verifyGiteaPAT(r.Context(), baseURL, token)
	if err != nil {
		// redact.Text guards against the (unexpected) case of a secret echoed
		// back in the failure string before it reaches the client / logs.
		writeError(w, http.StatusBadRequest, redact.Text(err.Error()))
		return
	}

	sealed, err := h.GiteaSecretBox.Seal([]byte(token))
	if err != nil {
		slog.Error("gitea: failed to encrypt token", "err", err, "base_url", baseURL)
		writeError(w, http.StatusInternalServerError, "failed to store connection")
		return
	}

	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}
	conn, err := h.Queries.CreateGiteaConnection(r.Context(), db.CreateGiteaConnectionParams{
		WorkspaceID:      wsUUID,
		BaseUrl:          baseURL,
		TokenEncrypted:   sealed,
		AccountLogin:     account.Login,
		AccountAvatarUrl: ptrToText(strPtrOrNil(account.AvatarURL)),
		ConnectedByID:    connectedBy,
	})
	if err != nil {
		slog.Error("gitea: failed to persist connection", "err", err, "base_url", baseURL)
		writeError(w, http.StatusInternalServerError, "failed to store connection")
		return
	}

	resp := giteaConnectionToResponse(conn)
	h.publish(protocol.EventGiteaConnectionCreated, workspaceID, "system", "", map[string]any{
		"connection": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// ── Listing / disconnect ────────────────────────────────────────────────────

// ListGiteaConnections returns the workspace's Gitea connections to any member.
// Connect/disconnect are admin-only at the router; the response carries a
// can_manage hint so the UI can gate those affordances. No secret is ever
// included, so unlike GitHub there is nothing to strip per role.
func (h *Handler) ListGiteaConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	rows, err := h.Queries.ListGiteaConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	out := make([]GiteaConnectionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, giteaConnectionToResponse(row))
	}
	// webhook_url is the address operators paste into each Gitea repo's webhook.
	// Null when MULTICA_PUBLIC_URL is unset — the host is not knowable server-side
	// then, so the UI falls back to showing the path only.
	var webhookURL any
	if h.cfg.PublicURL != "" {
		webhookURL = h.cfg.PublicURL + "/api/webhooks/gitea"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections":        out,
		"configured":         h.GiteaSecretBox != nil,
		"can_manage":         canManage,
		"webhook_url":        webhookURL,
		"webhook_configured": giteaWebhookSecret() != "",
	})
}

type giteaCreateWebhookRequest struct {
	Repo string `json:"repo"`
}

// CreateGiteaWebhook (POST /gitea/connections/{connectionId}/hooks) uses the
// connection's stored PAT to add a pull_request webhook on {owner}/{repo}
// pointing back at this deployment. It writes to the user's Gitea repo, so it is
// opt-in and one repo at a time — the UI gates it behind an explicit toggle.
func (h *Handler) CreateGiteaWebhook(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	connUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	if h.GiteaSecretBox == nil {
		writeError(w, http.StatusServiceUnavailable, "gitea integration is not configured on this server")
		return
	}
	if h.cfg.PublicURL == "" {
		writeError(w, http.StatusBadRequest, "server has no public URL; set MULTICA_PUBLIC_URL before auto-creating webhooks")
		return
	}
	secret := giteaWebhookSecret()
	if secret == "" {
		writeError(w, http.StatusBadRequest, "webhook secret is unset; set MULTICA_GITEA_WEBHOOK_SECRET before auto-creating webhooks")
		return
	}

	var req giteaCreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	owner, repo, ok := splitOwnerRepo(req.Repo)
	if !ok {
		writeError(w, http.StatusBadRequest, "repo must be in owner/repo form")
		return
	}

	// No dedicated by-id query: the per-workspace list is small, so filter in Go.
	conns, err := h.Queries.ListGiteaConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load connection")
		return
	}
	var conn *db.GiteaConnection
	for i := range conns {
		if conns[i].ID == connUUID {
			conn = &conns[i]
			break
		}
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "gitea connection not found")
		return
	}
	tokenBytes, err := h.GiteaSecretBox.Open(conn.TokenEncrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read connection token")
		return
	}

	targetURL := h.cfg.PublicURL + "/api/webhooks/gitea"
	created, err := createGiteaRepoWebhook(r.Context(), conn.BaseUrl, string(tokenBytes), owner, repo, targetURL, secret)
	if err != nil {
		writeError(w, http.StatusBadGateway, redact.Text(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "already_exists": !created})
}

func (h *Handler) DeleteGiteaConnection(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Queries.DeleteGiteaConnection(r.Context(), db.DeleteGiteaConnectionParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove connection")
		return
	}
	h.publish(protocol.EventGiteaConnectionDeleted, workspaceID, "system", "", map[string]any{
		"id": id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── Webhook ─────────────────────────────────────────────────────────────────

// HandleGiteaWebhook (POST /api/webhooks/gitea) is the destination for every
// Gitea repo webhook. We verify X-Gitea-Signature against the shared secret,
// decode into a neutral GitEvent, and drive the same mirror pipeline GitHub
// uses (PR upsert + auto-link + merge→done). Gitea has no App-installation
// concept, so deliveries are attributed by instance base URL, not an id.
func (h *Handler) HandleGiteaWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	if giteaWebhookSecret() == "" {
		// Refusing to process is safer than treating an unconfigured
		// deployment as "all signatures valid".
		writeError(w, http.StatusServiceUnavailable, "gitea webhooks not configured")
		return
	}
	// The instance base URL is only routing metadata (used after verification);
	// deriving it from the yet-unverified body takes no action on its own.
	prov := newGiteaProvider(giteaInstanceBaseURL(body))
	if !prov.VerifySignature(r.Header, body) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	ev, err := prov.ParseEvent(r.Header, body)
	if err != nil {
		// Malformed body for a known event: ack so Gitea doesn't retry forever.
		slog.Warn("gitea: parse webhook failed", "err", err)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch ev.Kind {
	case GitEventPing:
		writeJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	case GitEventPullRequest:
		h.handleGiteaPullRequestEvent(r.Context(), prov, ev.PullRequest)
	default:
		// Acknowledge unmodeled events so Gitea doesn't mark the hook failing.
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleGiteaPullRequestEvent attributes a Gitea pull_request delivery to every
// workspace connected to the delivering instance, then mirrors it into each via
// the shared pipeline. base_url matching keys on the scheme+host derived from
// the payload; a subpath install (base_url with a path segment) is not matched.
// ponytail: host-scoped attribution, add path-aware matching if subpath Gitea installs need it.
func (h *Handler) handleGiteaPullRequestEvent(ctx context.Context, prov GitProvider, ev *GitPullRequestEvent) {
	baseURL := prov.InstanceBaseURL()
	if baseURL == "" {
		return
	}
	conns, err := h.Queries.ListGiteaConnectionsByBaseURL(ctx, baseURL)
	if err != nil {
		slog.Warn("gitea: lookup connection failed", "err", err)
		return
	}
	if len(conns) == 0 {
		// Delivery from an instance no workspace connected — nothing to
		// attribute, so drop it silently.
		return
	}
	for _, c := range conns {
		h.mirrorPullRequestForWorkspace(ctx, prov, c.WorkspaceID, 0, ev)
	}
}
