package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// captureLogger returns a *slog.Logger whose output lands in buf, so tests
// can assert on the daemon's user-facing warning text without scraping
// stderr.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestClient_RenewToken_PostsToCorrectEndpoint(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/tokens/current/renew" {
			t.Errorf("expected /api/tokens/current/renew, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mul_abc" {
			t.Errorf("expected Bearer mul_abc, got %q", got)
		}
		// Body must be valid JSON — postJSON marshals an empty object when
		// reqBody is a non-nil map[string]any{}.
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"expires_at": "2099-01-02T03:04:05Z",
			"renewed":    true,
		})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL)
	c.SetToken("mul_abc")

	resp, err := c.RenewToken(context.Background())
	if err != nil {
		t.Fatalf("RenewToken: %v", err)
	}
	if called.Load() != 1 {
		t.Fatalf("expected 1 server call, got %d", called.Load())
	}
	if !resp.Renewed {
		t.Fatal("expected renewed=true")
	}
	if resp.ExpiresAt != "2099-01-02T03:04:05Z" {
		t.Fatalf("expected expires_at to round-trip, got %q", resp.ExpiresAt)
	}
}

func TestTryRenewToken_LogsRenewalOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"expires_at": "2099-01-02T03:04:05Z",
			"renewed":    true,
		})
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.tryRenewToken(context.Background())

	out := buf.String()
	if !strings.Contains(out, "auth token renewed") {
		t.Fatalf("expected 'auth token renewed' log, got: %s", out)
	}
	if !strings.Contains(out, "2099-01-02T03:04:05Z") {
		t.Fatalf("expected new expiry in log, got: %s", out)
	}
}

func TestTryRenewToken_LogsNotEligibleOnNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"expires_at": "2099-01-02T03:04:05Z",
			"renewed":    false,
		})
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.tryRenewToken(context.Background())

	out := buf.String()
	// Non-renewal must NOT emit the warning that an operator would interpret
	// as "something is wrong" — it's the normal steady-state for tokens with
	// plenty of life left.
	if strings.Contains(out, "WARN") {
		t.Fatalf("no-op renewal should not log at WARN, got: %s", out)
	}
}

func TestTryRenewToken_SurfacesReloginWarningOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.tryRenewToken(context.Background())

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("401 must surface as WARN, got: %s", out)
	}
	if !strings.Contains(out, "multica login") {
		t.Fatalf("401 warning must tell the user to run 'multica login', got: %s", out)
	}
}

func TestTryRenewToken_SurfacesReloginWarningOn401_WithProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{
		client: NewClient(srv.URL),
		logger: captureLogger(&buf),
		cfg:    Config{Profile: "staging"},
	}
	d.tryRenewToken(context.Background())

	out := buf.String()
	if !strings.Contains(out, "--profile staging") {
		t.Fatalf("profile-aware login hint missing, got: %s", out)
	}
}

func TestTryRenewToken_TransientErrorIsDebugNotWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"db down"}`))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.tryRenewToken(context.Background())

	out := buf.String()
	// A 500 is transient — the next tick will retry, so the operator should
	// NOT see a re-login warning that doesn't reflect the actual cause.
	if strings.Contains(out, "level=WARN") {
		t.Fatalf("transient 500 should not log at WARN, got: %s", out)
	}
	if !strings.Contains(out, "token renewal failed") {
		t.Fatalf("expected debug log about renewal failure, got: %s", out)
	}
}

// TestPreflightAuth_RenewsBeforeWorkspaceSyncOnExpiredToken locks in the
// must-fix from MUL-2744 review: when the daemon starts with an already-
// revoked or expired PAT, the renewal call has to happen BEFORE the first
// workspace sync, because the workspace sync's 401 would short-circuit Run
// and the operator would never see a "run multica login" hint.
func TestPreflightAuth_RenewsBeforeWorkspaceSyncOnExpiredToken(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		// Both endpoints 401 — this is the "PAT already revoked/expired
		// before the daemon even started" failure mode.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.client.SetToken("mul_already_revoked")

	err := d.preflightAuth(context.Background())
	if err == nil {
		t.Fatal("expected workspace sync to fail with 401")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 {
		t.Fatalf("expected both endpoints to be called; got %v", seen)
	}
	if seen[0] != "/api/tokens/current/renew" {
		t.Fatalf("renew must be the first API call so the WARN fires before the sync 401s; got order %v", seen)
	}
	if seen[1] != "/api/workspaces" {
		t.Fatalf("workspace sync should follow renew; got order %v", seen)
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected re-login WARN, got: %s", out)
	}
	if !strings.Contains(out, "multica login") {
		t.Fatalf("expected the actionable 'run multica login' hint in the WARN, got: %s", out)
	}
}

// TestPreflightAuth_SyncProceedsWhenRenewIsNoOp covers the steady-state
// startup: a PAT well outside the renewal window returns renewed=false,
// and preflightAuth must still go on to do the workspace sync. The
// renewal is best-effort and must not gate startup.
func TestPreflightAuth_SyncProceedsWhenRenewIsNoOp(t *testing.T) {
	var syncCalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens/current/renew":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"expires_at": "2099-01-02T03:04:05Z",
				"renewed":    false,
			})
		case "/api/workspaces":
			syncCalled.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.client.SetToken("mul_healthy")

	if err := d.preflightAuth(context.Background()); err != nil {
		t.Fatalf("preflightAuth returned error on healthy startup: %v", err)
	}
	if !syncCalled.Load() {
		t.Fatal("preflightAuth must run the workspace sync after a no-op renewal")
	}
}

// TestPreflightAuth_TransientRenewFailureDoesNotBlockStartup covers the
// "renewal endpoint is briefly down" path. The renewal failure must not
// kill the daemon — the workspace sync still happens, and the daemon is
// up and serving. The background renewal loop will retry later.
func TestPreflightAuth_TransientRenewFailureDoesNotBlockStartup(t *testing.T) {
	var syncCalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens/current/renew":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"db down"}`))
		case "/api/workspaces":
			syncCalled.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.client.SetToken("mul_healthy")

	if err := d.preflightAuth(context.Background()); err != nil {
		t.Fatalf("preflightAuth must not surface transient renew failures: %v", err)
	}
	if !syncCalled.Load() {
		t.Fatal("transient renew failure must not skip the workspace sync")
	}
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("transient 500 must not emit the re-login WARN, got: %s", buf.String())
	}
}

func TestTryRenewToken_RespectsContextTimeout(t *testing.T) {
	// Server that never responds — the per-call 15s timeout inside
	// tryRenewToken is too long for a unit test, so cancel the parent
	// context immediately and verify the call returns.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		d.tryRenewToken(ctx)
		close(done)
	}()
	select {
	case <-done:
		// Expected: tryRenewToken returns once the cancelled ctx propagates
		// through the HTTP client.
	case <-context.Background().Done():
	}
}
