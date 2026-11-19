package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/envtunnel"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testPreviewBase = "http://*.preview.example.com"
	testAppOrigin   = "http://app.example.net"
)

func proxyTestHandler(t *testing.T, limits environmentProxyLimits) *Handler {
	t.Helper()
	cfg := Config{
		AppURL: testAppOrigin, PublicURL: "http://api.example.net",
		PluginSurfaceOrigin: "http://plugins.example.org", EnvProxyBaseURL: testPreviewBase,
	}
	h := &Handler{Queries: testHandler.Queries, DB: testPool, cfg: cfg, envProxy: newEnvironmentProxy(cfg)}
	h.envProxy.limits = limits.withDefaults()
	h.envProxy.initProxy(h.dialEnvironmentTunnel)
	t.Cleanup(h.envProxy.transport.CloseIdleConnections)
	return h
}

func proxyRouter(h *Handler) http.Handler {
	router := chi.NewRouter()
	router.Use(h.PluginSurfaceHostBoundary, h.EnvironmentProxyHostBoundary, middleware.ContentSecurityPolicy)
	router.Get(environmentLaunchPath, h.LaunchEnvironmentProxy)
	router.Get("/api/daemon/runtimes/{runtimeId}/environment-tunnel/{streamId}", h.ConnectEnvironmentTunnel)
	router.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.DaemonAuth(h.Queries, nil, nil, nil))
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	return router
}

func callPreview(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func proxyToken(t *testing.T, user string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user, "exp": time.Now().Add(auth.AuthTokenTTL()).Unix()}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func proxyFixture(t *testing.T) (id, runtimeID, issueID string) {
	t.Helper()
	issueID, taskID := issueEnvironmentFixture(t)
	reportEnvironments(t, taskID, webEnvironment("running")).Want(200)
	dbfx.QueryRow(t, `SELECT id,runtime_id FROM issue_environment WHERE task_id=$1`, taskID).Scan(&id, &runtimeID)
	enableEnvironmentTunnel(t, runtimeID)
	return
}

func enableEnvironmentTunnel(t *testing.T, runtimeID string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_runtime SET status='online',metadata=jsonb_build_object('capabilities',jsonb_build_array($2::text)) WHERE id=$1`,
		runtimeID, protocol.DaemonCapabilityEnvironmentTunnelV1)
}

func TestEnvironmentProxyOriginAndRedirectBoundary(t *testing.T) {
	for _, raw := range []string{
		"ftp://*.preview.example.com", "https://preview.example.com", "https://*.*.com", "https://*.com",
		"https://*.co.uk", "https://*.preview.example.com:99999", "https://*.preview.example.com?",
		"https://*.preview.example.com/path", "https://user@*.preview.example.com",
	} {
		if newEnvironmentProxy(Config{EnvProxyBaseURL: raw, AppURL: testAppOrigin}) != nil {
			t.Errorf("invalid preview origin accepted: %s", raw)
		}
	}
	for _, cfg := range []Config{
		{EnvProxyBaseURL: testPreviewBase, AppURL: testAppOrigin, BackendReplicas: 2},
		{EnvProxyBaseURL: testPreviewBase, AppURL: "http://app.preview.example.com:80"},
		{EnvProxyBaseURL: testPreviewBase, AppURL: "http://PREVIEW.example.com.:3000"},
		{EnvProxyBaseURL: testPreviewBase, AppURL: "https://example.com"},
		{EnvProxyBaseURL: testPreviewBase, PublicURL: "http://api.preview.example.com", AppURL: testAppOrigin},
		{EnvProxyBaseURL: testPreviewBase, PublicURL: "https://preview.example.com:8443", AppURL: testAppOrigin},
		{EnvProxyBaseURL: testPreviewBase, PublicURL: "https://example.com", AppURL: testAppOrigin},
		{EnvProxyBaseURL: testPreviewBase, AppURL: testAppOrigin, PluginSurfaceOrigin: "https://plugins.preview.example.com"},
		{EnvProxyBaseURL: testPreviewBase, AppURL: testAppOrigin, PluginSurfaceOrigin: "https://preview.example.com"},
		{EnvProxyBaseURL: testPreviewBase, AppURL: testAppOrigin, PluginSurfaceOrigin: "https://example.com"},
		{EnvProxyBaseURL: testPreviewBase, AppURL: testAppOrigin, AttachmentFrameAncestors: []string{"https://desktop.preview.example.com"}},
		{EnvProxyBaseURL: "http://*.localhost:8080", AppURL: "http://app.localhost:3000"},
		{EnvProxyBaseURL: testPreviewBase},
	} {
		if newEnvironmentProxy(cfg) != nil {
			t.Errorf("invalid deployment accepted: %+v", cfg)
		}
	}
	for _, base := range []string{"http://*.localhost:8080", "http://*.127.0.0.1.nip.io:8080", "https://env-*.preview.example.com"} {
		cfg := Config{EnvProxyBaseURL: base, AppURL: "http://localhost:3000", BackendReplicas: 1}
		p := newEnvironmentProxy(cfg)
		if p == nil {
			t.Fatalf("valid config disabled: %s", base)
		}
		id := uuid.NewString()
		host := p.environmentHost(id)
		if resolved, owned := p.hostEnvironment(host); resolved != id || !owned {
			t.Fatalf("host %q matched %q, owned=%v", host, resolved, owned)
		}
		if resolved, owned := p.hostEnvironment("other." + p.suffix); resolved != "" || !owned {
			t.Fatalf("non-environment preview host reached app: %q, owned=%v", resolved, owned)
		}
	}
	for _, raw := range []string{"//other.example/", "/%2fother.example/", "/\\other.example", "/%5cother.example", "/%0d%0a", "https://other.example/", environmentCallbackPath} {
		if next := environmentNext(raw); next != "/" {
			t.Errorf("unsafe next %q kept as %q", raw, next)
		}
	}
	if next := environmentNext("/a%20b?a=1&b=2"); next != "/a%20b?a=1&b=2" {
		t.Fatalf("relative path changed: %q", next)
	}
}

func TestEnvironmentProxyAuthorizationAndState(t *testing.T) {
	id, rt, issue := proxyFixture(t)
	h := proxyTestHandler(t, environmentProxyLimits{})
	router := proxyRouter(h)
	outsider := dbfx.User(t, "outside", "env-outside-"+uuid.NewString()+"@example.com")
	project := newPrivateProjectFixture(t)
	memberToken := proxyToken(t, testUserID)
	serve := func(token, environmentID string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, testAppOrigin+environmentLaunchURL(environmentID, "/", true).String(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := callPreview(router, req)
		return rec.Code, rec.Body.String()
	}
	missingStatus, missingBody := serve(memberToken, uuid.NewString())
	if missingStatus != http.StatusNotFound {
		t.Fatalf("missing environment status = %d", missingStatus)
	}
	// A hidden environment must be indistinguishable from a missing one, so
	// these cases compare the whole response, not just the status code.
	assertHidden := func(label, token string) {
		t.Helper()
		status, body := serve(token, id)
		if status != missingStatus || body != missingBody {
			t.Fatalf("%s got %d %q, want the missing environment's %d %q", label, status, body, missingStatus, missingBody)
		}
	}
	assertHidden("nonmember", proxyToken(t, outsider))
	dbfx.Exec(t, `UPDATE issue SET project_id=$1 WHERE id=$2`, project.projectID, issue)
	assertHidden("private project outsider", proxyToken(t, project.outsider))
	assertHidden("private project workspace admin", proxyToken(t, project.outsideAdmin))
	// The fixture environment runs on an online, tunnel-capable runtime, so a
	// member who can see the project must actually launch, not merely differ
	// from the hidden case.
	if status, body := serve(memberToken, id); status != http.StatusFound {
		t.Fatalf("private project member launch = %d %s, want %d", status, body, http.StatusFound)
	}
	dbfx.Exec(t, `UPDATE issue SET project_id=NULL WHERE id=$1`, issue)
	for _, state := range []string{"stopped", "exited"} {
		dbfx.Exec(t, `UPDATE issue_environment SET state=$1 WHERE id=$2`, state, id)
		if got, _ := serve(memberToken, id); got != 410 {
			t.Fatalf("%s status = %d", state, got)
		}
	}
	dbfx.Exec(t, `UPDATE issue_environment SET state='running',last_seen_at=now()-interval '10 minutes' WHERE id=$1`, id)
	if got, _ := serve(memberToken, id); got != 503 {
		t.Fatalf("stale environment status = %d", got)
	}
	dbfx.Exec(t, `UPDATE issue_environment SET last_seen_at=now(),port=NULL WHERE id=$1`, id)
	if got, _ := serve(memberToken, id); got != 404 {
		t.Fatalf("missing port status = %d", got)
	}
	dbfx.Exec(t, `UPDATE issue_environment SET port=41000 WHERE id=$1`, id)
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata='{}' WHERE id=$1`, rt)
	if got, _ := serve(memberToken, id); got != 409 {
		t.Fatalf("legacy daemon status = %d", got)
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, rt)
	if got, _ := serve(memberToken, id); got != 503 {
		t.Fatalf("offline daemon status = %d", got)
	}
	dbfx.Exec(t, `DELETE FROM issue_environment WHERE id=$1`, id)
	if got, _ := serve(memberToken, id); got != 404 {
		t.Fatalf("missing environment status = %d", got)
	}
}

