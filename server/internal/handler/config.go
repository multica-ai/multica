package handler

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/analytics"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup or wire Google OAuth.
	AllowSignup    bool   `json:"allow_signup"`
	GoogleClientID string `json:"google_client_id,omitempty"`
	// WorkspaceCreationDisabled mirrors the server-side
	// DISABLE_WORKSPACE_CREATION env var so the UI can hide every
	// "Create workspace" affordance on self-hosted instances. Omitted
	// from the JSON when false to keep responses identical to the
	// previous shape for the common managed-cloud case (#3433).
	WorkspaceCreationDisabled bool `json:"workspace_creation_disabled,omitempty"`
	// Public daemon setup config consumed by the web app at runtime so
	// self-hosted instances can show `multica setup self-host` commands
	// with the operator's own domains instead of Multica Cloud defaults.
	DaemonServerURL string `json:"daemon_server_url,omitempty"`
	DaemonAppURL    string `json:"daemon_app_url,omitempty"`

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
		AllowSignup:               os.Getenv("ALLOW_SIGNUP") != "false",
		GoogleClientID:            os.Getenv("GOOGLE_CLIENT_ID"),
		WorkspaceCreationDisabled: os.Getenv("DISABLE_WORKSPACE_CREATION") == "true",
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}
	config.DaemonServerURL, config.DaemonAppURL = daemonSetupURLsFromEnv()

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

func daemonSetupURLsFromEnv() (string, string) {
	serverURL := normalizePublicURL(os.Getenv("MULTICA_PUBLIC_URL"))
	appURL := normalizePublicURL(os.Getenv("MULTICA_APP_URL"))
	if appURL == "" {
		appURL = normalizePublicURL(os.Getenv("FRONTEND_ORIGIN"))
	}
	if appURL == "" {
		return "", ""
	}

	if serverURL == "" {
		serverURL = appURL
	}
	if isOfficialCloudDaemonConfig(serverURL, appURL) {
		return "", ""
	}
	return serverURL, appURL
}

func normalizePublicURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func isOfficialCloudDaemonConfig(serverURL, appURL string) bool {
	if !urlHostEquals(serverURL, "api.multica.ai") {
		return false
	}
	return urlHostEquals(appURL, "multica.ai") || urlHostEquals(appURL, "app.multica.ai")
}

func urlHostEquals(raw, want string) bool {
	host := canonicalURLHost(raw)
	if host == "" {
		return false
	}
	want = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(want)), ".")
	return host == want
}

func canonicalURLHost(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" && !strings.Contains(raw, "://") {
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return ""
		}
		host = u.Hostname()
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}
