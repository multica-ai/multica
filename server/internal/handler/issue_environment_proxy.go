package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/entitlement"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"golang.org/x/net/publicsuffix"
)

const (
	environmentPreviewCookie = "multica_env_session"
	environmentLaunchPath    = "/api/environment-proxy/{environmentId}/launch"
	environmentCallbackPath  = "/_multica_env_auth"
	environmentLaunchTTL     = time.Minute
	environmentSessionTTL    = 10 * time.Minute
	maxEnvironmentLaunches   = 4096
)

type environmentProxy struct {
	base       *url.URL
	authOrigin *url.URL
	suffix     string
	launchBox  *secretbox.Box
	sessionBox *secretbox.Box
	transport  *http.Transport
	proxy      *httputil.ReverseProxy
	limits     EnvironmentProxyLimits
	state      environmentProxyState
	// notifyTunnel wakes a daemon over its runtime socket. The rendezvous
	// secret must stay on that socket, never on the shared event bus.
	notifyTunnel func(runtimeID, kind string) bool
}

type environmentSessionClaims struct {
	UserID      string `json:"uid"`
	WorkspaceID string `json:"wid"`
	EnvID       string `json:"eid"`
	RuntimeID   string `json:"rid"`
	LocalKey    string `json:"key"`
	Port        int32  `json:"port"`
	IssuedAt    int64  `json:"iat"` // Nanoseconds, to order revocation and launch within a second.
	ExpiresAt   int64  `json:"exp"`
	Limit       *int   `json:"limit,omitempty"`
	Revision    int64  `json:"revision,omitempty"`
}

type environmentLaunchClaims struct {
	environmentSessionClaims
	Nonce string `json:"n"`
	Next  string `json:"next"`
}

type environmentTunnelContextKey struct{}

func newEnvironmentProxy(cfg Config) *environmentProxy {
	if strings.TrimSpace(cfg.EnvProxyBaseURL) == "" {
		return nil
	}
	base, err := parsePluginSurfaceOrigin(cfg.EnvProxyBaseURL)
	if err != nil || strings.Count(base.Host, "*") != 1 || base.ForceQuery || base.RawPath != "" {
		slog.Error("invalid MULTICA_ENV_PROXY_BASE_URL; environment proxy disabled")
		return nil
	}
	base.Host = strings.ToLower(base.Host)
	prefix, suffix, _ := strings.Cut(base.Hostname(), "*")
	suffix = strings.TrimPrefix(suffix, ".")
	_, suffixErr := publicsuffix.EffectiveTLDPlusOne(suffix)
	port, portErr := strconv.Atoi(base.Port())
	if (prefix != "" && prefix != "env-") || !strings.HasPrefix(base.Hostname(), prefix+"*.") ||
		len(suffix) > 253-len("env-")-32-1 || !util.ValidDNSHostname(suffix) ||
		(suffix != "localhost" && (suffixErr != nil || net.ParseIP(suffix) != nil)) ||
		(base.Port() != "" && (portErr != nil || port < 1 || port > 65535)) || strings.HasSuffix(base.Host, ":") {
		slog.Error("invalid MULTICA_ENV_PROXY_BASE_URL wildcard host; environment proxy disabled")
		return nil
	}
	if cfg.BackendReplicas > 1 {
		slog.Error("environment proxy disabled: in-memory tunnel rendezvous requires MULTICA_BACKEND_REPLICAS=1", "replicas", cfg.BackendReplicas)
		return nil
	}

	// The app origin owns the browser's host-only login cookie. In split
	// deployments the app forwards /api to the backend.
	main := cfg.AppURL
	if strings.TrimSpace(main) == "" {
		main = cfg.PublicURL
	}
	origin, err := parsePluginSurfaceOrigin(main)
	if err != nil || origin.Hostname() == "" || strings.Contains(origin.Host, "*") {
		slog.Error("invalid environment proxy authentication origin; environment proxy disabled")
		return nil
	}
	if base.Port() == "80" && base.Scheme == "http" || base.Port() == "443" && base.Scheme == "https" {
		base.Host = base.Hostname()
	}
	p := &environmentProxy{
		base:       base,
		authOrigin: origin,
		suffix:     suffix,
		limits:     cfg.EnvProxyLimits.withDefaults(),
	}
	candidates := append([]string{cfg.PublicURL, cfg.AppURL, cfg.PluginSurfaceOrigin}, cfg.AttachmentFrameAncestors...)
	for _, candidate := range candidates {
		if app, err := parsePluginSurfaceOrigin(candidate); err == nil {
			host := strings.ToLower(strings.TrimSuffix(app.Hostname(), "."))
			// Allow the localhost app and its wildcard preview host in local development.
			if host == "localhost" && suffix == "localhost" {
				continue
			}
			if host == suffix || strings.HasSuffix(host, "."+suffix) || strings.HasSuffix(suffix, "."+host) {
				slog.Error("environment proxy overlaps a platform origin; environment proxy disabled", "host", host)
				return nil
			}
		}
	}

	key := auth.JWTSecret()
	p.launchBox = environmentProxyBox(key, "multica/env-proxy-launch/v1")
	p.sessionBox = environmentProxyBox(key, "multica/env-proxy-session/v1")
	p.state.init()
	return p
}

