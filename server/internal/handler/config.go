package handler

import (
	"net/http"
	"os"
	"strings"
)

type AppConfig struct {
	CdnDomain          string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup or wire Google OAuth.
	AllowSignup        bool   `json:"allow_signup"`
	GoogleClientID     string `json:"google_client_id,omitempty"`
	GoogleIOSClientID  string `json:"google_ios_client_id"`
	DingTalkClientID   string `json:"dingtalk_client_id"`
	DingTalkOAuthScope string `json:"dingtalk_oauth_scope"`
	HideEmailLogin     bool   `json:"hide_email_login"`

	// PostHog public config for the frontend.
	PosthogKey  string `json:"posthog_key"`
	PosthogHost string `json:"posthog_host"`
}

// GetConfig is mounted on the public (unauthenticated) route group because
// the web app calls it before login to decide whether to render the Google
// sign-in button and signup UI. Only add fields here that are safe to expose
// to anonymous callers — never user- or tenant-scoped data.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{
		GoogleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		GoogleIOSClientID:  strings.TrimSpace(os.Getenv("GOOGLE_IOS_CLIENT_ID")),
		DingTalkClientID:   strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_ID")),
		DingTalkOAuthScope: strings.TrimSpace(os.Getenv("DINGTALK_OAUTH_SCOPE")),
		HideEmailLogin:     os.Getenv("NEXT_PUBLIC_HIDE_EMAIL_LOGIN") == "true",
		AllowSignup:        os.Getenv("ALLOW_SIGNUP") != "false",
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
