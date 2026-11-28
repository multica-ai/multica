package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestReleaseSourceDefaultsToGitHub(t *testing.T) {
	t.Setenv(releaseAPIBaseURLEnv, "")
	t.Setenv(releaseDownloadBaseURLEnv, "")

	if got := releaseAPIBaseURL(); got != defaultReleaseAPIBaseURL {
		t.Fatalf("release API base URL = %q, want %q", got, defaultReleaseAPIBaseURL)
	}
	if got := releaseAssetDownloadURL("archive.tar.gz", "v1.2.3", "https://github.example/archive.tar.gz"); got != "https://github.example/archive.tar.gz" {
		t.Fatalf("fallback download URL = %q", got)
	}
}

func TestReleaseSourceOverridesGitHub(t *testing.T) {
	t.Setenv(releaseAPIBaseURLEnv, "https://mirror.example/api/")
	t.Setenv(releaseDownloadBaseURLEnv, "https://mirror.example/releases/")

	if got := releaseAPIBaseURL(); got != "https://mirror.example/api" {
		t.Fatalf("release API base URL = %q", got)
	}
	if got := releaseAssetDownloadURL("multica-cli-1.2.3-linux-amd64.tar.gz", "v1.2.3", "https://github.example/archive.tar.gz"); got != "https://mirror.example/releases/v1.2.3/multica-cli-1.2.3-linux-amd64.tar.gz" {
		t.Fatalf("mirror download URL = %q", got)
	}
}

func TestFetchLatestReleaseUsesConfiguredAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/multica-ai/multica/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GitHubRelease{TagName: "v9.8.7"})
	}))
	defer server.Close()
	t.Setenv(releaseAPIBaseURLEnv, server.URL)

	got, err := FetchLatestRelease()
	if err != nil {
		t.Fatalf("FetchLatestRelease() error = %v", err)
	}
	if got.TagName != "v9.8.7" {
		t.Fatalf("release tag = %q, want v9.8.7", got.TagName)
	}
}

func TestFetchLatestReleaseFileMirrorRequiresPinWithoutNetwork(t *testing.T) {
	for _, version := range []string{"", "dev", "../latest", "v1.2.3-42-gabc"} {
		t.Run(version, func(t *testing.T) {
			var requests atomic.Int32
			mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected network lookup", 500)
			}))
			defer mirror.Close()
			t.Setenv(releaseDownloadBaseURLEnv, mirror.URL)
			t.Setenv(releaseAPIBaseURLEnv, mirror.URL)
			t.Setenv("MULTICA_CLI_VERSION", version)
			if release, err := FetchLatestRelease(); err == nil {
				t.Fatalf("release=%+v, want a missing/invalid pin error", release)
			}
			if requests.Load() != 0 {
				t.Fatalf("made %d network requests for invalid mirror pin", requests.Load())
			}
		})
	}
}

func TestFetchLatestReleaseFileMirrorUsesPinWithoutAPI(t *testing.T) {
	var requests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected release API", 500)
	}))
	defer mirror.Close()
	t.Setenv(releaseDownloadBaseURLEnv, mirror.URL)
	t.Setenv(releaseAPIBaseURLEnv, mirror.URL)
	t.Setenv("MULTICA_CLI_VERSION", "1.2.3")
	got, err := FetchLatestRelease()
	if err != nil {
		t.Fatal(err)
	}
	if got.TagName != "v1.2.3" {
		t.Fatalf("tag=%q, want v1.2.3", got.TagName)
	}
	if requests.Load() != 0 {
		t.Fatalf("made %d network requests instead of using pin", requests.Load())
	}
}
