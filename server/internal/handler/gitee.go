package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Gitee Webhook Config CRUD ───────────────────────────────────────────────

type GiteeWebhookConfigResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	Secret      string `json:"secret"`
	WebhookURL  string `json:"webhook_url"`
	CreatedAt   string `json:"created_at"`
}

func giteeWebhookConfigToResponse(c db.GiteeWebhookConfig, baseURL string) GiteeWebhookConfigResponse {
	return GiteeWebhookConfigResponse{
		ID:          uuidToString(c.ID),
		WorkspaceID: uuidToString(c.WorkspaceID),
		RepoOwner:   c.RepoOwner,
		RepoName:    c.RepoName,
		Secret:      c.Secret,
		WebhookURL:  baseURL + "/api/webhooks/gitee",
		CreatedAt:   timestampToString(c.CreatedAt),
	}
}

type createGiteeWebhookConfigRequest struct {
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

func (h *Handler) CreateGiteeWebhookConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req createGiteeWebhookConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RepoOwner == "" || req.RepoName == "" {
		writeError(w, http.StatusBadRequest, "repo_owner and repo_name are required")
		return
	}

	// Generate a 32-byte hex secret for webhook verification.
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	secret := hex.EncodeToString(secretBytes)

	cfg, err := h.Queries.CreateGiteeWebhookConfig(r.Context(), db.CreateGiteeWebhookConfigParams{
		WorkspaceID: wsUUID,
		RepoOwner:   req.RepoOwner,
		RepoName:    req.RepoName,
		Secret:      secret,
	})
	if err != nil {
		slog.Error("gitee: create webhook config failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create config")
		return
	}

	baseURL := h.getBaseURL(r)
	writeJSON(w, http.StatusCreated, giteeWebhookConfigToResponse(cfg, baseURL))
}

func (h *Handler) ListGiteeWebhookConfigs(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListGiteeWebhookConfigsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list configs")
		return
	}

	baseURL := h.getBaseURL(r)
	out := make([]GiteeWebhookConfigResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, giteeWebhookConfigToResponse(row, baseURL))
	}
	writeJSON(w, http.StatusOK, map[string]any{"configs": out})
}

func (h *Handler) DeleteGiteeWebhookConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	configID := chi.URLParam(r, "configId")
	configUUID, ok := parseUUIDOrBadRequest(w, configID, "config id")
	if !ok {
		return
	}

	if err := h.Queries.DeleteGiteeWebhookConfig(r.Context(), db.DeleteGiteeWebhookConfigParams{
		ID:          configUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getBaseURL derives the base URL from the request or FRONTEND_ORIGIN env.
func (h *Handler) getBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + r.Host
}

// ── Gitee Webhook Handler ───────────────────────────────────────────────────

// HandleGiteeWebhook (POST /api/webhooks/gitee) receives Gitee Merge Request
// Hook events, verifies the secret, normalizes the payload, and processes it
// through the shared PR pipeline.
func (h *Handler) HandleGiteeWebhook(w http.ResponseWriter, r *http.Request) {
	// Handle Gitee ping/test events. Gitee sends X-Gitee-Ping: true with a
	// dummy payload (oschina/git-osc) that won't match any real config.
	if r.Header.Get("X-Gitee-Ping") == "true" {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	// Parse payload to extract repo info for secret lookup.
	var envelope giteeWebhookEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	// Gitee URLs are namespace/path (e.g. "wujie-agent/multica").
	// Prefer namespace over owner.login, and path over name (display name).
	repoOwner := envelope.Repository.Namespace
	if repoOwner == "" {
		repoOwner = envelope.Repository.Owner.Login
	}
	repoName := envelope.Repository.Path
	if repoName == "" {
		repoName = envelope.Repository.Name
	}
	if repoOwner == "" || repoName == "" {
		writeError(w, http.StatusBadRequest, "cannot determine repo owner/name from payload")
		return
	}

	// Look up the webhook config to get the shared secret.
	cfg, err := h.Queries.GetGiteeWebhookConfigByRepo(r.Context(), db.GetGiteeWebhookConfigByRepoParams{
		RepoOwner: repoOwner,
		RepoName:  repoName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no webhook config for this repo")
			return
		}
		slog.Error("gitee: lookup config failed", "err", err)
		writeError(w, http.StatusInternalServerError, "config lookup failed")
		return
	}

	// Verify the webhook token. Gitee uses X-Gitee-Token header with
	// the configured password/secret (plain comparison or HMAC depending
	// on configuration). We support both plain secret match and HMAC-SHA256.
	tokenHeader := r.Header.Get("X-Gitee-Token")
	timestamp := r.Header.Get("X-Gitee-Timestamp")
	if !verifyGiteeToken(cfg.Secret, tokenHeader, timestamp) {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Filter: only process Merge Request Hook events.
	event := r.Header.Get("X-Gitee-Event")
	if event != "Merge Request Hook" {
		// Acknowledge but ignore non-PR events.
		writeJSON(w, http.StatusOK, map[string]string{"ok": "ignored"})
		return
	}

	// Parse the full merge request payload.
	var p giteeMergeRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("gitee: bad merge_request payload", "err", err)
		writeError(w, http.StatusBadRequest, "invalid merge request payload")
		return
	}

	h.processGiteeMergeRequest(r.Context(), cfg, p)
	w.WriteHeader(http.StatusAccepted)
}

// verifyGiteeToken checks the X-Gitee-Token header against our stored secret.
// Gitee supports two webhook authentication modes:
//   - Password mode (密码): X-Gitee-Token == configured secret
//   - Sign mode (签名密钥): X-Gitee-Token == Base64(HMAC-SHA256(timestamp+"\n"+secret))
//
// We try both: first constant-time password comparison, then HMAC sign verification.
func verifyGiteeToken(secret, token, timestamp string) bool {
	if secret == "" || token == "" {
		return false
	}
	// Password mode: direct constant-time comparison.
	if hmac.Equal([]byte(secret), []byte(token)) {
		return true
	}
	// Sign mode: token = Base64(HMAC-SHA256(key=secret, msg=timestamp+"\n"+secret))
	if timestamp != "" {
		stringToSign := timestamp + "\n" + secret
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(expected), []byte(token))
	}
	return false
}

// ── Gitee payload structs ───────────────────────────────────────────────────

type giteeWebhookEnvelope struct {
	Repository struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		FullName  string `json:"full_name"`
		Namespace string `json:"namespace"`
		Owner     struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

type giteeMergeRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		ID        int64  `json:"id"`
		Number    int32  `json:"number"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		State     string `json:"state"`
		HTMLURL   string `json:"html_url"`
		Head      struct {
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		MergedAt  string `json:"merged_at"`
		ClosedAt  string `json:"closed_at"`
	} `json:"pull_request"`
	Repository struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		FullName  string `json:"full_name"`
		Namespace string `json:"namespace"`
		Owner     struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func (h *Handler) processGiteeMergeRequest(ctx context.Context, cfg db.GiteeWebhookConfig, p giteeMergeRequestPayload) {
	pr := p.PullRequest

	// Determine state: Gitee uses merged_at to signal merged.
	state := deriveGiteePRState(pr.State, pr.MergedAt)

	var mergedAt *time.Time
	if t, err := time.Parse(time.RFC3339, pr.MergedAt); err == nil {
		mergedAt = &t
	}
	var closedAt *time.Time
	if t, err := time.Parse(time.RFC3339, pr.ClosedAt); err == nil {
		closedAt = &t
	}
	createdAt := time.Now().UTC()
	if t, err := time.Parse(time.RFC3339, pr.CreatedAt); err == nil {
		createdAt = t
	}
	updatedAt := time.Now().UTC()
	if t, err := time.Parse(time.RFC3339, pr.UpdatedAt); err == nil {
		updatedAt = t
	}

	repoOwner := p.Repository.Namespace
	if repoOwner == "" {
		repoOwner = p.Repository.Owner.Login
	}
	repoName := p.Repository.Path
	if repoName == "" {
		repoName = p.Repository.Name
	}

	evt := NormalizedPREvent{
		Provider:        "gitee",
		WorkspaceID:     cfg.WorkspaceID,
		InstallationID:  pgtype.Int8{}, // null for Gitee
		RepoOwner:       repoOwner,
		RepoName:        repoName,
		Number:          pr.Number,
		Title:           pr.Title,
		Body:            pr.Body,
		HTMLURL:         pr.HTMLURL,
		SourceBranch:    pr.Head.Ref,
		AuthorLogin:     pr.User.Login,
		AuthorAvatarURL: pr.User.AvatarURL,
		State:           state,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		MergedAt:        mergedAt,
		ClosedAt:        closedAt,
		HeadSHA:         "", // Gitee doesn't always provide head SHA in webhook
		MergeableState:  pgtype.Text{},
		ClearMergeable:  false,
		Additions:       0, // Gitee webhook doesn't provide diff stats
		Deletions:       0,
		ChangedFiles:    0,
	}

	// Auto-done is disabled for Gitee by default (conservative).
	h.ProcessPullRequestEvent(ctx, evt, false)
}

// deriveGiteePRState maps Gitee's PR state to our internal state.
// Gitee: merged_at != nil → "merged"; state == "closed" → "closed"; else "open"
func deriveGiteePRState(state, mergedAt string) string {
	if mergedAt != "" {
		return "merged"
	}
	if state == "closed" {
		return "closed"
	}
	return "open"
}
