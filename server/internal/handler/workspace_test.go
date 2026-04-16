package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateWorkspace_RejectsReservedSlug(t *testing.T) {
	reserved := []string{
		// Auth + onboarding (covered by migration 043 audit)
		"login",
		"onboarding",
		"invite",
		"api",
		"settings",
		"admin",
		"auth",
		"signup",
		"logout",
		"_next",
		"favicon.ico",
		"robots.txt",
		"sitemap.xml",
		// Dashboard route segments (covered by migration 045 audit)
		"issues",
		"projects",
		"agents",
		"inbox",
		"my-issues",
	}

	for _, slug := range reserved {
		t.Run(slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name": fmt.Sprintf("Test %s", slug),
				"slug": slug,
			})
			testHandler.CreateWorkspace(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("slug %q: expected 400, got %d: %s", slug, w.Code, w.Body.String())
			}
		})
	}
}