func environmentProxyBox(secret []byte, scope string) *secretbox.Box {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(scope))
	box, _ := secretbox.New(mac.Sum(nil)) // SHA-256 always produces a valid AES-256 key.
	return box
}

func sealEnvironmentClaims(box *secretbox.Box, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	sealed, err := box.Seal(payload)
	return base64.RawURLEncoding.EncodeToString(sealed), err
}

func openEnvironmentClaims(box *secretbox.Box, token string, claims any) bool {
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return false
	}
	payload, err := box.Open(sealed)
	return err == nil && json.Unmarshal(payload, claims) == nil
}

func (p *environmentProxy) environmentHost(id string) string {
	label := "env-" + strings.ReplaceAll(id, "-", "")
	host := label + "." + p.suffix
	if port := p.base.Port(); port != "" {
		return net.JoinHostPort(host, port)
	}
	return host
}

func (p *environmentProxy) defaultPortSuffix() string {
	if p.base.Scheme == "https" {
		return ":443"
	}
	return ":80"
}

func environmentHostname(host string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func (p *environmentProxy) hostEnvironment(host string) (id string, owned bool) {
	name := environmentHostname(host)
	label, ok := strings.CutSuffix(name, "."+p.suffix)
	if !ok {
		return "", false
	}
	const prefix = "env-"
	if !strings.HasPrefix(label, prefix) || len(label) != len(prefix)+32 {
		return "", true
	}
	parsed, err := uuid.Parse(label[len(prefix):])
	if err != nil || !strings.EqualFold(strings.TrimSuffix(host, p.defaultPortSuffix()), p.environmentHost(parsed.String())) {
		return "", true
	}
	return parsed.String(), true
}

func (p *environmentProxy) resolveURL(row db.IssueEnvironment) string {
	source, err := url.Parse(row.Url)
	if err != nil || source.Hostname() == "" {
		return row.Url
	}
	hostname := strings.TrimSuffix(strings.ToLower(source.Hostname()), ".")
	// A non-loopback URL is already reachable without a relay.
	if hostname != "localhost" && !net.ParseIP(hostname).IsLoopback() {
		return row.Url
	}
	source.Scheme = p.base.Scheme
	source.Host = p.environmentHost(uuidToString(row.ID))
	source.User = nil
	if source.Path == "" {
		source.Path = "/"
	}
	return source.String()
}

// EnvironmentProxyHostBoundary keeps every host in the preview namespace
// out of the main application router.
func (h *Handler) EnvironmentProxyHostBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := h.envProxy; p != nil {
			if id, owned := p.hostEnvironment(r.Host); owned {
				if id == "" {
					writeError(w, http.StatusNotFound, "environment not found")
					return
				}
				h.serveEnvironmentProxy(w, r, id)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func environmentProxyStatus(row db.IssueEnvironment, now time.Time) int {
	if row.Kind != issueEnvironmentKindServer || !row.Port.Valid || row.Port.Int32 < 1 || row.Port.Int32 > 65535 {
		return http.StatusNotFound
	}
	if row.EndedAt.Valid {
		return http.StatusGone
	}
	switch issueEnvironmentState(row, now) {
	case "starting", "running":
		return 0
	case "unreachable":
		return http.StatusServiceUnavailable
	default:
		return http.StatusGone
	}
}

func environmentRuntimeSupportsTunnel(rt db.AgentRuntime, workspaceID pgtype.UUID) bool {
	return rt.Status == "online" && rt.WorkspaceID == workspaceID &&
		runtimeHasCapability(rt.Metadata, protocol.DaemonCapabilityEnvironmentTunnelV1)
}

func (h *Handler) environmentProxyGate(ctx context.Context, workspaceID pgtype.UUID) entitlement.Decision {
	if h.Entitlements == nil {
		return entitlement.Decision{Gate: entitlement.Gate{Action: entitlement.ActionOff}}
	}
	return h.Entitlements.Gate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateEnvironmentProxy)
}

func writeEnvironmentProxyLimit(w http.ResponseWriter, limit int, revision int64) {
	code, message := "environment_proxy_limit_reached", "workspace has reached its active remote environment limit"
	if limit == 0 {
		code, message = "environment_proxy_disabled", "remote environment preview is not available on the current plan"
	}
	writeJSON(w, http.StatusPaymentRequired, map[string]any{
		"code":            code,
		"error":           message,
		"limit":           limit,
		"policy_revision": revision,
	})
}

func (h *Handler) authorizeEnvironmentProxy(w http.ResponseWriter, r *http.Request, id string) (environmentSessionClaims, bool) {
	var claims environmentSessionClaims
	ctx := r.Context()
	issuedAt := time.Now()
	row, err := h.Queries.GetIssueEnvironmentByID(ctx, parseUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "environment not found")
		return claims, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load environment")
		return claims, false
	}

	wsID, userID := uuidToString(row.WorkspaceID), requestUserID(r)
	if r.Header.Get("X-Actor-Source") == "task_token" && r.Header.Get("X-Workspace-ID") != wsID {
		writeError(w, http.StatusForbidden, "task token is bound to a different workspace")
		return claims, false
	}
	if _, err := h.getWorkspaceMember(ctx, userID, wsID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return claims, false
	}
	if !h.requireIssueIDVisible(w, r, wsID, row.IssueID, http.StatusForbidden, "forbidden") {
		return claims, false
	}
	if status := environmentProxyStatus(row, time.Now()); status != 0 {
		writeError(w, status, http.StatusText(status))
		return claims, false
	}
	rt, err := h.getAgentRuntime(ctx, obsmetrics.RuntimeLookupSourceIssue, row.RuntimeID)
	if err != nil || rt.WorkspaceID != row.WorkspaceID || rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime unavailable")
		return claims, false
	}
	if !environmentRuntimeSupportsTunnel(rt, row.WorkspaceID) {
		writeErrorCode(w, http.StatusConflict, "environment_tunnel_unsupported", "runtime does not support environment tunnels")
		return claims, false
	}

	claims = environmentSessionClaims{
		UserID:      userID,
		WorkspaceID: wsID,
		EnvID:       id,
		RuntimeID:   uuidToString(row.RuntimeID),
		LocalKey:    row.LocalKey,
		Port:        row.Port.Int32,
		IssuedAt:    issuedAt.UnixNano(),
		ExpiresAt:   issuedAt.Add(environmentSessionTTL).Unix(),
	}
	if decision := h.environmentProxyGate(ctx, row.WorkspaceID); decision.Gate.Action == entitlement.ActionEnforce && decision.Gate.Limit != nil {
		claims.Limit, claims.Revision = decision.Gate.Limit, decision.PolicyRevision
	}
	return claims, true
}

