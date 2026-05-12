package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildInfoHandler_DefaultUnbuilt verifies the handler returns
// the dev defaults when ldflags injection didn't happen (i.e. local
// `go test` runs, where main.version and main.commit retain their
// init values). The deploy workflow's post-deploy verifier treats
// commit="unknown" as a hard failure — so the default values being
// exactly this set is the load-bearing assumption.
func TestBuildInfoHandler_DefaultUnbuilt(t *testing.T) {
	h := buildInfoHandler()
	req := httptest.NewRequest(http.MethodGet, "/build_info", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Commit    string `json:"commit"`
		Version   string `json:"version"`
		GoVersion string `json:"go_version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// During `go test`, the test binary doesn't apply the production
	// -ldflags so commit/version retain their main.go defaults.
	if body.Commit != "unknown" {
		t.Errorf("commit = %q, want %q (test binary has no ldflags)", body.Commit, "unknown")
	}
	if body.Version != "dev" {
		t.Errorf("version = %q, want %q", body.Version, "dev")
	}
	if body.GoVersion == "" {
		t.Error("go_version is empty; runtime.Version() should always populate it")
	}
}

// TestBuildInfoHandler_OverriddenCommit verifies the handler reflects
// the package-level vars at request time — proves that when ldflags
// HAS injected a commit (via -X main.commit=<sha>), the response
// carries that SHA. We can't easily inject via ldflags from a test,
// but we can mutate the package-level var directly to simulate the
// same effect.
func TestBuildInfoHandler_OverriddenCommit(t *testing.T) {
	prevCommit, prevVersion := commit, version
	commit = "abc123deadbeef"
	version = "v1.2.3"
	t.Cleanup(func() {
		commit = prevCommit
		version = prevVersion
	})

	h := buildInfoHandler()
	req := httptest.NewRequest(http.MethodGet, "/build_info", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	var body struct {
		Commit  string `json:"commit"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Commit != "abc123deadbeef" {
		t.Errorf("commit = %q, want %q", body.Commit, "abc123deadbeef")
	}
	if body.Version != "v1.2.3" {
		t.Errorf("version = %q, want %q", body.Version, "v1.2.3")
	}
}
