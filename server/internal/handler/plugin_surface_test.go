package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pluginSurfaceTokenHandler(t *testing.T) *Handler {
	t.Helper()
	box, err := NewPluginSurfaceTokenBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("NewPluginSurfaceTokenBox: %v", err)
	}
	return &Handler{PluginSurfaceTokens: box}
}

func validSurfaceClaims() pluginSurfaceLaunchClaims {
	return pluginSurfaceLaunchClaims{
		WorkspaceID:    "01900000-0000-7000-8000-000000000001",
		InstallationID: "01900000-0000-7000-8000-000000000002",
		VersionID:      "01900000-0000-7000-8000-000000000003",
		SurfaceKey:     "panel",
		Digest:         "sha256",
		Challenge:      "bridge-proof-must-stay-opaque",
		ExpiresAt:      time.Now().Add(time.Minute).Unix(),
	}
}

func TestPluginSurfaceTokenIsOpaqueAuthenticatedAndExpiring(t *testing.T) {
	h := pluginSurfaceTokenHandler(t)
	claims := validSurfaceClaims()
	token, err := h.mintPluginSurfaceToken(claims)
	if err != nil {
		t.Fatalf("mintPluginSurfaceToken: %v", err)
	}
	if strings.Contains(token, claims.Challenge) || strings.Contains(token, claims.InstallationID) {
		t.Fatalf("launch token exposed plaintext claims: %q", token)
	}
	opened, err := h.openPluginSurfaceToken(token)
	if err != nil {
		t.Fatalf("openPluginSurfaceToken: %v", err)
	}
	if opened != claims {
		t.Fatalf("opened claims = %+v, want %+v", opened, claims)
	}

	replacement := byte('A')
	if token[10] == replacement {
		replacement = 'B'
	}
	tampered := token[:10] + string(replacement) + token[11:]
	if _, err := h.openPluginSurfaceToken(tampered); err == nil {
		t.Fatal("tampered launch token was accepted")
	}
	claims.ExpiresAt = time.Now().Add(-time.Second).Unix()
	expired, err := h.mintPluginSurfaceToken(claims)
	if err != nil {
		t.Fatalf("mint expired token: %v", err)
	}
	if _, err := h.openPluginSurfaceToken(expired); err == nil {
		t.Fatal("expired launch token was accepted")
	}
}

func TestPluginSurfaceTokenKeyFailsClosed(t *testing.T) {
	if _, err := NewPluginSurfaceTokenBox(nil); err == nil {
		t.Fatal("empty deployment key enabled surface tokens")
	}
}

func TestPluginSurfaceOriginMustBeOnlyAnHTTPOrigin(t *testing.T) {
	for _, value := range []string{"", "plugin.example.test", "javascript:alert(1)", "https://plugin.example.test/path", "https://plugin.example.test?x=1"} {
		if _, err := parsePluginSurfaceOrigin(value); err == nil {
			t.Fatalf("parsePluginSurfaceOrigin(%q) succeeded", value)
		}
	}
	parsed, err := parsePluginSurfaceOrigin("https://plugin.example.test/")
	if err != nil || parsed.String() != "https://plugin.example.test" {
		t.Fatalf("valid origin = %v, %v", parsed, err)
	}
}

func TestPluginSurfaceOriginMustDifferFromEveryAppOrigin(t *testing.T) {
	h := &Handler{cfg: Config{
		PublicURL:                "https://api.example.test",
		AppURL:                   "https://canonical-app.example.test",
		AttachmentFrameAncestors: []string{"https://app.example.test"},
	}}
	for _, value := range []string{"https://api.example.test", "https://canonical-app.example.test", "https://app.example.test"} {
		origin, err := parsePluginSurfaceOrigin(value)
		if err != nil {
			t.Fatal(err)
		}
		if h.pluginSurfaceOriginIsDedicated(origin) {
			t.Fatalf("%s was accepted as a dedicated content origin", value)
		}
	}
	content, _ := parsePluginSurfaceOrigin("https://plugin-content.example.test")
	if !h.pluginSurfaceOriginIsDedicated(content) {
		t.Fatal("separate content origin was rejected")
	}
}