// TestEnvironmentProxyGuestAlwaysHidden locks the answer a workspace guest
// gets from both preview entry points. Guests are members the plan does not
// give runtimes or environments (MUL-9420), so neither the launch endpoint nor
// the preview origin's bearer path may reveal whether an environment exists,
// and a guest must never hand the proxy a platform path to claim that route's
// guest allowance: the preview origin's path is the developer's app, not ours.
func TestEnvironmentProxyGuestAlwaysHidden(t *testing.T) {
	guest := newGuestFixture(t)
	runtimeID := dbfx.Runtime(t, "guest preview runtime")
	enableEnvironmentTunnel(t, runtimeID)
	agentID := dbfx.Agent(t, "guest preview agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": guest.sharedIssue, "runtime_id": runtimeID, "status": "running"})
	environment := func(issueID, key string) string {
		return dbfx.Insert(t, "issue_environment", testutil.Cols{
			"workspace_id": testWorkspaceID, "issue_id": issueID, "task_id": taskID,
			"runtime_id": runtimeID, "local_key": key, "source": "config", "name": key,
			"state": "running", "started_at": testutil.Raw("now()"), "port": 41000,
		})
	}
	sharedEnv := environment(guest.sharedIssue, "shared")
	privateEnv := environment(guest.secretIssue, "private")
	looseEnv := environment(guest.looseIssue, "loose")

	h := proxyTestHandler(t, environmentProxyLimits{})
	router := proxyRouter(h)
	guestToken, memberToken := proxyToken(t, guest.guest), proxyToken(t, testUserID)

	launch := func(token, environmentID string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, testAppOrigin+environmentLaunchURL(environmentID, "/", true).String(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := callPreview(router, req)
		return rec.Code, rec.Body.String()
	}
	missingStatus, missingBody := launch(guestToken, uuid.NewString())
	if missingStatus != http.StatusNotFound {
		t.Fatalf("missing environment status = %d %s, want %d", missingStatus, missingBody, http.StatusNotFound)
	}
	assertGuestHidden := func(label string, status int, body string) {
		t.Helper()
		if status != missingStatus || body != missingBody {
			t.Fatalf("%s got %d %q, want the missing environment's %d %q", label, status, body, missingStatus, missingBody)
		}
	}
	for _, tc := range []struct{ label, environment string }{
		{"guest launch of a shared project's environment", sharedEnv},
		{"guest launch of a private project's environment", privateEnv},
		{"guest launch of a project-less environment", looseEnv},
	} {
		status, body := launch(guestToken, tc.environment)
		assertGuestHidden(tc.label, status, body)
	}
	// The fixture environments are launchable, so the answers above come from
	// the guest rule rather than from a broken environment.
	for _, tc := range []struct{ label, environment string }{
		{"member launch of a shared project's environment", sharedEnv},
		{"member launch of a project-less environment", looseEnv},
	} {
		if status, body := launch(memberToken, tc.environment); status != http.StatusFound {
			t.Fatalf("%s = %d %s, want %d", tc.label, status, body, http.StatusFound)
		}
	}

	// A guest bearer request on the preview origin must not reach the tunnel,
	// whatever path it names: /api/issues and /api/runtimes are the platform
	// routes whose guest allowance the proxy path could otherwise smuggle in.
	tunnel := atomic.Int32{}
	h.envProxy.notifyTunnel = func(string, string) bool {
		tunnel.Add(1)
		return false
	}
	preview := func(token, environmentID, path string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, "http://"+h.envProxy.environmentHost(environmentID)+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := callPreview(router, req)
		return rec.Code, rec.Body.String()
	}
	originMissing, originMissingBody := preview(guestToken, uuid.NewString(), "/api/issues")
	if originMissing != http.StatusNotFound {
		t.Fatalf("missing environment on the preview origin = %d %s, want %d", originMissing, originMissingBody, http.StatusNotFound)
	}
	for _, environmentID := range []string{sharedEnv, privateEnv} {
		for _, path := range []string{"/", "/api/issues", "/api/runtimes"} {
			status, body := preview(guestToken, environmentID, path)
			assertGuestHidden("guest preview origin "+environmentID+path, status, body)
		}
	}
	if got := tunnel.Load(); got != 0 {
		t.Fatalf("guest preview requests opened %d tunnel attempts, want none", got)
	}
}

