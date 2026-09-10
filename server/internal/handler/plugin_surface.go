package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const (
	pluginSurfaceLaunchTTL       = 2 * time.Minute
	pluginSurfaceProtocolVersion = 2
	pluginSurfaceConnectMessage  = "multica:plugin-bridge-connect"
	pluginSurfacePortGlobal      = "__multicaPluginBridgePortV2"
)

var errInvalidPluginSurfaceOrigin = errors.New("plugin surface origin must be an absolute HTTP(S) origin without a path")

type pluginSurfaceLaunchClaims struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	VersionID      string `json:"version_id"`
	SurfaceKey     string `json:"surface_key"`
	Digest         string `json:"digest"`
	Challenge      string `json:"challenge"`
	ExpiresAt      int64  `json:"expires_at"`
}

type pluginSurfaceLaunch struct {
	URL         string `json:"url"`
	BridgeToken string `json:"bridge_token"`
	Version     string `json:"version"`
	Digest      string `json:"digest"`
}

// NewPluginSurfaceTokenBox derives a separate AES-GCM key from the plugin
// deployment key. A surface URL can never be opened as a stored config secret,
// even though operators rotate both with one deployment setting.
func NewPluginSurfaceTokenBox(deploymentKey []byte) (*secretbox.Box, error) {
	if len(deploymentKey) != secretbox.KeySize {
		return nil, secretbox.ErrInvalidKey
	}
	mac := hmac.New(sha256.New, deploymentKey)
	_, _ = mac.Write([]byte("multica/plugin-surface-launch/v1"))
	return secretbox.New(mac.Sum(nil))
}

func parsePluginSurfaceOrigin(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errInvalidPluginSurfaceOrigin
	}
	parsed.Path = ""
	return parsed, nil
}

func (h *Handler) pluginSurfaceOriginIsDedicated(origin *url.URL) bool {
	candidates := make([]string, 0, len(h.cfg.AttachmentFrameAncestors)+2)
	candidates = append(candidates, h.cfg.PublicURL)
	candidates = append(candidates, h.cfg.AppURL)
	candidates = append(candidates, h.cfg.AttachmentFrameAncestors...)
	for _, candidate := range candidates {
		appOrigin, err := parsePluginSurfaceOrigin(candidate)
		if err == nil && strings.EqualFold(appOrigin.Host, origin.Host) {
			return false
		}
	}
	return true
}

// PluginSurfaceHostBoundary turns the configured content host into a one-route
// origin. Reusing the API process is operationally convenient; exposing its
// login, JSON API, uploads or redirects under the plugin host is not part of
// that bargain.
func (h *Handler) PluginSurfaceHostBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin, err := parsePluginSurfaceOrigin(h.cfg.PluginSurfaceOrigin)
		if err == nil && h.pluginSurfaceOriginIsDedicated(origin) && strings.EqualFold(r.Host, origin.Host) &&
			!strings.HasPrefix(r.URL.Path, "/plugin-surfaces/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randomPluginSurfaceChallenge() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (h *Handler) mintPluginSurfaceToken(claims pluginSurfaceLaunchClaims) (string, error) {
	if h.PluginSurfaceTokens == nil {
		return "", errors.New("plugin surface token encryption is not configured")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal plugin surface launch: %w", err)
	}
	sealed, err := h.PluginSurfaceTokens.Seal(payload)
	if err != nil {
		return "", fmt.Errorf("seal plugin surface launch: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (h *Handler) openPluginSurfaceToken(token string) (pluginSurfaceLaunchClaims, error) {
	if h.PluginSurfaceTokens == nil {
		return pluginSurfaceLaunchClaims{}, errors.New("plugin surface token encryption is not configured")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return pluginSurfaceLaunchClaims{}, errors.New("invalid plugin surface token encoding")
	}
	payload, err := h.PluginSurfaceTokens.Open(sealed)
	if err != nil {
		return pluginSurfaceLaunchClaims{}, errors.New("invalid plugin surface token")
	}
	var claims pluginSurfaceLaunchClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return pluginSurfaceLaunchClaims{}, errors.New("invalid plugin surface claims")
	}
	if claims.WorkspaceID == "" || claims.InstallationID == "" || claims.VersionID == "" ||
		claims.SurfaceKey == "" || claims.Digest == "" || claims.Challenge == "" ||
		claims.ExpiresAt <= time.Now().Unix() {
		return pluginSurfaceLaunchClaims{}, errors.New("expired or incomplete plugin surface claims")
	}
	return claims, nil
}

// GetPluginSurfaceLaunch returns no executable bytes. It mints one short-lived
// hosted URL plus the proof that exact document must present to the exact frame
// which requested it.
func (h *Handler) GetPluginSurfaceLaunch(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	origin, err := parsePluginSurfaceOrigin(h.cfg.PluginSurfaceOrigin)
	if err != nil || h.PluginSurfaceTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "Plugin surfaces are unavailable: MULTICA_PLUGIN_SURFACE_ORIGIN and MULTICA_PLUGIN_SECRET_KEY must be configured")
		return
	}
	if !h.pluginSurfaceOriginIsDedicated(origin) {
		writeError(w, http.StatusServiceUnavailable, "Plugin surfaces require a dedicated content origin separate from the app and API origins")
		return
	}

	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace_id")
	if !ok {
		return
	}
	installation, err := h.PluginService.InstallationForWorkspace(r.Context(), workspaceID, chi.URLParam(r, "installationId"))
	if err != nil {
		writePluginError(w, err, "failed to load the Plugin")
		return
	}
	if !installation.Enabled {
		writeError(w, http.StatusForbidden, "this Plugin is disabled")
		return
	}
	script, err := h.PluginService.SurfaceScript(r.Context(), installation, chi.URLParam(r, "surfaceKey"))
	if err != nil {
		writePluginError(w, err, "failed to load the Plugin surface")
		return
	}
	challenge, err := randomPluginSurfaceChallenge()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the Plugin surface launch")
		return
	}
	claims := pluginSurfaceLaunchClaims{
		WorkspaceID:    util.UUIDToString(workspaceID),
		InstallationID: util.UUIDToString(installation.ID),
		VersionID:      util.UUIDToString(installation.PackageVersionID),
		SurfaceKey:     chi.URLParam(r, "surfaceKey"),
		Digest:         script.Digest,
		Challenge:      challenge,
		ExpiresAt:      time.Now().Add(pluginSurfaceLaunchTTL).Unix(),
	}
	token, err := h.mintPluginSurfaceToken(claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the Plugin surface launch")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, pluginSurfaceLaunch{
		URL:         origin.String() + "/plugin-surfaces/" + token,
		BridgeToken: challenge,
		Version:     script.Version,
		Digest:      script.Digest,
	})
}

