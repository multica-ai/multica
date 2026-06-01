package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

func TestAdminAPI_HealthAndList(t *testing.T) {
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))
	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-A"}}}

	srv := httptest.NewServer(NewAdminMux(cache, cfg))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/repos")
	if err != nil {
		t.Fatalf("repos: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repos status: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminAPI_FetchUnknownReturns404(t *testing.T) {
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))
	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-A"}}}

	srv := httptest.NewServer(NewAdminMux(cache, cfg))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST",
		srv.URL+"/repos/fetch?workspace_id=ws-A&url=https://nope.invalid/x.git", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown repo, got %d", resp.StatusCode)
	}
}

func TestAdminAPI_FetchRequiresParams(t *testing.T) {
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))
	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-A"}}}
	srv := httptest.NewServer(NewAdminMux(cache, cfg))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL+"/repos/fetch", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing params, got %d", resp.StatusCode)
	}
}

func TestAdminAPI_FetchRejectsGet(t *testing.T) {
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))
	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-A"}}}
	srv := httptest.NewServer(NewAdminMux(cache, cfg))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/repos/fetch?workspace_id=ws-A&url=x")
	if err != nil {
		t.Fatalf("fetch GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET /repos/fetch, got %d", resp.StatusCode)
	}
}

func TestMetricsMux_Serves(t *testing.T) {
	srv := httptest.NewServer(NewMetricsMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("metrics status: %d", resp.StatusCode)
	}
}
