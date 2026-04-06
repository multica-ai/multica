package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxOAuthResponseSize limits provider API responses to 1 MB.
const maxOAuthResponseSize = 1 << 20

// oauthParams holds redirect parameters stored in the oauth_params cookie.
type oauthParams struct {
	NextURL     string `json:"next_url,omitempty"`
	CLICallback string `json:"cli_callback,omitempty"`
	CLIState    string `json:"cli_state,omitempty"`
}

// oauthRedirectURL constructs the OAuth callback URL from MULTICA_SERVER_URL.
// MULTICA_SERVER_URL is always set (e.g. "ws://localhost:8080/ws") —
// we convert ws(s):// → http(s):// and replace the path with /auth/callback.
func oauthRedirectURL() string {
	return oauthRedirectURLFrom(os.Getenv("MULTICA_SERVER_URL"))
}

// oauthRedirectURLFrom derives the OAuth callback URL from the given server URL.
// Exported for testing via the unexported function name convention (tested directly).
func oauthRedirectURLFrom(serverURL string) string {
	if serverURL == "" {
		return "http://localhost:8080/auth/callback"
	}

	// Convert ws(s) scheme to http(s).
	u, err := url.Parse(serverURL)
	if err != nil {
		return "http://localhost:8080/auth/callback"
	}

	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}

	// Replace any path (e.g. "/ws") with our callback path.
	u.Path = "/auth/callback"
	u.RawQuery = ""
	u.Fragment = ""

	return u.String()
}

// oauthProviderConfig returns the oauth2.Config for the given provider name.
// Returns nil if the provider is unknown or not configured.
func oauthProviderConfig(provider string) *oauth2.Config {
	switch provider {
	case "google":
		clientID := os.Getenv("GOOGLE_CLIENT_ID")
		clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
		if clientID == "" || clientSecret == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     google.Endpoint,
			RedirectURL:  oauthRedirectURL(),
			Scopes:       []string{"openid", "email", "profile"},
		}
	case "github":
		clientID := os.Getenv("GITHUB_CLIENT_ID")
		clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
		if clientID == "" || clientSecret == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     github.Endpoint,
			RedirectURL:  oauthRedirectURL(),
			Scopes:       []string{"user:email"},
		}
	default:
		return nil
	}
}

func generateOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// isSecureRequest returns true if the request was made over HTTPS,
// accounting for reverse proxies that set X-Forwarded-Proto.
func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// isValidCLICallback validates that a CLI callback URL points to localhost.
func isValidCLICallback(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1"
}

// isValidNextPath validates that a redirect path is a relative path (no open redirect).
func isValidNextPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//")
}

// OAuthStart handles GET /auth/{provider} — redirects the user to the provider's consent screen.
func (h *Handler) OAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	cfg := oauthProviderConfig(provider)
	if cfg == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported or unconfigured oauth provider: %s", provider))
		return
	}

	state, err := generateOAuthState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	// Encode the provider in the state so the callback knows which provider was used.
	// Format: "provider:randomState"
	stateWithProvider := provider + ":" + state

	// Preserve next and cli_callback query params through the OAuth flow via the state cookie.
	nextURL := r.URL.Query().Get("next")
	cliCallback := r.URL.Query().Get("cli_callback")
	cliState := r.URL.Query().Get("cli_state")

	// Validate cli_callback to prevent open redirect attacks.
	if cliCallback != "" && !isValidCLICallback(cliCallback) {
		writeError(w, http.StatusBadRequest, "invalid cli_callback URL: must be http://localhost or http://127.0.0.1")
		return
	}

	// Validate next path to prevent open redirect attacks.
	if nextURL != "" && !isValidNextPath(nextURL) {
		nextURL = ""
	}

	secure := isSecureRequest(r)

	// Store state in a short-lived cookie for CSRF validation.
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    stateWithProvider,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})

	// Store redirect params in a separate cookie (base64-encoded JSON for safe encoding).
	if nextURL != "" || cliCallback != "" {
		paramsJSON, _ := json.Marshal(oauthParams{
			NextURL:     nextURL,
			CLICallback: cliCallback,
			CLIState:    cliState,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_params",
			Value:    base64.RawURLEncoding.EncodeToString(paramsJSON),
			Path:     "/",
			MaxAge:   600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   secure,
		})
	}

	url := cfg.AuthCodeURL(stateWithProvider)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// OAuthCallback handles GET /auth/callback — exchanges the code for a token, fetches user info, issues JWT.