func TestEnvironmentProxyTaskTokenWorkspace(t *testing.T) {
	id, rt, _ := proxyFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, rt)
	otherWS := dbfx.Workspace(t, "other preview workspace", "preview-other-"+uuid.NewString())
	dbfx.Member(t, otherWS, testUserID, "owner")
	h := proxyTestHandler(t, environmentProxyLimits{})
	address := "http://" + h.envProxy.environmentHost(id)
	for _, wsID := range []string{testWorkspaceID, otherWS} {
		fx := testutil.New(testPool, wsID, testUserID)
		runtimeID := fx.Runtime(t, "token runtime")
		agentID := fx.Agent(t, "token agent", runtimeID)
		taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
		token, err := auth.GenerateAgentTaskToken()
		if err != nil {
			t.Fatal(err)
		}
		fx.Insert(t, "task_token", testutil.Cols{"token_hash": auth.HashToken(token), "task_id": taskID, "agent_id": agentID, "workspace_id": wsID, "user_id": testUserID, "expires_at": time.Now().Add(time.Hour)})
		want := http.StatusServiceUnavailable // Same-workspace auth reaches the offline runtime.
		if wsID != testWorkspaceID {
			want = http.StatusForbidden
		}
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			req := httptest.NewRequest(method, address, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Actor-Source", "member")
			if got := callPreview(proxyRouter(h), req).Code; got != want {
				t.Errorf("workspace=%s method=%s status=%d want=%d", wsID, method, got, want)
			}
		}
	}
}

type previewQueryCounter struct {
	db.DBTX
	count atomic.Int64
}

func (db *previewQueryCounter) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	db.count.Add(1)
	return db.DBTX.QueryRow(ctx, sql, args...)
}

func (db *previewQueryCounter) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.count.Add(1)
	return db.DBTX.Query(ctx, sql, args...)
}

func (db *previewQueryCounter) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.count.Add(1)
	return db.DBTX.Exec(ctx, sql, args...)
}

func launchPreview(t *testing.T, router http.Handler, id string) (*http.Cookie, string) {
	t.Helper()
	launchPath := environmentLaunchURL(id, "/docs?q=1&f=2", true)
	req := httptest.NewRequest(http.MethodGet, testAppOrigin+launchPath.String(), nil)
	req.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: proxyToken(t, testUserID)})
	rec := callPreview(router, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("launch failed: %d %s", rec.Code, rec.Body.String())
	}
	callback := rec.Header().Get("Location")
	u, err := url.Parse(callback)
	if err != nil || u.Path != environmentCallbackPath || u.Query().Get("ticket") == "" {
		t.Fatal("invalid callback URL")
	}
	wrongHost := *u
	wrongHost.Host = "env-" + strings.ReplaceAll(uuid.NewString(), "-", "") + ".preview.example.com"
	if got := callPreview(router, httptest.NewRequest(http.MethodGet, wrongHost.String(), nil)); got.Code != http.StatusUnauthorized {
		t.Fatalf("cross-environment ticket accepted: %d", got.Code)
	}
	rec = callPreview(router, httptest.NewRequest(http.MethodGet, callback, nil))
	cookies := rec.Result().Cookies()
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/docs?q=1&f=2" || len(cookies) != 1 {
		t.Fatalf("callback failed: %d %s", rec.Code, rec.Body.String())
	}
	cookie := cookies[0]
	if cookie.Name != environmentPreviewCookie || cookie.Domain != "" || !cookie.HttpOnly || cookie.Secure ||
		cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 600 {
		t.Fatalf("invalid preview cookie attributes: %s", cookie.Name)
	}
	if replay := callPreview(router, httptest.NewRequest(http.MethodGet, callback, nil)); replay.Code != http.StatusUnauthorized {
		t.Fatalf("launch replay accepted: %d", replay.Code)
	}
	return cookie, u.Host
}

