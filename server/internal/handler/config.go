package handler

import (
	"net/http"
	"os"

	"github.com/multica-ai/multica/server/internal/auth"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup or wire OAuth.
	AllowSignup    bool                                      `json:"allow_signup"`
	OAuthProviders map[string]auth.OAuthProviderPublicConfig `json:"oauth_providers,omitempty"`

	// PostHog public config for the frontend. The key is the same Project
	// API Key the backend uses; returning it here (instead of baking it
	// into the frontend bundle via NEXT_PUBLIC_*) means self-hosted
	// instances — whose server returns an empty key — automatically
	// disable frontend event shipping too.
	PosthogKey  string `json:"posthog_key"`
	PosthogHost string `json:"posthog_host"`
}

// GetConfig is mounted on the public (unauthenticated) route group because
// the web app calls it before login to decide whether to render the OAuth
// sign-in buttons and signup UI. Only add fields here that are safe to
// expose to anonymous callers — never user- or tenant-scoped data.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{
		AllowSignup: os.Getenv("ALLOW_SIGNUP") != "false",
	}
	for id, p := range h.OAuthProviders {
		if !p.Configured() {
			continue
		}
		pc := p.PublicConfig()
		if config.OAuthProviders == nil {
			config.OAuthProviders = map[string]auth.OAuthProviderPublicConfig{}
		}
		config.OAuthProviders[id] = pc
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}

	// Re-read from env on every request so operators can rotate keys via
	// secret refresh without a server restart.
	if v := os.Getenv("ANALYTICS_DISABLED"); v != "true" && v != "1" {
		config.PosthogKey = os.Getenv("POSTHOG_API_KEY")
		config.PosthogHost = os.Getenv("POSTHOG_HOST")
		if config.PosthogHost == "" && config.PosthogKey != "" {
			config.PosthogHost = "https://us.i.posthog.com"
		}
	}

	writeJSON(w, http.StatusOK, config)
}