// ServePluginSurface renders stored plugin code only on the configured content
// host. The path credential is encrypted, authenticated, short-lived and bound
// to the installation's immutable version, so this route needs no session and
// receives no app cookie.
func (h *Handler) ServePluginSurface(w http.ResponseWriter, r *http.Request) {
	origin, err := parsePluginSurfaceOrigin(h.cfg.PluginSurfaceOrigin)
	if err != nil || !h.pluginSurfaceOriginIsDedicated(origin) || !strings.EqualFold(r.Host, origin.Host) {
		http.NotFound(w, r)
		return
	}
	// Enforce the deployment promise at the request boundary. A broad parent-
	// domain auth cookie must make the surface fail visibly, not quietly arrive
	// on a host that was supposed to be cookie-free.
	if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
		writeError(w, http.StatusBadRequest, "the Plugin content origin must not receive app credentials")
		return
	}
	claims, err := h.openPluginSurfaceToken(chi.URLParam(r, "token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	workspaceID, err := util.ParseUUID(claims.WorkspaceID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	installation, err := h.PluginService.InstallationForWorkspace(r.Context(), workspaceID, claims.InstallationID)
	if err != nil || !installation.Enabled || util.UUIDToString(installation.PackageVersionID) != claims.VersionID {
		http.NotFound(w, r)
		return
	}
	script, err := h.PluginService.SurfaceScript(r.Context(), installation, claims.SurfaceKey)
	if err != nil || !hmac.Equal([]byte(script.Digest), []byte(claims.Digest)) {
		http.NotFound(w, r)
		return
	}
	var scopes []string
	if err := json.Unmarshal(installation.GrantedScopes, &scopes); err != nil {
		http.NotFound(w, r)
		return
	}

	document := buildPluginSurfaceDocument(script.Code, claims.Challenge)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Content-Security-Policy", pluginSurfaceCSP(scopes))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(document))
}

func pluginSurfaceCSP(scopes []string) string {
	connect := make([]string, 0)
	for _, domain := range plugincontract.NetDomains(scopes) {
		// Installation scopes are validated when written, but the CSP remains a
		// boundary if a row is corrupted or restored from an older database.
		if plugincontract.ValidateScope(plugincontract.ScopeNetPrefix+domain) != nil {
			continue
		}
		connect = append(connect, "https://"+domain)
	}
	connectSource := "'none'"
	if len(connect) > 0 {
		connectSource = strings.Join(connect, " ")
	}
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"style-src 'unsafe-inline'",
		"img-src data: blob:",
		"font-src data:",
		"connect-src " + connectSource,
		"frame-src 'none'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	}, "; ")
}

func buildPluginSurfaceDocument(code, challenge string) string {
	encodedCode := base64.StdEncoding.EncodeToString([]byte(code))
	bootstrap := fmt.Sprintf(`(function () {
  var challenge = %s;
  var codeElement = document.getElementById("multica-surface-code");
  var bootstrapElement = document.currentScript;
  var failed = false;
  function reportSurfaceError() {
    if (failed) return;
    failed = true;
    parent.postMessage({ type: "multica:plugin-surface-error" }, "*");
  }
  window.addEventListener("error", reportSurfaceError);
  window.addEventListener("unhandledrejection", reportSurfaceError);
  try {
    var channel = new MessageChannel();
    Object.defineProperty(globalThis, %s, {
      value: channel.port2,
      configurable: true,
      enumerable: false,
      writable: false
    });
    window.addEventListener("pagehide", function () {
      parent.postMessage({ type: "multica:plugin-surface-navigated" }, "*");
    });
    parent.postMessage({
      type: %s,
      version: %d,
      challenge: challenge
    }, "*", [channel.port1]);
    challenge = "";

    var binary = atob(codeElement.textContent || "");
    var bytes = new Uint8Array(binary.length);
    for (var index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
    codeElement.remove();
    bootstrapElement.remove();
    var plugin = document.createElement("script");
    plugin.textContent = new TextDecoder().decode(bytes);
    document.body.appendChild(plugin);
  } catch (error) {
    reportSurfaceError();
  }
})();`, strconv.Quote(challenge), strconv.Quote(pluginSurfacePortGlobal), strconv.Quote(pluginSurfaceConnectMessage), pluginSurfaceProtocolVersion)

	return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  background: var(--background, transparent);
  color: var(--foreground, inherit);
  font: 400 var(--text-body, 14px)/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
</style>
</head>
<body>
<div id="root"></div>
<script type="text/plain" id="multica-surface-code">` + html.EscapeString(encodedCode) + `</script>
<script>` + bootstrap + `</script>
</body>
</html>`
}