func (h *Handler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	// Read and validate state.
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing oauth state cookie")
		return
	}

	stateParam := r.URL.Query().Get("state")
	if stateParam == "" || stateParam != stateCookie.Value {
		writeError(w, http.StatusBadRequest, "invalid oauth state")
		return
	}

	// Extract provider from state ("provider:random").
	parts := strings.SplitN(stateParam, ":", 2)
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "malformed oauth state")
		return
	}
	provider := parts[0]

	cfg := oauthProviderConfig(provider)
	if cfg == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported provider: %s", provider))
		return
	}

	// Check for error from provider.
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		slog.Warn("oauth provider returned error", "provider", provider, "error", errParam, "description", desc)
		frontendURL := os.Getenv("MULTICA_APP_URL")
		if frontendURL == "" {
			frontendURL = "http://localhost:3000"
		}
		http.Redirect(w, r, frontendURL+"/login?error=oauth_denied", http.StatusTemporaryRedirect)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing authorization code")
		return
	}

	// Exchange code for token.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	oauthToken, err := cfg.Exchange(ctx, code)
	if err != nil {
		slog.Error("oauth token exchange failed", "provider", provider, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to exchange authorization code")
		return
	}

	// Fetch user email from provider.
	email, name, avatarURL, err := fetchOAuthUserInfo(ctx, provider, cfg, oauthToken)
	if err != nil {
		slog.Error("failed to fetch oauth user info", "provider", provider, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch user info from provider")
		return
	}

	if email == "" {
		writeError(w, http.StatusBadRequest, "could not retrieve email from oauth provider")
		return
	}

	// Find or create user (reuses existing auth logic — auto-links by email).
	user, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Update name and avatar if user was just created (name matches email prefix pattern).
	if name != "" && user.Name == strings.Split(email, "@")[0] {
		updated, updateErr := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:        user.ID,
			Name:      name,
			AvatarUrl: strToText(avatarURL),
		})
		if updateErr == nil {
			user = updated
		}
	} else if avatarURL != "" && !user.AvatarUrl.Valid {
		updated, updateErr := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:        user.ID,
			Name:      user.Name,
			AvatarUrl: strToText(avatarURL),
		})
		if updateErr == nil {
			user = updated
		}
	}

	if err := h.ensureUserWorkspace(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to provision workspace")
		return
	}

	jwtToken, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("oauth login failed to issue jwt", append(logger.RequestAttrs(r), "error", err, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Set CloudFront signed cookies for CDN access.
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(72 * time.Hour)) {
			http.SetCookie(w, cookie)
		}
	}

	// Read redirect params from cookie (base64-encoded JSON).
	nextURL := ""
	cliCallback := ""
	cliState := ""
	if paramsCookie, paramErr := r.Cookie("oauth_params"); paramErr == nil {
		if decoded, decErr := base64.RawURLEncoding.DecodeString(paramsCookie.Value); decErr == nil {
			var params oauthParams
			if json.Unmarshal(decoded, &params) == nil {
				nextURL = params.NextURL
				cliCallback = params.CLICallback
				cliState = params.CLIState
			}
		}
	}

	// Clear OAuth cookies.
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "oauth_params", Path: "/", MaxAge: -1})

	slog.Info("user logged in via oauth", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email, "provider", provider)...)

	// For CLI callback flow, redirect directly to the CLI.
	if cliCallback != "" {
		if !isValidCLICallback(cliCallback) {
			writeError(w, http.StatusBadRequest, "invalid cli_callback URL")
			return
		}
		sep := "?"
		if strings.Contains(cliCallback, "?") {
			sep = "&"
		}
		http.Redirect(w, r, cliCallback+sep+"token="+url.QueryEscape(jwtToken)+"&state="+url.QueryEscape(cliState), http.StatusTemporaryRedirect)
		return
	}

	// Redirect to frontend with token in query param.
	frontendURL := os.Getenv("MULTICA_APP_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	redirectPath := "/login"
	if nextURL != "" && isValidNextPath(nextURL) {
		redirectPath = nextURL
	}
	http.Redirect(w, r, frontendURL+"/auth/callback?token="+url.QueryEscape(jwtToken)+"&next="+url.QueryEscape(redirectPath), http.StatusTemporaryRedirect)
}

