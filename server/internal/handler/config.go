package handler

import (
	"net/http"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/analytics"
	enterpriseLark "github.com/multica-ai/multica/server/internal/enterprise/lark"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup or wire Google OAuth.
	AllowSignup       bool   `json:"allow_signup"`
	GoogleClientID    string `json:"google_client_id,omitempty"`
	LarkAuthEnabled   bool   `json:"lark_auth_enabled"`
	LarkAppID         string `json:"lark_app_id,omitempty"`
	LarkAuthorizeURL  string `json:"lark_authorize_url,omitempty"`
	ReleaseRepository string `json:"release_repository,omitempty"`

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
	larkEnabled, larkAppID, larkAuthorizeURL := enterpriseLark.PublicConfigFromEnv()
	config := AppConfig{
		AllowSignup:       os.Getenv("ALLOW_SIGNUP") != "false",
		GoogleClientID:    os.Getenv("GOOGLE_CLIENT_ID"),
		LarkAuthEnabled:   larkEnabled && larkAppID != "",
		LarkAppID:         larkAppID,
		LarkAuthorizeURL:  larkAuthorizeURL,
		ReleaseRepository: publicReleaseRepository(),
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
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

func publicReleaseRepository() string {
	value := strings.TrimSpace(os.Getenv("MULTICA_RELEASE_REPOSITORY"))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
	}
	value = strings.TrimPrefix(value, "https://github.com/")
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return value
}
