package handler

import (
	"net/http"
	"os"

	"github.com/multica-ai/multica/server/internal/analytics"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup or wire Google OAuth.
	AllowSignup    bool   `json:"allow_signup"`
	GoogleClientID string `json:"google_client_id,omitempty"`

	// AppEnv reflects the APP_ENV environment variable (production, staging,
	// development, etc.). The frontend uses it to decide login UI behaviour
	// — e.g. production shows SSO-only when Casdoor is enabled, while
	// non-production also keeps the email verification path.
	AppEnv string `json:"app_env,omitempty"`

	// Casdoor SSO config. Returned at runtime so the frontend can render the
	// "Sign in with SSO" button without requiring a rebuild when operators
	// enable/disable Casdoor.
	CasdoorEnabled  bool   `json:"casdoor_enabled"`
	CasdoorLoginUrl string `json:"casdoor_login_url,omitempty"`

	// ServerURL is the public HTTP(S) address of this backend. Returned so
	// the frontend can generate accurate CLI setup commands for self-hosted
	// deployments (e.g. http://localhost:8080) without baking URLs into the
	// build. Empty for builds where the operator has not set SERVER_URL.
	ServerURL string `json:"server_url,omitempty"`

	// PostHog public config for the frontend. The key is the same Project
	// API Key the backend uses; returning it here (instead of baking it
	// into the frontend bundle via NEXT_PUBLIC_*) means self-hosted
	// instances — whose server returns an empty key — automatically
	// disable frontend event shipping too.
	PosthogKey           string `json:"posthog_key"`
	PosthogHost          string `json:"posthog_host"`
	AnalyticsEnvironment string `json:"analytics_environment"`
}

// GetConfig is mounted on the public (unauthenticated) route group because
// the web app calls it before login to decide whether to render the Google
// sign-in button and signup UI. Only add fields here that are safe to expose
// to anonymous callers — never user- or tenant-scoped data.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{
		AllowSignup:     os.Getenv("ALLOW_SIGNUP") != "false",
		GoogleClientID:  os.Getenv("GOOGLE_CLIENT_ID"),
		AppEnv:          os.Getenv("APP_ENV"),
		CasdoorEnabled:  h.cfg.CasdoorEndpoint != "",
		CasdoorLoginUrl: os.Getenv("NEXT_PUBLIC_CASDOOR_LOGIN_URL"),
	}
	if config.CasdoorLoginUrl == "" && config.CasdoorEnabled {
		config.CasdoorLoginUrl = "/auth/casdoor/login"
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}

	// SERVER_URL is the canonical public backend address (e.g. https://api.multica.ai
	// or http://localhost:8080). REMOTE_API_URL is the Next.js proxy target and
	// works as a fallback for deployments where only the web tier has the explicit
	// backend address.
	config.ServerURL = os.Getenv("SERVER_URL")
	if config.ServerURL == "" {
		config.ServerURL = os.Getenv("REMOTE_API_URL")
	}

	// Re-read from env on every request so operators can rotate keys via
	// secret refresh without a server restart.
	if v := os.Getenv("ANALYTICS_DISABLED"); v != "true" && v != "1" {
		config.PosthogKey = os.Getenv("POSTHOG_API_KEY")
		config.PosthogHost = os.Getenv("POSTHOG_HOST")
		config.AnalyticsEnvironment = analytics.EnvironmentFromEnv()
		if config.PosthogHost == "" && config.PosthogKey != "" {
			config.PosthogHost = "https://us.i.posthog.com"
		}
	}

	writeJSON(w, http.StatusOK, config)
}