func TestServePluginSurfaceRejectsConfiguredAppOriginBeforeOpeningToken(t *testing.T) {
	h := pluginSurfaceTokenHandler(t)
	h.cfg.AppURL = "https://app.example.test"
	h.cfg.PluginSurfaceOrigin = "https://app.example.test"
	token, err := h.mintPluginSurfaceToken(validSurfaceClaims())
	if err != nil {
		t.Fatalf("mintPluginSurfaceToken: %v", err)
	}

	request := pluginHandlerRequest(http.MethodGet, "/plugin-surfaces/"+token, nil, map[string]string{"token": token})
	request.Host = "app.example.test"
	recorder := httptest.NewRecorder()
	h.ServePluginSurface(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("shared app/content origin: status=%d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPluginSurfaceHostExposesOnlySurfaceDocuments(t *testing.T) {
	h := &Handler{cfg: Config{PluginSurfaceOrigin: "https://plugin-content.example.test"}}
	called := false
	boundary := h.PluginSurfaceHostBoundary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	blocked := httptest.NewRequest(http.MethodGet, "https://plugin-content.example.test/api/config", nil)
	blocked.Host = "plugin-content.example.test"
	blockedRecorder := httptest.NewRecorder()
	boundary.ServeHTTP(blockedRecorder, blocked)
	if blockedRecorder.Code != http.StatusNotFound || called {
		t.Fatalf("content host API request: status=%d called=%v", blockedRecorder.Code, called)
	}

	allowed := httptest.NewRequest(http.MethodGet, "https://plugin-content.example.test/plugin-surfaces/token", nil)
	allowed.Host = "plugin-content.example.test"
	allowedRecorder := httptest.NewRecorder()
	boundary.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusNoContent || !called {
		t.Fatalf("content document request: status=%d called=%v", allowedRecorder.Code, called)
	}
}

func TestPluginSurfaceCSPComesOnlyFromGrantedNetScopes(t *testing.T) {
	csp := pluginSurfaceCSP([]string{"issues:read", "net:api.example.test", "storage:user", "net:evil.test; script-src *"})
	for _, directive := range []string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"connect-src https://api.example.test",
		"frame-src 'none'",
		"form-action 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Fatalf("CSP missing %q: %s", directive, csp)
		}
	}
	if strings.Contains(csp, "issues:read") || strings.Contains(csp, "connect-src https:;") {
		t.Fatalf("CSP widened beyond net grants: %s", csp)
	}
	if strings.Contains(csp, "evil.test") || strings.Contains(csp, "script-src *") {
		t.Fatalf("invalid stored net scope escaped into CSP: %s", csp)
	}
	if noNetwork := pluginSurfaceCSP([]string{"issues:read"}); !strings.Contains(noNetwork, "connect-src 'none'") {
		t.Fatalf("no-network CSP = %s", noNetwork)
	}
}

func TestPluginSurfaceDocumentHidesBootstrapProofBeforePluginCode(t *testing.T) {
	code := `const html = "</script><script>alert(1)</script>";`
	document := buildPluginSurfaceDocument(code, "bridge-proof")
	if strings.Contains(document, "alert(1)") || strings.Contains(document, `"</script>`) {
		t.Fatal("plugin source escaped its inert base64 element")
	}
	for _, required := range []string{
		"new MessageChannel()",
		pluginSurfacePortGlobal,
		pluginSurfaceConnectMessage,
		"challenge = \"\"",
		`window.addEventListener("error", reportSurfaceError)`,
		`window.addEventListener("unhandledrejection", reportSurfaceError)`,
		"codeElement.remove()",
		"bootstrapElement.remove()",
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("surface document missing %q", required)
		}
	}
	if strings.Index(document, "bootstrapElement.remove()") > strings.Index(document, "document.body.appendChild(plugin)") {
		t.Fatal("plugin code runs before its launch proof is removed from the DOM")
	}
	if strings.Index(document, `window.addEventListener("error", reportSurfaceError)`) > strings.Index(document, "document.body.appendChild(plugin)") {
		t.Fatal("plugin code runs before browser error reporting is armed")
	}
}