func environmentNext(raw string) string {
	if len(raw) > 2048 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.ContainsAny(raw, "\\") || strings.IndexFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return "/"
	}
	next, err := url.ParseRequestURI(raw)
	if err != nil || strings.HasPrefix(next.Path, "//") || strings.Contains(next.Path, "\\") ||
		strings.IndexFunc(next.Path, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 ||
		next.Path == environmentCallbackPath {
		return "/"
	}
	return next.RequestURI()
}

func environmentNavigation(r *http.Request) bool {
	mode := r.Header.Get("Sec-Fetch-Mode")
	dest := r.Header.Get("Sec-Fetch-Dest")
	return r.Method == http.MethodGet && !websocket.IsWebSocketUpgrade(r) &&
		(mode == "navigate" || mode == "") && (dest == "document" || dest == "")
}

func environmentBearer(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func environmentLaunchURL(id, next string, bounced bool) *url.URL {
	query := url.Values{"next": {next}}
	if bounced {
		query.Set("bounced", "1")
	}
	return &url.URL{Path: strings.Replace(environmentLaunchPath, "{environmentId}", id, 1), RawQuery: query.Encode()}
}

func environmentPrivateResponse(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// environmentProxyAuthWriter normalizes errors from Auth. On first browser
// navigation it turns a 401 into a document on the main origin, allowing the
// next navigation to send the host-only SameSite=Strict cookie.
type environmentProxyAuthWriter struct {
	http.ResponseWriter
	target  string
	bounced bool
}

func (w *environmentProxyAuthWriter) WriteHeader(status int) {
	if status != http.StatusUnauthorized || w.target == "" {
		if strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
			w.Header().Set("Content-Type", "application/json")
		}
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.bounced = true
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.ResponseWriter.WriteHeader(http.StatusOK)
	fmt.Fprintf(w.ResponseWriter, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=%s"><a href="%s">Continue to preview</a>`, w.target, w.target)
}

func (w *environmentProxyAuthWriter) Write(body []byte) (int, error) {
	if w.bounced {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

// LaunchEnvironmentProxy exchanges the main site's authenticated session for
// a one-use ticket scoped to a single preview host.
func (h *Handler) LaunchEnvironmentProxy(w http.ResponseWriter, r *http.Request) {
	p := h.envProxy
	if p == nil {
		writeError(w, http.StatusNotFound, "environment proxy unavailable")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "environmentId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "environment not found")
		return
	}
	environmentPrivateResponse(w)
	next := environmentNext(r.URL.Query().Get("next"))
	authWriter := &environmentProxyAuthWriter{ResponseWriter: w}
	if !environmentBearer(r) && environmentNavigation(r) && r.URL.Query().Get("bounced") != "1" {
		authWriter.target = html.EscapeString(environmentLaunchURL(id.String(), next, true).String())
	}
	middleware.Auth(h.Queries, h.PATCache, h.CloudPAT, h.CFSigner)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		claims, ok := h.authorizeEnvironmentProxy(w, r, id.String())
		if !ok {
			return
		}
		launch := environmentLaunchClaims{
			environmentSessionClaims: claims,
			Nonce:                    uuid.NewString(),
			Next:                     next,
		}
		launch.ExpiresAt = time.Now().Add(environmentLaunchTTL).Unix()
		ticket, err := sealEnvironmentClaims(p.launchBox, launch)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue environment launch ticket")
			return
		}
		// Reserve the launch slot before taking a workspace lease: a full queue
		// must not leave an idle lease holding an entitlement slot.
		if !p.state.addLaunch(launch.Nonce, launch.ExpiresAt) {
			writeError(w, http.StatusTooManyRequests, "too many pending environment launches")
			return
		}
		if p.admitEnvironment(w, claims, false) == nil {
			p.state.consumeLaunch(launch)
			return
		}
		target := url.URL{
			Scheme:   p.base.Scheme,
			Host:     p.environmentHost(claims.EnvID),
			Path:     environmentCallbackPath,
			RawQuery: url.Values{"ticket": {ticket}}.Encode(),
		}
		http.Redirect(w, r, target.String(), http.StatusFound)
	})).ServeHTTP(authWriter, r)
}

func (p *environmentProxy) completeLaunch(w http.ResponseWriter, r *http.Request, id string) {
	environmentPrivateResponse(w)
	var launch environmentLaunchClaims
	if !environmentNavigation(r) || !openEnvironmentClaims(p.launchBox, r.URL.Query().Get("ticket"), &launch) ||
		launch.EnvID != id || !p.state.consumeLaunch(launch) {
		writeError(w, http.StatusUnauthorized, "invalid environment launch ticket")
		return
	}
	claims := launch.environmentSessionClaims
	claims.ExpiresAt = time.Now().Add(environmentSessionTTL).Unix()
	token, err := sealEnvironmentClaims(p.sessionBox, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue environment session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     environmentPreviewCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(environmentSessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   p.base.Scheme == "https",
	})
	http.Redirect(w, r, launch.Next, http.StatusFound)
}

func (h *Handler) serveEnvironmentProxy(w http.ResponseWriter, r *http.Request, id string) {
	p := h.envProxy
	if r.URL.Path == environmentCallbackPath {
		p.completeLaunch(w, r, id)
		return
	}
	if environmentBearer(r) {
		req := r.Clone(r.Context())
		// Only the main origin accepts a platform session cookie.
		req.Header.Del("Cookie")
		middleware.Auth(h.Queries, h.PATCache, h.CloudPAT, h.CFSigner)(http.HandlerFunc(func(_ http.ResponseWriter, authenticated *http.Request) {
			if claims, ok := h.authorizeEnvironmentProxy(w, authenticated, id); ok {
				p.serve(w, r, claims)
			}
		})).ServeHTTP(&environmentProxyAuthWriter{ResponseWriter: w}, req)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions || websocket.IsWebSocketUpgrade(r) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Referer()
		}
		u, err := url.Parse(origin)
		if err != nil || u.User != nil || u.Scheme != p.base.Scheme || !strings.EqualFold(strings.TrimSuffix(u.Host, p.defaultPortSuffix()), p.environmentHost(id)) {
			writeError(w, http.StatusForbidden, "forbidden origin")
			return
		}
	}
	now := time.Now()
	for _, cookie := range r.CookiesNamed(environmentPreviewCookie) {
		var claims environmentSessionClaims
		if openEnvironmentClaims(p.sessionBox, cookie.Value, &claims) && claims.EnvID == id && p.state.sessionValid(claims, now) {
			p.serve(w, r, claims)
			return
		}
	}
	if !environmentNavigation(r) {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return
	}
	environmentPrivateResponse(w)
	target := p.authOrigin.ResolveReference(environmentLaunchURL(id, environmentNext(r.URL.RequestURI()), false))
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func stripEnvironmentCredentials(r *http.Request) {
	var cookies []string
	for _, header := range r.Header.Values("Cookie") {
		for _, cookie := range strings.Split(header, ";") {
			cookie = strings.TrimLeft(cookie, " \t")
			if cookie != "" && !isEnvironmentPlatformCookie(cookie) {
				cookies = append(cookies, cookie)
			}
		}
	}
	r.Header.Del("Cookie")
	if len(cookies) != 0 {
		r.Header.Set("Cookie", strings.Join(cookies, "; "))
	}
	for _, name := range []string{
		"Authorization",
		"X-User-ID",
		"X-User-Email",
		"X-Actor-Source",
		"X-Agent-ID",
		"X-Task-ID",
		"X-CSRF-Token",
		"X-Workspace-ID",
		"X-Workspace-Slug",
		"X-Client-Capabilities",
		"X-Client-Type",
		"X-Client-Version",
		"X-Env-Tunnel-Key",
		"X-Env-Tunnel-Port",
	} {
		r.Header.Del(name)
	}
	for name := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-multica-") {
			r.Header.Del(name)
		}
	}
}

