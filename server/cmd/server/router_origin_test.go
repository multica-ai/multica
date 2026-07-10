package main

import (
	"slices"
	"testing"
)

func TestAllowedOriginsIncludesNextAlternateDevPort(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	origins := allowedOrigins()
	if !slices.Contains(origins, "http://localhost:3001") {
		t.Fatalf("allowedOrigins() = %v, want localhost:3001 for alternate Next dev server", origins)
	}
}

func TestAllowedOriginsExpandsLocalConfiguredOrigin(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")
	t.Setenv("FRONTEND_ORIGIN", "")

	origins := allowedOrigins()
	if !slices.Contains(origins, "http://localhost:3001") {
		t.Fatalf("allowedOrigins() = %v, want localhost:3001 when local dev origin is configured", origins)
	}
}

func TestAllowedOriginsDoesNotExpandProductionOrigin(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://multica.ai")
	t.Setenv("FRONTEND_ORIGIN", "")

	origins := allowedOrigins()
	if slices.Contains(origins, "http://localhost:3001") {
		t.Fatalf("allowedOrigins() = %v, did not want localhost:3001 for production origin", origins)
	}
}
