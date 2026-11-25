package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGoogleOAuthDefaultClientHasTimeout(t *testing.T) {
	h := newTestHandler(Config{})
	if got := h.googleHTTPClient().Timeout; got <= 0 {
		t.Fatalf("production Google OAuth client timeout = %s, want a bound", got)
	}
}

func TestGoogleLoginTokenExchangeStopsWhenRequestIsCancelled(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exchangeCancelled := false
	h := newTestHandler(Config{})
	h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "oauth2.googleapis.com" {
			t.Fatalf("unexpected request to %s", req.URL)
		}
		if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("token request Content-Type = %q", got)
		}
		if err := req.ParseForm(); err != nil {
			t.Errorf("parse token request form: %v", err)
		}
		if req.PostForm.Get("code") != "test-code" || req.PostForm.Get("grant_type") != "authorization_code" {
			t.Errorf("token request form = %v", req.PostForm)
		}
		// Simulate a Google endpoint that accepts the connection but never
		// answers, and a browser that gives up on the login request.
		cancel()
		select {
		case <-req.Context().Done():
			exchangeCancelled = true
			return nil, req.Context().Err()
		case <-time.After(5 * time.Second):
			return nil, errors.New("token exchange ignored the request context")
		}
	})}

	req := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"code":"test-code"}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.GoogleLogin(rec, req)

	if !exchangeCancelled {
		t.Fatal("token exchange kept running after the inbound request was cancelled")
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
