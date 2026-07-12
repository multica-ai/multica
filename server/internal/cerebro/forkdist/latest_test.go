package forkdist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestVersionUsesConfiguredPublicReleaseRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/firtal-group/homebrew-tap/releases/latest" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer server.Close()
	t.Setenv("MULTICA_GITHUB_API_BASE", server.URL)
	resetLatestVersionCache()

	got, err := LatestVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.2.3" {
		t.Fatalf("LatestVersion = %q, want v1.2.3", got)
	}
}

func TestIsNewerVersion(t *testing.T) {
	if !IsNewerVersion("v1.2.3", "1.2.2") {
		t.Fatal("v1.2.3 should be newer than 1.2.2")
	}
	if IsNewerVersion("v1.2.3", "v1.2.3") {
		t.Fatal("equal versions must not be newer")
	}
	if IsNewerVersion("not-a-version", "v1.2.2") {
		t.Fatal("invalid latest version must fail closed")
	}
}
