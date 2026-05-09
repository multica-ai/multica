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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Feishu Project (a.k.a. Meego, project.feishu.cn) plugin login.
//
// Distinct from /auth/feishu — this one is invoked from inside a Feishu
// Project plugin iframe (projectplg.feishupkg.com), where the JSSDK gives
// the page a short-lived `code`. The flow:
//
//	1. plugin_id + plugin_secret  →  plugin_token   (cached, ~2h TTL)
//	2. plugin_token + code        →  user_plugin_token + user_key
//	3. plugin_token + user_key    →  user info (email, name, avatar)
//	4. find/create user by email, return ship JWT in JSON body
//
// Bearer-token (not cookie) response shape: Safari's third-party cookie
// policy drops Set-Cookie when the iframe and ship are on different sites.
// The plugin frontend stores the JWT and sends it back via
// Authorization: Bearer <token>; ship's existing JWT middleware handles
// it just like a PAT, so no rewiring is needed downstream.
//
// Endpoints and field names verified against two open-source Meego clients:
//   - github.com/Roland0511/mcp-feishu-proj (archived; was prod-tested)
//   - github.com/lyonDan/feishu-proj-cli
const (
	meegoBaseURL              = "https://project.feishu.cn"
	meegoPluginTokenPath      = "/open_api/authen/plugin_token"
	meegoUserPluginTokenPath  = "/open_api/authen/user_plugin_token"
	meegoUserQueryPath        = "/open_api/user/query"
	pluginTokenRefreshSkew    = 5 * time.Minute
)

// FeishuPluginLoginRequest matches the body the plugin frontend sends.
// `plugin_id` is sent (not in env) because in theory a single ship deployment
// could front multiple Meego plugins; keeping it in the body lets us evolve
// without re-deploying. We still validate against an allowlist for safety.
type FeishuPluginLoginRequest struct {
	Code     string `json:"code"`
	PluginID string `json:"plugin_id"`
}

type meegoPluginTokenResponse struct {
	ErrCode int    `json:"err_code"`
	ErrMsg  string `json:"err_msg"`
	Data    struct {
		Token      string `json:"token"`
		ExpireTime int64  `json:"expire_time"` // seconds-from-now per Meego docs
	} `json:"data"`
}

// meegoUserPluginTokenResponse covers the response shape from both observed
// client implementations. user_key is the canonical Meego user identifier;
// access_token / user_plugin_token names varied across clients so accept either.
type meegoUserPluginTokenResponse struct {
	ErrCode int    `json:"err_code"`
	ErrMsg  string `json:"err_msg"`
	Data    struct {
		UserPluginToken string `json:"user_plugin_token"`
		AccessToken     string `json:"access_token"`
		RefreshToken    string `json:"refresh_token"`
		ExpiresIn       int    `json:"expires_in"`
		UserKey         string `json:"user_key"`
	} `json:"data"`
}

