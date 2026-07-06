package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func TestGetConfigReportsCdnSignedMode(t *testing.T) {
	origStorage := testHandler.Storage
	origSigner := testHandler.CFSigner
	testHandler.Storage = &mockStorage{}
	defer func() {
		testHandler.Storage = origStorage
		testHandler.CFSigner = origSigner
	}()

	fetch := func() AppConfig {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	testHandler.CFSigner = nil
	if cfg := fetch(); cfg.CdnSigned {
		t.Fatalf("cdn_signed: want false without a CloudFront signer, got true")
	}

	// With signing enabled the same cdn_domain serves private content via
	// signed URLs only — clients must be told raw storage URLs won't load.
	testHandler.CFSigner = &auth.CloudFrontSigner{}
	cfg := fetch()
	if !cfg.CdnSigned {
		t.Fatalf("cdn_signed: want true with a CloudFront signer, got false")
	}
	if cfg.CdnDomain != "cdn.example.com" {
		t.Fatalf("cdn_domain: want cdn.example.com alongside cdn_signed, got %q", cfg.CdnDomain)
	}
}

func TestGetConfigIncludesRuntimeAuthConfig(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Setenv("ALLOW_SIGNUP", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "google-client-id")
	t.Setenv("POSTHOG_API_KEY", "phc_test")
	t.Setenv("POSTHOG_HOST", "https://eu.i.posthog.com")
	t.Setenv("MULTICA_PUBLIC_URL", "https://api.example.com/")
	t.Setenv("MULTICA_APP_URL", "https://app.example.com/")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if cfg.CdnDomain != "cdn.example.com" {
		t.Fatalf("cdn_domain: want cdn.example.com, got %q", cfg.CdnDomain)
	}
	if cfg.AllowSignup {
		t.Fatalf("allow_signup: want false, got true")
	}
	if cfg.GoogleClientID != "google-client-id" {
		t.Fatalf("google_client_id: want google-client-id, got %q", cfg.GoogleClientID)
	}
	if cfg.PosthogKey != "phc_test" {
		t.Fatalf("posthog_key: want phc_test, got %q", cfg.PosthogKey)
	}
	if cfg.PosthogHost != "https://eu.i.posthog.com" {
		t.Fatalf("posthog_host: want https://eu.i.posthog.com, got %q", cfg.PosthogHost)
	}
	if cfg.AnalyticsEnvironment != "dev" {
		t.Fatalf("analytics_environment: want dev, got %q", cfg.AnalyticsEnvironment)
	}
	if cfg.WorkspaceCreationDisabled {
		t.Fatalf("workspace_creation_disabled: want false by default, got true")
	}
	if cfg.DaemonServerURL != "https://api.example.com" {
		t.Fatalf("daemon_server_url: want https://api.example.com, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "https://app.example.com" {
		t.Fatalf("daemon_app_url: want https://app.example.com, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigUsesAppURLForSameOriginDaemonSetup(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://multica.internal.example/")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "https://multica.internal.example" {
		t.Fatalf("daemon_server_url: want same-origin URL, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "https://multica.internal.example" {
		t.Fatalf("daemon_app_url: want app URL, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigUsesFrontendOriginForSameOriginDaemonSetup(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://multica.internal.example/")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "https://multica.internal.example" {
		t.Fatalf("daemon_server_url: want same-origin URL, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "https://multica.internal.example" {
		t.Fatalf("daemon_app_url: want frontend origin, got %q", cfg.DaemonAppURL)
	}
}

func TestGetConfigOmitsOfficialCloudDaemonSetup(t *testing.T) {
	t.Setenv("MULTICA_PUBLIC_URL", "https://api.multica.ai")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://multica.ai")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "" {
		t.Fatalf("daemon_server_url: want omitted for cloud, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "" {
		t.Fatalf("daemon_app_url: want omitted for cloud, got %q", cfg.DaemonAppURL)
	}
}

// TestGetConfigOmitsCloudDaemonSetupWithoutPublicURL reproduces the production
// regression behind the broken "Add a computer" command: the official cloud
// frontend is multica.ai, but the deployment does not set MULTICA_PUBLIC_URL to
// the api host. Previously this fell through to the same-origin branch and
// emitted daemon_server_url=https://multica.ai, which the dialog turned into
// `multica setup self-host --server-url https://multica.ai` — pointing the
// daemon's backend at the frontend (no /health, no WebSocket proxy). The
// official cloud must be recognised by its frontend host alone so the daemon
// setup URLs are omitted and the dialog falls back to `multica setup`.
func TestGetConfigOmitsCloudDaemonSetupWithoutPublicURL(t *testing.T) {
	t.Setenv("MULTICA_PUBLIC_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://multica.ai")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "" {
		t.Fatalf("daemon_server_url: want omitted for official cloud, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "" {
		t.Fatalf("daemon_app_url: want omitted for official cloud, got %q", cfg.DaemonAppURL)
	}
}

// TestGetConfigOmitsCloudDaemonSetupForAppSubdomain covers the app.multica.ai
// frontend variant of the official cloud.
func TestGetConfigOmitsCloudDaemonSetupForAppSubdomain(t *testing.T) {
	t.Setenv("MULTICA_PUBLIC_URL", "")
	t.Setenv("MULTICA_APP_URL", "https://app.multica.ai")
	t.Setenv("FRONTEND_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DaemonServerURL != "" {
		t.Fatalf("daemon_server_url: want omitted for official cloud, got %q", cfg.DaemonServerURL)
	}
	if cfg.DaemonAppURL != "" {
		t.Fatalf("daemon_app_url: want omitted for official cloud, got %q", cfg.DaemonAppURL)
	}
}

func TestURLHostEqualsCanonicalizesCommonHostForms(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "full URL", raw: "https://api.multica.ai", want: true},
		{name: "bare host", raw: "api.multica.ai", want: true},
		{name: "host port", raw: "api.multica.ai:8080", want: true},
		{name: "trailing dot", raw: "https://api.multica.ai.", want: true},
		{name: "different host", raw: "https://evil.example", want: false},
		{name: "empty", raw: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := urlHostEquals(tt.raw, "api.multica.ai"); got != tt.want {
				t.Fatalf("urlHostEquals(%q): want %v, got %v", tt.raw, tt.want, got)
			}
		})
	}
}

// TestGetConfigExposesWorkspaceCreationDisabled verifies that the self-host
// gate added by #3433 surfaces to the frontend through /api/config so the UI
// can hide every "Create workspace" affordance.
func TestGetConfigExposesWorkspaceCreationDisabled(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Setenv("DISABLE_WORKSPACE_CREATION", "true")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !cfg.WorkspaceCreationDisabled {
		t.Fatalf("workspace_creation_disabled: want true with env on, got false (body=%s)", w.Body.String())
	}
}

func TestGetConfigExposesFrontendFeatureFlags(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig default flags: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode default config: %v", err)
	}
	if cfg.FeatureFlags["composio_mcp_apps"] {
		t.Fatalf("composio_mcp_apps: want false by default, got true")
	}

	withComposioMCPAppsFlag(t, h, true)
	w = httptest.NewRecorder()
	h.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig enabled flags: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode enabled config: %v", err)
	}
	if !cfg.FeatureFlags["composio_mcp_apps"] {
		t.Fatalf("composio_mcp_apps: want true with flag enabled, got false")
	}
}
