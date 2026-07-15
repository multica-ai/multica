package handler

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/featureflags"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// CdnSigned tells clients that the CDN domain above serves PRIVATE
	// content through time-bounded signed URLs (CloudFront signing is
	// enabled). When true, a raw storage URL on the CDN domain is NOT
	// publicly fetchable — renderers must not pick it as a native
	// <img>/<video> source and should fall back to the per-attachment
	// API endpoint or a freshly signed download_url instead (MUL-3254).
	// Omitted when false so older clients see the previous shape.
	CdnSigned bool `json:"cdn_signed,omitempty"`
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

	// FeatureFlags exposes only frontend-safe boolean decisions. Do not dump
	// raw rules here: /api/config is public and may be called anonymously.
	FeatureFlags map[string]bool `json:"feature_flags,omitempty"`

	// ServerVersion is the running API build version, so self-hosted
	// operators can confirm what's deployed and include it in bug reports.
	// Only emitted on self-hosted deployments — omitted on the managed cloud,
	// which is continuously deployed so its users can't act on the version —
	// and empty for dev builds that aren't stamped via -X main.version.
	ServerVersion string `json:"server_version,omitempty"`
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
	config.CdnSigned = h.CFSigner != nil
	config.DaemonServerURL, config.DaemonAppURL = daemonSetupURLsFromEnv()
	config.FeatureFlags = featureflags.EvaluateFrontendPublicFlags(r.Context(), h.FeatureFlags)
	// Only surface the build version on self-hosted deployments. The managed
	// cloud is continuously deployed and its users can't choose the build, so
	// the Help popover's version row would just be noise there (MUL-4108).
	if !isOfficialCloudDeployment() {
		config.ServerVersion = h.cfg.ServerVersion
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

func daemonSetupURLsFromEnv() (string, string) {
	serverURL := normalizePublicURL(os.Getenv("MULTICA_PUBLIC_URL"))
	appURL := resolveFrontendAppURL()
	if appURL == "" {
		return "", ""
	}

	if serverURL == "" {
		serverURL = appURL
	}
	if isOfficialCloudDaemonConfig(appURL) {
		return "", ""
	}
	return serverURL, appURL
}

// resolveFrontendAppURL returns the operator-configured frontend origin
// (MULTICA_APP_URL, falling back to FRONTEND_ORIGIN), normalized. Shared by
// the daemon-setup URLs and the managed-cloud detection so both read the same
// signal.
func resolveFrontendAppURL() string {
	appURL := normalizePublicURL(os.Getenv("MULTICA_APP_URL"))
	if appURL == "" {
		appURL = normalizePublicURL(os.Getenv("FRONTEND_ORIGIN"))
	}
	return appURL
}

func normalizePublicURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// isOfficialCloudDaemonConfig reports whether this deployment is the official
// Multica Cloud, identified by its frontend host alone (multica.ai). The
// daemon setup for the managed cloud is always
// `multica setup` (which hardcodes api.multica.ai), so the per-deployment URLs
// must be omitted from /api/config even when MULTICA_PUBLIC_URL is unset or
// misconfigured. Previously this also required serverURL==api.multica.ai, so a
// cloud deployment that forgot MULTICA_PUBLIC_URL fell through and emitted a
// `setup self-host --server-url https://multica.ai` command — pointing the
// daemon's backend at the frontend (no /health, no WebSocket proxy).
func isOfficialCloudDaemonConfig(appURL string) bool {
	return urlHostEquals(appURL, "multica.ai")
}

// isOfficialCloudDeployment reports whether this server is the official Multica
// Cloud, reusing the same frontend-host signal as the daemon setup (multica.ai).
// Managed-cloud-only behavior — such as suppressing the Help popover's
// server-version row, which only matters to self-hosted operators — is gated on
// this.
func isOfficialCloudDeployment() bool {
	return isOfficialCloudDaemonConfig(resolveFrontendAppURL())
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
