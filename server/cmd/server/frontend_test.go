package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendHandlerServesWorkspaceSPA(t *testing.T) {
	distDir := t.TempDir()
	indexPath := filepath.Join(distDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("workspace index"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	t.Setenv("WORKSPACE_DIST_DIR", distDir)
	t.Setenv("WORKSPACE_SITE_ORIGIN", "")
	t.Setenv("MARKETING_SITE_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/issues", nil)
	rec := httptest.NewRecorder()

	newFrontendHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "workspace index") {
		t.Fatalf("expected workspace index, got %q", rec.Body.String())
	}
}

func TestFrontendHandlerServesWorkspaceAssets(t *testing.T) {
	distDir := t.TempDir()
	assetPath := filepath.Join(distDir, "assets", "app.js")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("console.log('workspace asset');"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	t.Setenv("WORKSPACE_DIST_DIR", distDir)
	t.Setenv("WORKSPACE_SITE_ORIGIN", "")
	t.Setenv("MARKETING_SITE_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()

	newFrontendHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "workspace asset") {
		t.Fatalf("expected workspace asset, got %q", rec.Body.String())
	}
}

func TestFrontendHandlerProxiesMarketingRoutes(t *testing.T) {
	marketing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "marketing homepage")
	}))
	defer marketing.Close()

	t.Setenv("WORKSPACE_DIST_DIR", t.TempDir())
	t.Setenv("WORKSPACE_SITE_ORIGIN", "")
	t.Setenv("MARKETING_SITE_ORIGIN", marketing.URL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	newFrontendHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "marketing homepage") {
		t.Fatalf("expected marketing homepage, got %q", rec.Body.String())
	}
}

func TestFrontendHandlerProxiesWorkspaceDevRequests(t *testing.T) {
	workspace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "workspace dev server")
	}))
	defer workspace.Close()

	t.Setenv("WORKSPACE_DIST_DIR", t.TempDir())
	t.Setenv("WORKSPACE_SITE_ORIGIN", workspace.URL)
	t.Setenv("MARKETING_SITE_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	newFrontendHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "workspace dev server") {
		t.Fatalf("expected workspace dev server, got %q", rec.Body.String())
	}
}
