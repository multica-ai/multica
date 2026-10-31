package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// Personal GitHub account connection (MUL-8433). A member connects their own
// GitHub account once; PR writes made from Multica then go to GitHub as that
// person. The connection is per user and shared across workspaces.

const (
	githubUserCallbackPath = "/api/github/user/callback"
	maxGitHubUserReturnTo  = 2048
)

type GitHubAccountResponse struct {
	// Configured is false when the deployment lacks GITHUB_APP_CLIENT_ID,
	// GITHUB_APP_CLIENT_SECRET or MULTICA_GITHUB_SECRET_KEY.
	Configured bool `json:"configured"`
	// Connected is false when no account is stored or the stored authorization
	// can no longer be renewed.
	Connected bool    `json:"connected"`
	Login     *string `json:"login"`
	AvatarURL *string `json:"avatar_url"`
}

type GitHubAccountConnectResponse struct {
	URL string `json:"url"`
}

func writeGitHubUserAuthNotConfigured(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusServiceUnavailable, "github_user_auth_not_configured",
		"GitHub account connection is not configured on this server")
}

// GetMyGitHubAccount (GET /api/me/github) reports the caller's connection
// without contacting GitHub.
func (h *Handler) GetMyGitHubAccount(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if !h.GitHubUser.Configured() {
		writeJSON(w, http.StatusOK, GitHubAccountResponse{})
		return
	}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	account, connected, err := h.GitHubUser.Status(r.Context(), uid)
	if err != nil {
		slog.Warn("github account: status lookup failed", "user_id", userID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load GitHub account")
		return
	}
	resp := GitHubAccountResponse{Configured: true, Connected: connected}
	if connected {
		resp.Login = &account.Login
		if account.AvatarURL != "" {
			resp.AvatarURL = &account.AvatarURL
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ConnectMyGitHubAccount (GET /api/me/github/connect?return_to=) returns the
// GitHub authorize URL. The signed state binds the callback to the caller and
// to where the browser returns afterwards.
func (h *Handler) ConnectMyGitHubAccount(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if !h.GitHubUser.Configured() {
		writeGitHubUserAuthNotConfigured(w)
		return
	}
	returnTo, ok := normalizeGitHubUserReturnTo(r.URL.Query().Get("return_to"))
	if !ok {
		writeError(w, http.StatusBadRequest, "return_to must be a path in the Multica web app")
		return
	}
	state, err := h.GitHubUser.SignState(userID, returnTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	writeJSON(w, http.StatusOK, GitHubAccountConnectResponse{
		URL: h.GitHubUser.AuthorizeURL(state, h.githubUserRedirectURI()),
	})
}

// GitHubUserCallback (GET /api/github/user/callback) is GitHub's browser
// redirect after the user authorizes. It is public: the signed state carries
// the Multica user. Like the App install callback, it always ends in a
// redirect back to the web app, with github_account_connected=1 or
// github_account_error=<reason> appended.
func (h *Handler) GitHubUserCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	origin := h.githubUserReturnOrigin()
	fail := func(returnTo, reason string) {
		http.Redirect(w, r, githubUserReturnURL(origin, returnTo, "github_account_error", reason), http.StatusFound)
	}
	if !h.GitHubUser.Configured() {
		fail("/", "not_configured")
		return
	}
	stateToken := q.Get("state")
	if stateToken == "" {
		fail("/", "missing_params")
		return
	}
	state, err := h.GitHubUser.VerifyState(stateToken)
	if err != nil {
		fail("/", "invalid_state")
		return
	}
	returnTo, ok := normalizeGitHubUserReturnTo(state.ReturnTo)
	if !ok {
		returnTo = "/"
	}
	if ghErr := q.Get("error"); ghErr != "" {
		reason := "authorization_failed"
		if ghErr == "access_denied" {
			reason = "access_denied"
		}
		fail(returnTo, reason)
		return
	}
	code := q.Get("code")
	if code == "" {
		fail(returnTo, "missing_params")
		return
	}
	uid, err := util.ParseUUID(state.UserID)
	if err != nil {
		fail(returnTo, "invalid_state")
		return
	}
	if _, err := h.GitHubUser.Connect(r.Context(), uid, code, h.githubUserRedirectURI()); err != nil {
		slog.Warn("github account: connect failed", "user_id", state.UserID, "err", err)
		fail(returnTo, "exchange_failed")
		return
	}
	http.Redirect(w, r, githubUserReturnURL(origin, returnTo, "github_account_connected", "1"), http.StatusFound)
}

// DisconnectMyGitHubAccount (DELETE /api/me/github) revokes the grant on
// GitHub and deletes the stored connection, even when revocation fails.
func (h *Handler) DisconnectMyGitHubAccount(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if !h.GitHubUser.Configured() {
		writeGitHubUserAuthNotConfigured(w)
		return
	}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	revoked, err := h.GitHubUser.Disconnect(r.Context(), uid)
	if err != nil {
		slog.Warn("github account: disconnect failed", "user_id", userID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to disconnect GitHub account")
		return
	}
	if !revoked {
		slog.Info("github account: disconnected without revoking the grant on GitHub", "user_id", userID)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// githubUserRedirectURI is sent with the authorize and token requests when the
// API's public URL is known; otherwise GitHub uses the App's registered
// callback URL.
func (h *Handler) githubUserRedirectURI() string {
	if h.cfg.PublicURL == "" {
		return ""
	}
	return h.cfg.PublicURL + githubUserCallbackPath
}

func (h *Handler) githubUserReturnOrigin() string {
	for _, origin := range []string{h.cfg.AppURL, os.Getenv("FRONTEND_ORIGIN")} {
		if origin = strings.TrimRight(strings.TrimSpace(origin), "/"); origin != "" {
			return origin
		}
	}
	return "http://localhost:3000"
}

// normalizeGitHubUserReturnTo accepts only a path inside the web app, so the
// callback can never redirect to another origin. Empty means the app root.
func normalizeGitHubUserReturnTo(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/", true
	}
	if len(raw) > maxGitHubUserReturnTo || !strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, "//") || strings.Contains(raw, `\`) {
		return "", false
	}
	for _, c := range raw {
		if c < 0x20 || c == 0x7f {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return "", false
	}
	return raw, true
}

func githubUserReturnURL(origin, returnTo, key, value string) string {
	u, err := url.Parse(returnTo)
	if err != nil {
		u = &url.URL{Path: "/"}
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return origin + u.String()
}
