package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// casdoorOAuthMaxBody is the maximum bytes read from Casdoor API responses.
const casdoorOAuthMaxBody = 1 << 20 // 1 MB

// casdoorStateCookieName is kept as a fallback for CSRF protection, but the
// primary verification uses HMAC-signed state embedded in the redirect URL.
// This avoids Chrome dropping the Set-Cookie during the cross-origin redirect
// chain (localhost:3000 → 127.0.0.1:8000 Casdoor → localhost:3000 callback).
const casdoorStateCookieName = "casdoor_oauth_state"

// casdoorTokenResponse is the JSON response from Casdoor's token endpoint.
type casdoorTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// casdoorUserInfo is the JSON response from Casdoor's userinfo endpoint.
type casdoorUserInfo struct {
	Sub               string `json:"sub"`
	Name              string `json:"name"`
	PreferredUsername  string `json:"preferred_username"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	Picture           string `json:"picture"`
}

// generateSignedState creates a CSRF state parameter signed with HMAC-SHA256.
// Format: "<nonce_hex>.<signature_hex>". The signature prevents tampering
// without needing a cookie (which Chrome may drop during cross-origin
// redirect chains like localhost → 127.0.0.1 Casdoor → localhost callback).
func generateSignedState() (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	nonceHex := hex.EncodeToString(nonce)
	mac := hmac.New(sha256.New, []byte(auth.JWTSecret()))
	mac.Write([]byte(nonceHex))
	sig := hex.EncodeToString(mac.Sum(nil))
	return nonceHex + "." + sig, nil
}

// validateSignedState verifies the HMAC signature of a state parameter.
func validateSignedState(state string) bool {
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(auth.JWTSecret()))
	mac.Write([]byte(parts[0]))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

// CasdoorLogin initiates the Casdoor OAuth2 authorization code flow.
// It generates an HMAC-signed state parameter for CSRF protection and
// redirects the user to Casdoor's authorize endpoint. A cookie is also set
// as a fallback for browsers that preserve it through the redirect chain.
func (h *Handler) CasdoorLogin(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfg

	if cfg.CasdoorEndpoint == "" || cfg.CasdoorClientID == "" {
		writeError(w, http.StatusServiceUnavailable, "Casdoor SSO is not configured")
		return
	}

	// Generate HMAC-signed state for CSRF protection.
	state, err := generateSignedState()
	if err != nil {
		slog.Error("casdoor: failed to generate state", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	// Also set state cookie as fallback (some browsers preserve it).
	http.SetCookie(w, &http.Cookie{
		Name:     casdoorStateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})

	// Build redirect URI. If not explicitly configured, derive from request.
	redirectURI := cfg.CasdoorRedirectURI
	if redirectURI == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		redirectURI = fmt.Sprintf("%s://%s/auth/casdoor/callback", scheme, r.Host)
	}

	// Build Casdoor authorize URL.
	authorizeURL, err := url.Parse(cfg.CasdoorEndpoint + "/login/oauth/authorize")
	if err != nil {
		slog.Error("casdoor: invalid endpoint URL", "error", err, "endpoint", cfg.CasdoorEndpoint)
		writeError(w, http.StatusInternalServerError, "invalid Casdoor endpoint")
		return
	}
	q := authorizeURL.Query()
	q.Set("client_id", cfg.CasdoorClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email")
	q.Set("state", state)
	if cfg.CasdoorOrgName != "" {
		q.Set("organization", cfg.CasdoorOrgName)
	}
	authorizeURL.RawQuery = q.Encode()

	http.Redirect(w, r, authorizeURL.String(), http.StatusFound)
}

// CasdoorCallback handles the OAuth2 callback from Casdoor. It validates the
// state parameter, exchanges the authorization code for an access token,
// fetches user info, finds or provisions the Multica user, and sets session
// cookies before redirecting to the frontend.
func (h *Handler) CasdoorCallback(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfg

	if cfg.CasdoorEndpoint == "" || cfg.CasdoorClientID == "" {
		writeError(w, http.StatusServiceUnavailable, "Casdoor SSO is not configured")
		return
	}

	// Validate state parameter (CSRF protection).
	// Primary: verify HMAC signature (works even when Chrome drops the cookie
	// during the cross-origin redirect chain).
	// Fallback: compare against cookie value for backward compatibility.
	stateParam := r.URL.Query().Get("state")
	if stateParam == "" {
		writeError(w, http.StatusBadRequest, "missing state parameter")
		return
	}
	if !validateSignedState(stateParam) {
		// Fallback: check cookie (set by login endpoint).
		stateCookie, cookieErr := r.Cookie(casdoorStateCookieName)
		if cookieErr != nil || stateCookie.Value != stateParam {
			writeError(w, http.StatusBadRequest, "invalid state parameter")
			return
		}
	}
	// Clear the state cookie if present.
	http.SetCookie(w, &http.Cookie{
		Name:     casdoorStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	// Check for error response from Casdoor.
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		errDesc := r.URL.Query().Get("error_description")
		slog.Warn("casdoor: authorization error", "code", errCode, "description", errDesc)
		writeError(w, http.StatusUnauthorized, fmt.Sprintf("Casdoor authorization failed: %s", errCode))
		return
	}

	// Get authorization code.
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing authorization code")
		return
	}

	// Build redirect URI (must match the one used in login).
	redirectURI := cfg.CasdoorRedirectURI
	if redirectURI == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		redirectURI = fmt.Sprintf("%s://%s/auth/casdoor/callback", scheme, r.Host)
	}

	// Exchange code for access token.
	tokenResp, err := h.exchangeCasdoorCode(r, cfg, code, redirectURI)
	if err != nil {
		slog.Error("casdoor: token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange authorization code")
		return
	}

	// Fetch user info using access token.
	userInfo, err := h.fetchCasdoorUserInfo(r, cfg, tokenResp.AccessToken)
	if err != nil {
		slog.Error("casdoor: user info fetch failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user info")
		return
	}

	if userInfo.Sub == "" {
		writeError(w, http.StatusBadGateway, "Casdoor returned empty subject ID")
		return
	}

	// Find or create Multica user by subject_id.
	user, isNew, err := h.findOrCreateCasdoorUser(r, userInfo)
	if err != nil {
		slog.Error("casdoor: user provisioning failed", "error", err, "subject_id", userInfo.Sub)
		writeError(w, http.StatusInternalServerError, "failed to provision user")
		return
	}
	if isNew {
		slog.Info("casdoor: auto-provisioned user", "user_id", uuidToString(user.ID), "subject_id", userInfo.Sub)
		h.Bus.Publish(events.Event{
			Type:      "user.signup",
			ActorType: "system",
			Payload: map[string]any{
				"user_id": uuidToString(user.ID),
				"source":  "casdoor",
			},
		})
	}

	// Issue Multica JWT and set cookies.
	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Error("casdoor: JWT generation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate session token")
		return
	}

	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Error("casdoor: failed to set auth cookies", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set session cookies")
		return
	}

	slog.Info("casdoor: user logged in", "user_id", uuidToString(user.ID), "email", userInfo.Email)

	// Redirect to frontend. Strip the callback path from the configured
	// redirect URI so the browser lands on the app root (including any
	// basePath prefix such as /multica-web).
	frontendOrigin := "/"
	if cfg.CasdoorRedirectURI != "" {
		if u, err := url.Parse(cfg.CasdoorRedirectURI); err == nil {
			callbackPath := "/auth/casdoor/callback"
			basePath := u.Path
			if strings.HasSuffix(basePath, callbackPath) {
				basePath = strings.TrimSuffix(basePath, callbackPath)
			}
			if basePath == "" {
				basePath = "/"
			}
			frontendOrigin = u.Scheme + "://" + u.Host + basePath
		}
	}
	http.Redirect(w, r, frontendOrigin, http.StatusFound)
}

// exchangeCasdoorCode exchanges an authorization code for an access token.
func (h *Handler) exchangeCasdoorCode(r *http.Request, cfg Config, code, redirectURI string) (*casdoorTokenResponse, error) {
	tokenURL := cfg.CasdoorEndpoint + "/api/login/oauth/access_token"

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {cfg.CasdoorClientID},
		"client_secret": {cfg.CasdoorClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, casdoorOAuthMaxBody)

	var tokenResp casdoorTokenResponse
	if err := json.NewDecoder(body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}

	if tokenResp.Error != "" {
		return nil, fmt.Errorf("casdoor token error: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("casdoor returned empty access token")
	}

	return &tokenResp, nil
}

// fetchCasdoorUserInfo fetches user info from Casdoor using the access token.
func (h *Handler) fetchCasdoorUserInfo(r *http.Request, cfg Config, accessToken string) (*casdoorUserInfo, error) {
	userInfoURL := cfg.CasdoorEndpoint + "/api/userinfo"

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, casdoorOAuthMaxBody))
		return nil, fmt.Errorf("userinfo returned status %d: %s", resp.StatusCode, string(body))
	}

	body := io.LimitReader(resp.Body, casdoorOAuthMaxBody)

	var userInfo casdoorUserInfo
	if err := json.NewDecoder(body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("decoding userinfo: %w", err)
	}

	return &userInfo, nil
}

// findOrCreateCasdoorUser finds a Multica user by Casdoor subject_id, or
// creates one if not found. Uses email from Casdoor when available, falling
// back to a synthetic address based on the subject ID.
func (h *Handler) findOrCreateCasdoorUser(r *http.Request, info *casdoorUserInfo) (user db.MulticaUser, isNew bool, err error) {
	ctx := r.Context()
	subjectID := pgtype.Text{String: info.Sub, Valid: true}

	// Try to find existing user by subject_id.
	existing, err := h.Queries.GetUserBySubjectID(ctx, subjectID)
	if err == nil {
		return existing, false, nil
	}

	// Not found — create a new user.
	email := info.Email
	if email == "" {
		email = info.Sub + "@casdoor.local"
	}
	name := info.Name
	if name == "" {
		name = info.PreferredUsername
	}
	if name == "" {
		name = "casdoor-" + info.Sub[:min(8, len(info.Sub))]
	}

	user, err = h.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:  name,
		Email: email,
	})
	if err != nil {
		// Race condition: another request created the user between our
		// lookup and insert. Re-fetch by subject_id.
		if isUniqueViolation(err) {
			existing, findErr := h.Queries.GetUserBySubjectID(ctx, subjectID)
			if findErr == nil {
				return existing, false, nil
			}
		}
		return user, false, fmt.Errorf("create user: %w", err)
	}

	// Set subject_id on the newly created user.
	if err := h.Queries.SetUserSubjectID(ctx, db.SetUserSubjectIDParams{
		ID:        user.ID,
		SubjectID: subjectID,
	}); err != nil {
		slog.Warn("casdoor: failed to set subject_id on new user", "error", err, "user_id", uuidToString(user.ID))
	}

	return user, true, nil
}