func startPreviewTunnelTest(t *testing.T, h *Handler, router http.Handler, upstream http.Handler) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	local := httptest.NewServer(upstream)
	server := httptest.NewServer(router)
	var opened atomic.Int32
	var peers sync.WaitGroup
	ctx, cancel := context.WithCancel(t.Context())
	h.envProxy.notifyTunnel = func(runtime, kind string) bool {
		peers.Go(func() {
			payload := strings.TrimPrefix(kind, protocol.PendingWorkKindEnvironmentTunnelPrefix)
			stream, secret, _ := strings.Cut(payload, ".")
			endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/daemon/runtimes/" + runtime + "/environment-tunnel/" + stream
			ws, resp, err := websocket.DefaultDialer.DialContext(ctx, endpoint, http.Header{"Authorization": {"Bearer " + secret}})
			if err != nil {
				if resp != nil {
					resp.Body.Close()
				}
				t.Errorf("tunnel rendezvous failed: %v", err)
				return
			}
			opened.Add(1)
			conn := envtunnel.NewConn(ws)
			defer conn.Close()
			if resp.Header.Get("X-Env-Tunnel-Key") != "config:web" || resp.Header.Get("X-Env-Tunnel-Port") != "41000" {
				t.Error("tunnel lost the registered environment port binding")
			}
			peer, err := (&net.Dialer{}).DialContext(ctx, "tcp", local.Listener.Addr().String())
			if err != nil {
				t.Error(err)
				return
			}
			defer peer.Close()
			stop := context.AfterFunc(ctx, func() {
				conn.Close()
				peer.Close()
			})
			defer stop()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = io.Copy(conn, peer)
				conn.Close()
			}()
			_, _ = io.Copy(peer, conn)
			peer.Close()
			<-done
		})
		return true
	}
	t.Cleanup(func() {
		server.Close()
		cancel()
		h.envProxy.transport.CloseIdleConnections()
		peers.Wait()
		local.Close()
	})
	return server, &opened
}