type meegoUser struct {
	UserKey   string `json:"user_key"`
	NameCN    string `json:"name_cn"`
	NameEN    string `json:"name_en"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

type meegoUserQueryResponse struct {
	ErrCode int         `json:"err_code"`
	ErrMsg  string      `json:"err_msg"`
	Data    []meegoUser `json:"data"`
}

// pluginTokenCache holds the short-lived plugin_token between requests. Only
// one entry per plugin_id is kept — this server doesn't yet front multiple
// Meego plugins, but the map keeps the design open without locking us in.
type pluginTokenEntry struct {
	token   string
	expires time.Time
}

var (
	pluginTokenMu    sync.Mutex
	pluginTokenCache = map[string]pluginTokenEntry{}
)

func (h *Handler) FeishuPluginLogin(w http.ResponseWriter, r *http.Request) {
	var req FeishuPluginLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	if req.PluginID == "" {
		writeError(w, http.StatusBadRequest, "plugin_id is required")
		return
	}

	// Plugin-id allowlist + secret resolution. Format:
	//   FEISHU_PROJECT_PLUGINS = "MII_69EB1A9FC1038BD0:<secret>,OTHER_ID:<secret>"
	// A single-plugin shortcut is also accepted via FEISHU_PROJECT_PLUGIN_ID +
	// FEISHU_PROJECT_PLUGIN_SECRET so the common case stays one secret entry.
	pluginSecret, ok := lookupPluginSecret(req.PluginID)
	if !ok {
		slog.Warn("feishu plugin login rejected: unknown plugin_id", "plugin_id", req.PluginID)
		writeError(w, http.StatusForbidden, "unknown plugin_id")
		return
	}

	pluginToken, err := getMeegoPluginToken(r.Context(), req.PluginID, pluginSecret)
	if err != nil {
		slog.Error("meego plugin_token fetch failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to obtain plugin_token")
		return
	}

	upt, err := exchangeMeegoUserPluginToken(r.Context(), pluginToken, req.Code)
	if err != nil {
		slog.Error("meego user_plugin_token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code")
		return
	}
	if upt.Data.UserKey == "" {
		slog.Error("meego user_plugin_token response missing user_key")
		writeError(w, http.StatusBadGateway, "Feishu Project did not return user identity")
		return
	}

	mu, err := fetchMeegoUserByKey(r.Context(), pluginToken, upt.Data.UserKey)
	if err != nil {
		slog.Error("meego user query failed", "error", err, "user_key", upt.Data.UserKey)
		writeError(w, http.StatusBadGateway, "failed to fetch user info")
		return
	}
	if mu.Email == "" {
		writeError(w, http.StatusBadRequest, "Feishu Project user has no email; cannot sign in")
		return
	}
	email := strings.ToLower(strings.TrimSpace(mu.Email))

	user, isNew, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if isNew {
		h.Analytics.Capture(analytics.Signup(uuidToString(user.ID), user.Email, "feishu_plugin"))
	}

	// Backfill display name + avatar on first sign-in or when user has the
	// email-prefix default. Mirrors auth_feishu.go behaviour.
	displayName := mu.NameCN
	if displayName == "" {
		displayName = mu.NameEN
	}
	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl
	if displayName != "" && user.Name == strings.Split(email, "@")[0] {
		newName = displayName
		needsUpdate = true
	}
	if mu.AvatarURL != "" && !user.AvatarUrl.Valid {
		newAvatar = pgtype.Text{String: mu.AvatarURL, Valid: true}
		needsUpdate = true
	}
	if needsUpdate {
		if updated, err := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:        user.ID,
			Name:      newName,
			AvatarUrl: newAvatar,
		}); err == nil {
			user = updated
		}
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("feishu-plugin login failed", append(logger.RequestAttrs(r), "error", err, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	slog.Info("user logged in via feishu plugin",
		append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email, "plugin_id", req.PluginID)...)

	// Bearer-token response shape (Zadig-style). Cookie-based auth doesn't
	// survive Safari's third-party cookie policy when the plugin runs inside
	// the projectplg.feishupkg.com iframe, so we return the JWT in JSON and
	// the plugin sends it back as Authorization: Bearer <token>.
	// expires_at matches issueJWT's 30-day exp claim.
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      tokenString,
		"user_key":   upt.Data.UserKey,
		"user":       userToResponse(user),
		"expires_at": time.Now().Add(30 * 24 * time.Hour).Unix(),
	})
}

// lookupPluginSecret returns the plugin_secret for the given plugin_id. It
// first checks FEISHU_PROJECT_PLUGINS (a comma-separated list of id:secret
// pairs), then falls back to the single-plugin env vars FEISHU_PROJECT_PLUGIN_ID
// + FEISHU_PROJECT_PLUGIN_SECRET. Returns ("", false) when the id isn't allowed.
func lookupPluginSecret(pluginID string) (string, bool) {
	if list := strings.TrimSpace(os.Getenv("FEISHU_PROJECT_PLUGINS")); list != "" {
		for _, pair := range strings.Split(list, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == pluginID {
				return strings.TrimSpace(parts[1]), true
			}
		}
	}
	if single := strings.TrimSpace(os.Getenv("FEISHU_PROJECT_PLUGIN_ID")); single != "" && single == pluginID {
		if secret := strings.TrimSpace(os.Getenv("FEISHU_PROJECT_PLUGIN_SECRET")); secret != "" {
			return secret, true
		}
	}
	return "", false
}

func getMeegoPluginToken(ctx context.Context, pluginID, pluginSecret string) (string, error) {
	pluginTokenMu.Lock()
	if entry, ok := pluginTokenCache[pluginID]; ok && time.Now().Add(pluginTokenRefreshSkew).Before(entry.expires) {
		token := entry.token
		pluginTokenMu.Unlock()
		return token, nil
	}
	pluginTokenMu.Unlock()

	body, _ := json.Marshal(map[string]string{
		"plugin_id":     pluginID,
		"plugin_secret": pluginSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meegoBaseURL+meegoPluginTokenPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("meego plugin_token http %d: %s", resp.StatusCode, string(raw))
	}

	var parsed meegoPluginTokenResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode plugin_token: %w", err)
	}
	if parsed.ErrCode != 0 || parsed.Data.Token == "" {
		return "", fmt.Errorf("meego plugin_token err_code=%d msg=%q", parsed.ErrCode, parsed.ErrMsg)
	}

	// expire_time is documented as seconds-from-now (typically 7200). Cache
	// with a short safety skew so we refresh before it actually expires.
	expiresAt := time.Now().Add(time.Duration(parsed.Data.ExpireTime) * time.Second)
	pluginTokenMu.Lock()
	pluginTokenCache[pluginID] = pluginTokenEntry{token: parsed.Data.Token, expires: expiresAt}
	pluginTokenMu.Unlock()

	return parsed.Data.Token, nil
}

func exchangeMeegoUserPluginToken(ctx context.Context, pluginToken, code string) (meegoUserPluginTokenResponse, error) {
	var out meegoUserPluginTokenResponse

	// Two observed Meego clients use slightly different field names ("code"
	// with grant_type vs "auth_code"). Sending both lets the server pick the
	// one it understands without flakiness — safe because the unused field is
	// ignored.
	body, _ := json.Marshal(map[string]string{
		"code":       code,
		"auth_code":  code,
		"grant_type": "authorization_code",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meegoBaseURL+meegoUserPluginTokenPath, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PLUGIN-TOKEN", pluginToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("meego user_plugin_token http %d: %s", resp.StatusCode, string(raw))
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("decode user_plugin_token: %w", err)
	}
	if out.ErrCode != 0 {
		return out, fmt.Errorf("meego user_plugin_token err_code=%d msg=%q", out.ErrCode, out.ErrMsg)
	}
	return out, nil
}

func fetchMeegoUserByKey(ctx context.Context, pluginToken, userKey string) (meegoUser, error) {
	body, _ := json.Marshal(map[string][]string{"user_keys": {userKey}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meegoBaseURL+meegoUserQueryPath, bytes.NewReader(body))
	if err != nil {
		return meegoUser{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PLUGIN-TOKEN", pluginToken)
	req.Header.Set("X-USER-KEY", userKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return meegoUser{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return meegoUser{}, fmt.Errorf("meego user query http %d: %s", resp.StatusCode, string(raw))
	}

	var parsed meegoUserQueryResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return meegoUser{}, fmt.Errorf("decode user query: %w", err)
	}
	if parsed.ErrCode != 0 {
		return meegoUser{}, fmt.Errorf("meego user query err_code=%d msg=%q", parsed.ErrCode, parsed.ErrMsg)
	}
	if len(parsed.Data) == 0 {
		return meegoUser{}, fmt.Errorf("meego user not found: %s", userKey)
	}
	return parsed.Data[0], nil
}
