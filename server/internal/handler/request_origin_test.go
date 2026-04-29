package handler

import (
	"net/http"
	"testing"
)

func TestRequestAppOriginPrefersBrowserOrigin(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://backend:8080/api/issues/123/comments", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("X-Forwarded-Host", "multica.wujieai.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	if got := requestAppOrigin(req); got != "http://localhost:3000" {
		t.Fatalf("requestAppOrigin() = %q, want %q", got, "http://localhost:3000")
	}
}

func TestRequestAppOriginFallsBackToForwardedHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://backend:8080/api/issues/123/comments", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Forwarded-Host", "localhost:3000")
	req.Header.Set("X-Forwarded-Proto", "http")

	if got := requestAppOrigin(req); got != "http://localhost:3000" {
		t.Fatalf("requestAppOrigin() = %q, want %q", got, "http://localhost:3000")
	}
}

func TestRequestAppOriginIgnoresBackendHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://backend:8080/api/issues/123/comments", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	if got := requestAppOrigin(req); got != "" {
		t.Fatalf("requestAppOrigin() = %q, want empty", got)
	}
}