func TestEnvironmentProxyHandshakeAndZeroQueryDataPlane(t *testing.T) {
	id, runtimeID, _ := proxyFixture(t)
	h := proxyTestHandler(t, environmentProxyLimits{MaxBodyBytes: 64})
	queries := &previewQueryCounter{DBTX: testPool}
	h.Queries, h.DB = db.New(queries), queries
	router := proxyRouter(h)
	previewOrigin := "http://" + h.envProxy.environmentHost(id)
	initial := httptest.NewRequest(http.MethodGet, previewOrigin+"/docs?q=1&f=2", nil)
	initial.Header.Set("Sec-Fetch-Mode", "navigate")
	initial.Header.Set("Sec-Fetch-Dest", "document")
	initial.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: proxyToken(t, testUserID)})
	rec := callPreview(router, initial)
	want := testAppOrigin + environmentLaunchURL(id, "/docs?q=1&f=2", false).String()
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
		t.Fatalf("initial redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec = callPreview(router, httptest.NewRequest(http.MethodGet, want, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Security-Policy") == "" ||
		!strings.Contains(rec.Body.String(), `http-equiv="refresh"`) || !strings.Contains(rec.Body.String(), "bounced=1") ||
		queries.count.Load() != 0 {
		t.Fatalf("cross-site bounce = %d %s, queries=%d", rec.Code, rec.Body.String(), queries.count.Load())
	}
	missing := callPreview(router, httptest.NewRequest(http.MethodGet, testAppOrigin+environmentLaunchURL(id, "/", true).String(), nil))
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated second hop = %d", missing.Code)
	}
	if blocked := callPreview(router, httptest.NewRequest(http.MethodGet, h.cfg.PluginSurfaceOrigin+"/api/health", nil)); blocked.Code != http.StatusNotFound {
		t.Fatalf("plugin host reached main API: %d", blocked.Code)
	}

	cookie, host := launchPreview(t, router, id)
	before := queries.count.Load()
	if before == 0 {
		t.Fatal("launch never performed the authoritative database lookup")
	}
	requests := make(chan *http.Request, 32)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Add("Set-Cookie", "multica_env_session=upstream; Path=/")
		w.Header().Add("Set-Cookie", "=MuLtIcA_auth; Path=/")
		w.Header().Add("Set-Cookie", "local_pref=on; Path=/")
		switch r.URL.Path {
		case "/socket":
			ws, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, w.Header())
			if err != nil {
				return
			}
			defer ws.Close()
			kind, message, err := ws.ReadMessage()
			if err == nil {
				ws.WriteMessage(kind, message)
			}
			_, _, _ = ws.ReadMessage()
		case "/redirect":
			http.Redirect(w, r, "/next?q=1", http.StatusFound)
		default:
			if _, err := io.Copy(io.Discard, r.Body); err == nil {
				io.WriteString(w, r.URL.RequestURI())
			}
		}
	})
	server, opened := startPreviewTunnelTest(t, h, router, upstream)
	client := &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	do := func(t *testing.T, method, path, origin string, body io.Reader, upgrade bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Origin", origin)
		req.Header.Set("Authorization", "Basic local-marker")
		req.Header.Set("X-User-ID", "platform-marker")
		req.Header.Set("X-Multica-Client", "platform-marker")
		if upgrade {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		req.AddCookie(cookie)
		req.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: "platform-marker"})
		req.AddCookie(&http.Cookie{Name: "local_pref", Value: "on"})
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	assertCookies := func(resp *http.Response) {
		t.Helper()
		if cookies := resp.Header.Values("Set-Cookie"); len(cookies) != 1 || cookies[0] != "local_pref=on; Path=/" {
			t.Errorf("status %d forwarded cookies %v", resp.StatusCode, cookies)
		}
	}
	for _, path := range []string{"/docs?q=1", "/style.css", "/app.js", "/favicon.ico", "/api/data", "/rediscover"} {
		resp := do(t, http.MethodGet, path, "", nil, false)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || string(body) != path {
			t.Fatalf("GET %s = %d %q %v", path, resp.StatusCode, body, err)
		}
		assertCookies(resp)
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("sequential HTTP requests opened %d tunnels, want 1", got)
	}
	resp := do(t, http.MethodGet, "/redirect", "", nil, false)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/next?q=1" {
		t.Fatalf("upstream redirect changed: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	for _, tc := range []struct {
		name    string
		origin  string
		body    io.Reader
		upgrade bool
		want    int
	}{
		{"same origin", previewOrigin, strings.NewReader("allowed"), false, http.StatusOK},
		{"cross origin", "http://other.example.net", strings.NewReader("foreign"), false, http.StatusForbidden},
		{"no origin", "", nil, false, http.StatusForbidden},
		{"at limit", previewOrigin, strings.NewReader(strings.Repeat("x", 64)), false, http.StatusOK},
		{"over limit", previewOrigin, strings.NewReader(strings.Repeat("x", 65)), false, http.StatusRequestEntityTooLarge},
		{"chunked over limit", previewOrigin, io.NopCloser(strings.NewReader(strings.Repeat("x", 65))), false, http.StatusRequestEntityTooLarge},
		{"over limit with upgrade headers", previewOrigin, strings.NewReader(strings.Repeat("x", 65)), true, http.StatusRequestEntityTooLarge},
		{"chunked over limit with upgrade headers", previewOrigin, io.NopCloser(strings.NewReader(strings.Repeat("x", 65))), true, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, http.MethodPost, "/submit", tc.origin, tc.body, tc.upgrade)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != tc.want {
				t.Fatalf("POST = %d %q, err=%v, want %d", resp.StatusCode, body, err, tc.want)
			}
		})
	}

	headers := http.Header{"Host": {host}, "Origin": {"http://other.example.net"}, "Cookie": {cookie.String()}}
	socketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	_, resp, err := websocket.DefaultDialer.Dial(socketURL, headers)
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign WebSocket origin accepted: %v", err)
	}
	resp.Body.Close()
	headers.Set("Origin", previewOrigin)
	ws, resp, err := websocket.DefaultDialer.Dial(socketURL, headers)
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}
	defer ws.Close()
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("WebSocket handshake status = %d", resp.StatusCode)
	}
	assertCookies(resp)
	wantMessage := strings.Repeat("preview ready", 8)
	if err := ws.WriteMessage(websocket.TextMessage, []byte(wantMessage)); err != nil {
		t.Fatal(err)
	}
	_, message, err := ws.ReadMessage()
	if err != nil || string(message) != wantMessage {
		t.Fatalf("WebSocket message = %q, %v", message, err)
	}
	closeRequests := len(requests)
	for i := 0; i < closeRequests; i++ {
		req := <-requests
		if req.Header.Get("Authorization") != "" || req.Header.Get("X-User-ID") != "" ||
			req.Header.Get("X-Multica-Client") != "" || strings.Contains(req.Header.Get("Cookie"), "multica_") ||
			req.Header.Get("X-Forwarded-Host") != host || req.Host != "localhost:41000" {
			t.Errorf("upstream credential or host boundary failed on %s", req.URL.Path)
		}
		if req.Method == http.MethodGet && req.URL.Path == "/style.css" && req.Header.Get("Cookie") != "local_pref=on" {
			t.Error("application cookie lost")
		}
	}
	h.NotifyRuntimeGone(runtimeID)
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("runtime removal did not close the live WebSocket")
	}
	resp = do(t, http.MethodGet, "/style.css", "", nil, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked session served an asset: %d", resp.StatusCode)
	}
	if delta := queries.count.Load() - before; delta != 0 {
		t.Fatalf("HTTP, WebSocket and tunnel rendezvous used %d database queries", delta)
	}
}