func isEnvironmentPlatformCookie(cookie string) bool {
	pair, _, _ := strings.Cut(cookie, ";")
	name, value, _ := strings.Cut(pair, "=")
	// Browsers send nameless cookie values as bare request cookie names.
	if strings.TrimSpace(name) == "" {
		name = strings.Trim(strings.TrimSpace(value), "\"")
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "multica_")
}

func (p *environmentProxy) initProxy(dial func(context.Context, string, string) (net.Conn, error)) {
	p.transport = &http.Transport{
		DialContext:           dial,
		MaxConnsPerHost:       6,
		MaxIdleConnsPerHost:   4,
		MaxIdleConns:          64,
		IdleConnTimeout:       time.Minute,
		ResponseHeaderTimeout: time.Minute,
		ExpectContinueTimeout: time.Second,
	}
	p.proxy = &httputil.ReverseProxy{
		Transport:     p.transport,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			target := pr.In.Context().Value(environmentTunnelContextKey{}).(environmentSessionClaims)
			port := strconv.Itoa(int(target.Port))
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = target.EnvID + "." + target.RuntimeID + ".env.internal:" + port
			pr.Out.Host = "localhost:" + port
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Proto", p.base.Scheme)
			stripEnvironmentCredentials(pr.Out)
		},
		// ReverseProxy calls ModifyResponse for both ordinary and 101 responses.
		ModifyResponse: func(resp *http.Response) error {
			cookies := resp.Header.Values("Set-Cookie")
			resp.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				if !isEnvironmentPlatformCookie(cookie) {
					resp.Header.Add("Set-Cookie", cookie)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			var bodyErr *http.MaxBytesError
			switch {
			case errors.As(err, &bodyErr):
				writeErrorCode(w, http.StatusRequestEntityTooLarge, "request_body_too_large", "environment preview request body exceeds the limit")
			case errors.Is(err, errEnvironmentTunnelLimit):
				writeErrorCode(w, http.StatusTooManyRequests, "environment_tunnel_limit_reached", "too many active environment tunnels")
			case errors.Is(err, errEnvironmentSessionRevoked):
				writeError(w, http.StatusGone, "environment session revoked")
			case errors.Is(err, context.DeadlineExceeded):
				writeError(w, http.StatusServiceUnavailable, "environment tunnel unavailable")
			default:
				writeError(w, http.StatusBadGateway, "environment tunnel failed")
			}
		},
	}
}

func (p *environmentProxy) serve(w http.ResponseWriter, r *http.Request, claims environmentSessionClaims) {
	lease := p.admitEnvironment(w, claims, true)
	if lease == nil {
		return
	}
	defer p.state.releaseEnvironment(lease)
	if r.ContentLength > p.limits.MaxBodyBytes {
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "request_body_too_large", "environment preview request body exceeds the limit")
		return
	}
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, r.Body, p.limits.MaxBodyBytes)
	}
	p.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), environmentTunnelContextKey{}, claims)))
}
