package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshToken_PostsCorrectShape(t *testing.T) {
	var gotBody string
	var gotVersion, gotCT, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("anthropic-version")
		gotCT = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ACCESS_NEW",
			"refresh_token": "REFRESH_ROTATED",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         "user:inference",
		})
	}))
	defer srv.Close()

	c := newClientForTest(srv.URL, "9d1c250a-test", "oauth-2025-04-20")
	out, err := c.Refresh(context.Background(), "REFRESH_OLD")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if out.AccessToken != "ACCESS_NEW" || out.RefreshToken != "REFRESH_ROTATED" {
		t.Errorf("unexpected output: %+v", out)
	}
	if gotVersion != "oauth-2025-04-20" {
		t.Errorf("version header = %q", gotVersion)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q", gotCT)
	}
	if !strings.HasPrefix(gotUA, "multica-claude-broker/") {
		t.Errorf("user-agent = %q", gotUA)
	}
	for _, fragment := range []string{
		`"grant_type":"refresh_token"`,
		`"refresh_token":"REFRESH_OLD"`,
		`"client_id":"9d1c250a-test"`,
	} {
		if !strings.Contains(gotBody, fragment) {
			t.Errorf("body missing %s; got: %s", fragment, gotBody)
		}
	}
}

func TestRefreshToken_4xxIsTerminal(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	_, err := c.Refresh(context.Background(), "stale")
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server called %d times; expected exactly 1 (no retries on 4xx)", got)
	}
}

func TestRefreshToken_5xxRetriesThenFails(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "upstream blew up", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	c.MaxAttempts = 3
	c.BackoffBase = 1 * time.Millisecond
	_, err := c.Refresh(context.Background(), "fresh")
	if err == nil {
		t.Fatal("expected error")
	}
	var transient *TransientError
	if !errors.As(err, &transient) {
		t.Errorf("expected TransientError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("server called %d times; expected exactly MaxAttempts (3)", got)
	}
}

func TestRefreshToken_5xxThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "transient", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "A",
			"refresh_token": "R",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	c.BackoffBase = 1 * time.Millisecond
	c.MaxAttempts = 3
	out, err := c.Refresh(context.Background(), "fresh")
	if err != nil {
		t.Fatalf("Refresh after one retry: %v", err)
	}
	if out.AccessToken != "A" {
		t.Errorf("got %+v", out)
	}
}

func TestRefreshToken_NoRetryAfter2xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Length", "999999")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("{"))
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	c.MaxAttempts = 5
	c.BackoffBase = 1 * time.Millisecond
	_, err := c.Refresh(context.Background(), "fresh")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server called %d times; must be exactly 1 (don't retry after 2xx)", got)
	}
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got %T: %v", err, err)
	}
}

func TestRefreshToken_2xxMissingAccessTokenIsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "R", "expires_in": 3600})
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	_, err := c.Refresh(context.Background(), "fresh")
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got %T: %v", err, err)
	}
}

func TestRefreshToken_ContextCancelsBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "x", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	c.MaxAttempts = 10
	c.BackoffBase = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := c.Refresh(ctx, "x")
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("context cancellation did not abort backoff (took %v)", elapsed)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshToken_EmptyTokenIsPermanent(t *testing.T) {
	c := newClientForTest("http://example.invalid", "x", "x")
	_, err := c.Refresh(context.Background(), "")
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got %T: %v", err, err)
	}
}

func newClientForTest(endpoint, clientID, version string) *OAuthClient {
	return &OAuthClient{
		Endpoint:      endpoint,
		ClientID:      clientID,
		VersionHeader: version,
		UserAgent:     "multica-claude-broker/test",
		HTTP:          &http.Client{Timeout: 5 * time.Second},
		MaxAttempts:   2,
		BackoffBase:   1 * time.Millisecond,
	}
}