func TestEnvironmentProxySessionEnvironmentBinding(t *testing.T) {
	h := proxyTestHandler(t, environmentProxyLimits{})
	router := proxyRouter(h)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Host)
	}))
	defer upstream.Close()
	dialer := &net.Dialer{}
	h.envProxy.transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, upstream.Listener.Addr().String())
	}

	now := time.Now()
	runtimeID := uuid.NewString()
	session := func(id string, port int32) *http.Cookie {
		t.Helper()
		claims := environmentSessionClaims{
			UserID: testUserID, WorkspaceID: testWorkspaceID, RuntimeID: runtimeID,
			EnvID: id, LocalKey: "config:web", Port: port,
			IssuedAt: now.UnixNano(), ExpiresAt: now.Add(environmentSessionTTL).Unix(),
		}
		token, err := sealEnvironmentClaims(h.envProxy.sessionBox, claims)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: environmentPreviewCookie, Value: token}
	}
	id := uuid.NewString()
	foreign := session(uuid.NewString(), 41000)
	matching := session(id, 41001)

	const path = "/preview?tab=1"
	for _, tc := range []struct {
		name     string
		mode     string
		cookies  []*http.Cookie
		status   int
		location string
	}{
		{"foreign session on asset", "no-cors", []*http.Cookie{foreign}, http.StatusUnauthorized, ""},
		{"foreign session on navigation", "navigate", []*http.Cookie{foreign}, http.StatusFound, testAppOrigin + environmentLaunchURL(id, path, false).String()},
		{"foreign cookie first", "no-cors", []*http.Cookie{foreign, matching}, http.StatusOK, ""},
		{"matching cookie first", "no-cors", []*http.Cookie{matching, foreign}, http.StatusOK, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+h.envProxy.environmentHost(id)+path, nil)
			req.Header.Set("Sec-Fetch-Mode", tc.mode)
			for _, cookie := range tc.cookies {
				req.AddCookie(cookie)
			}
			rec := callPreview(router, req)
			if got := rec.Header().Get("Location"); rec.Code != tc.status || got != tc.location {
				t.Fatalf("status = %d, Location = %q; want %d, %q", rec.Code, got, tc.status, tc.location)
			}
			if tc.status == http.StatusOK && rec.Body.String() != "localhost:41001" {
				t.Fatalf("upstream host = %q; want localhost:41001", rec.Body.String())
			}
		})
	}
}

func TestEnvironmentProxyEntitlementAndDirectURLs(t *testing.T) {
	issue, task := issueEnvironmentFixture(t)
	reportEnvironments(t, task, webEnvironment("running"), map[string]any{
		"key": "detected:test", "source": "detected", "name": "detected",
		"url": "http://localhost:5173/docs?q=1", "port": 5173, "state": "running",
	}).Want(200)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM issue_environment WHERE task_id=$1 LIMIT 1`, task).Scan(&runtimeID)
	h := proxyTestHandler(t, environmentProxyLimits{})
	previousProxy, previousGate := testHandler.envProxy, testHandler.Entitlements
	t.Cleanup(func() { testHandler.envProxy, testHandler.Entitlements = previousProxy, previousGate })
	list := func() map[string]IssueEnvironmentResponse { return listEnvironments(t, issue) }
	original := list()
	checkURLs := func(want map[string]IssueEnvironmentResponse) {
		t.Helper()
		got := list()
		if len(got) != 2 || len(want) != 2 {
			t.Fatalf("sidebar environment count = %d, want 2", len(got))
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].URL != want[i].URL {
				t.Errorf("sidebar environment %s URL = %s, want %s", got[i].ID, got[i].URL, want[i].URL)
			}
		}
	}
	testHandler.envProxy = h.envProxy
	checkURLs(original) // Legacy runtime.
	enableEnvironmentTunnel(t, runtimeID)
	gates := entitlementtest.New()
	h.Entitlements, testHandler.Entitlements = gates, gates
	router := proxyRouter(h)
	setGate := func(action entitlement.Action, limit int) {
		t.Helper()
		gates.Set(uuid.MustParse(testWorkspaceID), entitlement.GateEnvironmentProxy,
			entitlement.Decision{Gate: entitlement.Gate{Action: action, Limit: &limit}, PolicyRevision: 17})
	}
	launch := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, testAppOrigin+environmentLaunchURL(id, "/", false).String(), nil)
		req.Header.Set("Authorization", "Bearer "+proxyToken(t, testUserID))
		return callPreview(router, req)
	}
	wantError := func(rec *httptest.ResponseRecorder, code string, limit int) {
		t.Helper()
		var got struct {
			Code     string `json:"code"`
			Limit    int    `json:"limit"`
			Revision int    `json:"policy_revision"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusPaymentRequired ||
			got.Code != code || got.Limit != limit || got.Revision != 17 {
			t.Errorf("limit response = %d %s", rec.Code, rec.Body.String())
		}
	}
	pendingLaunches := func() int {
		h.envProxy.state.mu.Lock()
		defer h.envProxy.state.mu.Unlock()
		return len(h.envProxy.state.launchNonces)
	}
	setGate(entitlement.ActionOff, 0)
	for _, env := range list() {
		if !strings.HasPrefix(env.URL, "http://"+h.envProxy.environmentHost(env.ID)+"/") {
			t.Fatalf("ActionOff left loopback URL %s", env.URL)
		}
	}
	setGate(entitlement.ActionEnforce, 0)
	checkURLs(original)
	wantError(launch(original["web"].ID), "environment_proxy_disabled", 0)
	directBearer := httptest.NewRequest(http.MethodGet, "http://"+h.envProxy.environmentHost(original["web"].ID), nil)
	directBearer.Header.Set("Authorization", "Bearer "+proxyToken(t, testUserID))
	wantError(callPreview(router, directBearer), "environment_proxy_disabled", 0)
	setGate(entitlement.ActionEnforce, 1)
	if got := launch(original["web"].ID); got.Code != http.StatusFound {
		t.Fatalf("first active environment launch = %d %s", got.Code, got.Body.String())
	}
	beforeReject := pendingLaunches()
	wantError(launch(original["detected"].ID), "environment_proxy_limit_reached", 1)
	if after := pendingLaunches(); after != beforeReject {
		t.Fatalf("rejected launch leaked launch tickets: %d -> %d", beforeReject, after)
	}
	h.revokeEnvironmentSession(original["web"].ID)
	if got := launch(original["detected"].ID); got.Code != http.StatusFound {
		t.Fatalf("revocation did not release the active environment: %d %s", got.Code, got.Body.String())
	}

	for name, env := range original {
		env.URL = strings.Replace(env.URL, "localhost", "dev-machine.internal", 1)
		dbfx.Exec(t, `UPDATE issue_environment SET url=$1 WHERE id=$2`, env.URL, env.ID)
		original[name] = env
	}
	for _, action := range []entitlement.Action{entitlement.ActionOff, entitlement.ActionEnforce} {
		setGate(action, 0)
		checkURLs(original)
	}
	testHandler.envProxy = nil
	checkURLs(original)
}

