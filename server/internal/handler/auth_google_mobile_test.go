package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleMobileLogin_MissingIDToken(t *testing.T) {
	h := newTestHandler(Config{AllowSignup: true})
	req := httptest.NewRequest(http.MethodPost, "/auth/google/mobile", strings.NewReader(`{"id_token":""}`))
	rec := httptest.NewRecorder()

	h.GoogleMobileLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGoogleMobileLogin_NotConfigured(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "")
	h := newTestHandler(Config{AllowSignup: true})
	req := httptest.NewRequest(http.MethodPost, "/auth/google/mobile", strings.NewReader(`{"id_token":"token"}`))
	rec := httptest.NewRecorder()

	h.GoogleMobileLogin(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestGoogleMobileLogin_InvalidToken(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "web-client-id")
	restore := withGoogleIDTokenValidator(func(_ context.Context, token, audience string) (googleIDTokenPayload, error) {
		if token != "bad-token" {
			t.Fatalf("expected token bad-token, got %q", token)
		}
		if audience != "web-client-id" {
			t.Fatalf("expected audience web-client-id, got %q", audience)
		}
		return googleIDTokenPayload{}, errInvalidGoogleIDToken
	})
	defer restore()

	h := newTestHandler(Config{AllowSignup: true})
	req := httptest.NewRequest(http.MethodPost, "/auth/google/mobile", strings.NewReader(`{"id_token":"bad-token"}`))
	rec := httptest.NewRecorder()

	h.GoogleMobileLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