// fetchOAuthUserInfo retrieves the user's email, display name, and avatar URL from the OAuth provider.
func fetchOAuthUserInfo(ctx context.Context, provider string, cfg *oauth2.Config, token *oauth2.Token) (email, name, avatarURL string, err error) {
	client := cfg.Client(ctx, token)

	switch provider {
	case "google":
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			return "", "", "", fmt.Errorf("google userinfo request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", "", "", fmt.Errorf("google userinfo API returned %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthResponseSize))
		if err != nil {
			return "", "", "", fmt.Errorf("reading google response: %w", err)
		}
		var info struct {
			Email   string `json:"email"`
			Name    string `json:"name"`
			Picture string `json:"picture"`
		}
		if err := json.Unmarshal(body, &info); err != nil {
			return "", "", "", fmt.Errorf("parsing google userinfo: %w", err)
		}
		return strings.ToLower(strings.TrimSpace(info.Email)), info.Name, info.Picture, nil

	case "github":
		// Fetch user profile.
		resp, err := client.Get("https://api.github.com/user")
		if err != nil {
			return "", "", "", fmt.Errorf("github user request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", "", "", fmt.Errorf("github user API returned %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthResponseSize))
		if err != nil {
			return "", "", "", fmt.Errorf("reading github response: %w", err)
		}
		var ghUser struct {
			Email     string `json:"email"`
			Name      string `json:"name"`
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		}
		if err := json.Unmarshal(body, &ghUser); err != nil {
			return "", "", "", fmt.Errorf("parsing github user: %w", err)
		}

		ghEmail := strings.ToLower(strings.TrimSpace(ghUser.Email))
		displayName := ghUser.Name
		if displayName == "" {
			displayName = ghUser.Login
		}

		// If email is empty (private), fetch from /user/emails endpoint.
		if ghEmail == "" {
			emailResp, emailErr := client.Get("https://api.github.com/user/emails")
			if emailErr != nil {
				return "", "", "", fmt.Errorf("github emails request failed: %w", emailErr)
			}
			defer emailResp.Body.Close()
			if emailResp.StatusCode != http.StatusOK {
				return "", "", "", fmt.Errorf("github emails API returned %d", emailResp.StatusCode)
			}
			emailBody, emailErr := io.ReadAll(io.LimitReader(emailResp.Body, maxOAuthResponseSize))
			if emailErr != nil {
				return "", "", "", fmt.Errorf("reading github emails response: %w", emailErr)
			}
			var emails []struct {
				Email    string `json:"email"`
				Primary  bool   `json:"primary"`
				Verified bool   `json:"verified"`
			}
			if err := json.Unmarshal(emailBody, &emails); err != nil {
				return "", "", "", fmt.Errorf("parsing github emails: %w", err)
			}
			// Pick the primary verified email.
			for _, e := range emails {
				if e.Primary && e.Verified {
					ghEmail = strings.ToLower(strings.TrimSpace(e.Email))
					break
				}
			}
			// Fallback: any verified email.
			if ghEmail == "" {
				for _, e := range emails {
					if e.Verified {
						ghEmail = strings.ToLower(strings.TrimSpace(e.Email))
						break
					}
				}
			}
		}

		return ghEmail, displayName, ghUser.AvatarURL, nil

	default:
		return "", "", "", fmt.Errorf("unsupported provider: %s", provider)
	}
}