func TestEnvironmentProxyLaunchQueueFullKeepsEntitlementSlot(t *testing.T) {
	issue, task := issueEnvironmentFixture(t)
	reportEnvironments(t, task, webEnvironment("running"), map[string]any{
		"key": "detected:test", "source": "detected", "name": "detected",
		"url": "http://localhost:5173/", "port": 5173, "state": "running",
	}).Want(http.StatusOK)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM issue_environment WHERE task_id=$1 LIMIT 1`, task).Scan(&runtimeID)
	enableEnvironmentTunnel(t, runtimeID)

	h := proxyTestHandler(t, environmentProxyLimits{})
	gates, limit := entitlementtest.New(), 1
	gates.Set(uuid.MustParse(testWorkspaceID), entitlement.GateEnvironmentProxy,
		entitlement.Decision{Gate: entitlement.Gate{Action: entitlement.ActionEnforce, Limit: &limit}, PolicyRevision: 17})
	h.Entitlements = gates
	router := proxyRouter(h)
	envs := listEnvironments(t, issue)
	launch := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, testAppOrigin+environmentLaunchURL(id, "/", false).String(), nil)
		req.Header.Set("Authorization", "Bearer "+proxyToken(t, testUserID))
		return callPreview(router, req)
	}
	for i := 0; i < maxEnvironmentLaunches; i++ {
		h.envProxy.state.addLaunch(uuid.NewString(), time.Now().Add(environmentLaunchTTL).Unix())
	}
	if rec := launch(envs["web"].ID); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("full launch queue = %d %s", rec.Code, rec.Body.String())
	}
	h.envProxy.state.mu.Lock()
	h.envProxy.state.launchNonces = make(map[string]int64)
	h.envProxy.state.mu.Unlock()
	if rec := launch(envs["detected"].ID); rec.Code != http.StatusFound {
		t.Fatalf("rejected launch consumed the workspace entitlement slot: %d %s", rec.Code, rec.Body.String())
	}
}

func TestEnvironmentProxyTunnelLimitsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limits  environmentProxyLimits
		otherWS bool
	}{
		{name: "workspace", limits: environmentProxyLimits{WorkspaceTunnels: 1, GlobalTunnels: 2}},
		{name: "global", limits: environmentProxyLimits{WorkspaceTunnels: 2, GlobalTunnels: 1}, otherWS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := proxyTestHandler(t, tc.limits)
			p := h.envProxy
			target := environmentSessionClaims{
				EnvID: uuid.NewString(), WorkspaceID: uuid.NewString(), RuntimeID: uuid.NewString(), Port: 41000,
				IssuedAt: time.Now().UnixNano(), ExpiresAt: time.Now().Add(time.Minute).Unix(),
			}
			entered := make(chan struct{}, 1)
			h.envProxy.notifyTunnel = func(string, string) bool {
				entered <- struct{}{}
				return true
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				ctx := context.WithValue(ctx, environmentTunnelContextKey{}, target)
				_, err := h.dialEnvironmentTunnel(ctx, "tcp", "")
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("first dial did not enter")
			}
			other := target
			other.EnvID = uuid.NewString()
			if tc.otherWS {
				other.WorkspaceID = uuid.NewString()
			}
			req := httptest.NewRequest(http.MethodGet, "http://"+p.environmentHost(other.EnvID), nil)
			rec := httptest.NewRecorder()
			p.serve(rec, req, other)
			if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "environment_tunnel_limit_reached") {
				t.Fatalf("second dial = %d %s", rec.Code, rec.Body.String())
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled dial = %v", err)
			}
			if p.state.activeGlobal != 0 || len(p.state.pending) != 0 || len(p.state.slots) != 0 {
				t.Fatal("cancel left tunnel broker state behind")
			}
		})
	}
}

func TestEnvironmentProxyNotifyTunnelProductionPath(t *testing.T) {
	hub := daemonws.NewHub()
	h := New(testHandler.Queries, testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService,
		nil, nil, analytics.NoopClient{},
		Config{AppURL: testAppOrigin, PublicURL: "http://api.example.net", EnvProxyBaseURL: testPreviewBase}, hub)
	t.Cleanup(h.envProxy.transport.CloseIdleConnections)
	notifier := &pendingWorkRecorder{}
	h.DaemonPendingWork = notifier

	target := environmentSessionClaims{
		EnvID:       uuid.NewString(),
		WorkspaceID: uuid.NewString(),
		RuntimeID:   uuid.NewString(),
		Port:        41000,
		IssuedAt:    time.Now().UnixNano(),
		ExpiresAt:   time.Now().Add(time.Minute).Unix(),
	}
	req := httptest.NewRequest(http.MethodGet, "http://"+h.envProxy.environmentHost(target.EnvID)+"/", nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	h.envProxy.serve(rec, req, target)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "environment tunnel unavailable") {
		t.Fatalf("empty hub = %d %s", rec.Code, rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("empty hub waited %s", elapsed)
	}
	if s := &h.envProxy.state; s.activeGlobal != 0 || len(s.pending) != 0 || len(s.slots) != 0 {
		t.Fatal("failed dial leaked tunnel state")
	}
	if got := notifier.count(); got != 0 {
		t.Fatalf("empty hub used the generic notifier %d times", got)
	}

	runtimeID := uuid.NewString()
	identity := daemonws.ClientIdentity{
		UserID: testUserID, WorkspaceID: testWorkspaceID, WorkspaceIDs: []string{testWorkspaceID},
		RuntimeIDs: []string{runtimeID},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for hub.RuntimeConnectionCount(runtimeID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runtime websocket was not registered")
		}
		time.Sleep(time.Millisecond)
	}

	const kind = "environment_tunnel:test-stream.test-secret"
	if !h.envProxy.notifyTunnel(runtimeID, kind) {
		t.Fatal("connected runtime was reported unreachable")
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var msg protocol.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	var payload protocol.PendingWorkPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if msg.Type != protocol.EventDaemonPendingWork || payload.RuntimeID != runtimeID || payload.Kind != kind {
		t.Fatalf("unexpected notify frame: %s %+v", msg.Type, payload)
	}
	if got := notifier.count(); got != 0 {
		t.Fatalf("connected runtime used the generic notifier %d times", got)
	}
}

func TestEnvironmentProxySnapshotRevokesSession(t *testing.T) {
	_, task := issueEnvironmentFixture(t)
	report := webEnvironment("running")
	reportEnvironments(t, task, report).Want(http.StatusOK)
	var id, runtimeID string
	dbfx.QueryRow(t, `SELECT id,runtime_id FROM issue_environment WHERE task_id=$1`, task).Scan(&id, &runtimeID)
	enableEnvironmentTunnel(t, runtimeID)
	h := proxyTestHandler(t, environmentProxyLimits{})
	previous := testHandler.envProxy
	testHandler.envProxy = h.envProxy
	t.Cleanup(func() { testHandler.envProxy = previous })
	router := proxyRouter(h)
	cookie, host := launchPreview(t, router, id)
	report["port"], report["url"] = 41001, "http://localhost:41001"
	reportEnvironments(t, task, report).Want(http.StatusOK)
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/style.css", nil)
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.AddCookie(cookie)
	if got := callPreview(router, req); got.Code != http.StatusUnauthorized {
		t.Fatalf("old preview cookie survived a port change: %d %s", got.Code, got.Body.String())
	}
}

func TestEnvironmentProxyRendezvousBinding(t *testing.T) {
	h := proxyTestHandler(t, environmentProxyLimits{})
	router := proxyRouter(h)
	state := &h.envProxy.state
	runtimeID := uuid.NewString()
	stream, secret := uuid.NewString(), uuid.NewString()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state.pending[stream] = &pendingEnvironmentTunnel{
		target: environmentSessionClaims{RuntimeID: runtimeID},
		secret: secret, ctx: ctx, cancel: cancel,
	}
	dial := func(runtime, credential string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/daemon/runtimes/"+runtime+"/environment-tunnel/"+stream, nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		return callPreview(router, req).Code
	}
	if got := dial(runtimeID, uuid.NewString()); got != http.StatusNotFound {
		t.Fatalf("wrong rendezvous credential = %d", got)
	}
	if got := dial(uuid.NewString(), secret); got != http.StatusNotFound {
		t.Fatalf("wrong rendezvous runtime = %d", got)
	}
	// The authenticated callback reaches the upgrade check, which refuses
	// this deliberately ordinary GET and consumes the pending entry.
	if got := dial(runtimeID, secret); got != http.StatusBadRequest {
		t.Fatalf("authenticated callback = %d", got)
	}
	if got := dial(runtimeID, secret); got != http.StatusNotFound {
		t.Fatalf("replayed rendezvous credential = %d", got)
	}
}

func TestEnvironmentProxyTunnelLifetime(t *testing.T) {
	id, _, _ := proxyFixture(t)
	h := proxyTestHandler(t, environmentProxyLimits{TunnelLifetime: 500 * time.Millisecond})
	router := proxyRouter(h)
	cookie, host := launchPreview(t, router, id)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_, _, _ = ws.ReadMessage()
	})
	server, _ := startPreviewTunnelTest(t, h, router, upstream)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/socket",
		http.Header{"Host": {host}, "Origin": {"http://" + host}, "Cookie": {cookie.String()}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = ws.ReadMessage()
	var netErr net.Error
	if err == nil || (errors.As(err, &netErr) && netErr.Timeout()) {
		t.Fatalf("tunnel lifetime did not close the WebSocket: %v", err)
	}
}

func TestEnvironmentProxyBandwidth(t *testing.T) {
	start := time.Now()
	limiter := &environmentBandwidth{rate: 20000, last: start}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := limiter.wait(ctx, 1000); err != nil || time.Since(start) < 45*time.Millisecond {
		t.Fatalf("bandwidth limiter did not wait: %s, %v", time.Since(start), err)
	}
	cancel()
	if err := limiter.wait(ctx, environmentTunnelChunkSize); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed connection kept waiting for bandwidth: %v", err)
	}
}
