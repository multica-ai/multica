package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestBroker(t *testing.T, initial *TokenState, isLeader bool, anthropicSrv string) (*Broker, *fake.Clientset) {
	t.Helper()
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
		Data: map[string][]byte{
			"access_token":  []byte(initial.AccessToken),
			"refresh_token": []byte(initial.RefreshToken),
			"expires_at":    []byte(initial.ExpiresAt.Format(time.RFC3339)),
		},
	}
	k := fake.NewSimpleClientset(sec)
	store := NewSecretStore(k, "ns", "s", "ns-access-token")
	oauth := newClientForTest(anthropicSrv, "client-id-x", "oauth-2025-04-20")
	refresher := NewRefresher(store, &stubLeader{leader: isLeader}, oauth, 5*time.Minute)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := NewBroker(refresher, store, logger)
	if err := b.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	return b, k
}

func TestAdminMux_AccessTokenFresh_NoRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream must not be called for fresh token")
	}))
	defer srv.Close()
	state := &TokenState{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(1 * time.Hour)}
	b, _ := newTestBroker(t, state, true, srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/access_token", nil)
	NewAdminMux(b).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body)
	}
	if w.Body.String() != "A" {
		t.Errorf("body = %q, want A", w.Body.String())
	}
}

func TestAdminMux_AccessTokenStale_RefreshesAndServes(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ROTATED",
			"refresh_token": "R2",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	state := &TokenState{AccessToken: "OLD", RefreshToken: "R", ExpiresAt: time.Now().Add(1 * time.Minute)}
	b, _ := newTestBroker(t, state, true, srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/access_token", nil)
	NewAdminMux(b).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Body.String() != "ROTATED" {
		t.Errorf("expected rotated token in body, got %q", w.Body.String())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("upstream calls = %d, want 1", atomic.LoadInt32(&calls))
	}
}

func TestAdminMux_AccessTokenExpiredAndNotLeader_503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("non-leader must not call upstream")
	}))
	defer srv.Close()
	state := &TokenState{AccessToken: "EXPIRED", RefreshToken: "R", ExpiresAt: time.Now().Add(-1 * time.Minute)}
	b, _ := newTestBroker(t, state, false, srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/access_token", nil)
	NewAdminMux(b).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%s", w.Code, w.Body)
	}
}

func TestAdminMux_Readyz(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	state := &TokenState{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(1 * time.Hour)}
	b, _ := newTestBroker(t, state, true, srv.URL)

	w := httptest.NewRecorder()
	NewAdminMux(b).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Errorf("ready status = %d", w.Code)
	}
}

func TestAdminMux_DoesNotExposeRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream must not be called via admin mux /refresh")
	}))
	defer srv.Close()
	state := &TokenState{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(1 * time.Hour)}
	b, _ := newTestBroker(t, state, true, srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	NewAdminMux(b).ServeHTTP(w, r)
	// ServeMux returns 404 for unregistered paths.
	if w.Code != http.StatusNotFound {
		t.Errorf("/refresh on admin mux must 404 (loopback-only), got %d", w.Code)
	}
}

func TestOpsMux_RefreshForcesRefresh(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "FORCED",
			"refresh_token": "R2",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	// Even though state is fresh, /refresh forces a call.
	state := &TokenState{AccessToken: "FRESH", RefreshToken: "R", ExpiresAt: time.Now().Add(1 * time.Hour)}
	b, _ := newTestBroker(t, state, true, srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	NewOpsMux(b).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("upstream calls = %d, want 1", atomic.LoadInt32(&calls))
	}
	if !strings.Contains(w.Body.String(), "refreshed") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestOpsMux_RefreshOnNonLeader_503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	state := &TokenState{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(1 * time.Hour)}
	b, _ := newTestBroker(t, state, false, srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	NewOpsMux(b).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("non-leader /refresh = %d, want 503", w.Code)
	}
}

func TestOpsMux_GetRefreshIsMethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	state := &TokenState{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(1 * time.Hour)}
	b, _ := newTestBroker(t, state, true, srv.URL)

	w := httptest.NewRecorder()
	NewOpsMux(b).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/refresh", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /refresh = %d, want 405", w.Code)
	}
}

func TestMetricsMux_HasMetricsEndpoint(t *testing.T) {
	w := httptest.NewRecorder()
	NewMetricsMux().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Errorf("metrics status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "multica_claude_broker_constants_info") {
		t.Errorf("metrics missing constants_info gauge; body excerpt: %.200s", w.Body.String())
	}
}
