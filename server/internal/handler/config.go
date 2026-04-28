package handler

import (
	"net/http"
	"os"
	"strings"
)

type AppConfig struct {
	CdnDomain          string `json:"cdn_domain"`
	DingTalkClientID   string `json:"dingtalk_client_id"`
	DingTalkOAuthScope string `json:"dingtalk_oauth_scope"`
	HideEmailLogin     bool   `json:"hide_email_login"`

	// PostHog public config for the frontend. The key is the same Project
	// API Key the backend uses; returning it here (instead of baking it
	// into the frontend bundle via NEXT_PUBLIC_*) means self-hosted
	// instances — whose server returns an empty key — automatically
	// disable frontend event shipping too.
	PosthogKey  string `json:"posthog_key"`
	PosthogHost string `json:"posthog_host"`
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{
		DingTalkClientID:   strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_ID")),
		DingTalkOAuthScope: strings.TrimSpace(os.Getenv("DINGTALK_OAUTH_SCOPE")),
		HideEmailLogin:     os.Getenv("NEXT_PUBLIC_HIDE_EMAIL_LOGIN") == "true",
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
