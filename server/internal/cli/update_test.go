package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewGitHubGETRequestAuthentication(t *testing.T) {
	t.Run("omits authorization without a token", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")

		req, err := newGitHubGETRequest("https://api.github.com/repos/multica-ai/multica/releases/latest", githubJSONAccept)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
	})

	t.Run("prefers GH_TOKEN and trims whitespace", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "  gh-token  ")
		t.Setenv("GITHUB_TOKEN", "github-token")

		req, err := newGitHubGETRequest("https://api.github.com/repos/multica-ai/multica/releases/latest", githubJSONAccept)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer gh-token" {
			t.Fatalf("Authorization = %q, want GH_TOKEN bearer", got)
		}
		if strings.Contains(req.URL.String(), "gh-token") {
			t.Fatal("token leaked into request URL")
		}
	})

	t.Run("falls back to GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "github-token")

		req, err := newGitHubGETRequest("https://api.github.com/repos/multica-ai/multica/releases/latest", githubJSONAccept)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer github-token" {
			t.Fatalf("Authorization = %q, want GITHUB_TOKEN bearer", got)
		}
	})

	t.Run("never authenticates outside the exact GitHub API origin", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "secret-token")
		t.Setenv("GITHUB_TOKEN", "")

		for _, rawURL := range []string{
			"http://api.github.com/repos/multica-ai/multica/releases/latest",
			"https://api.github.com.evil.example/releases/latest",
			"https://user@api.github.com/repos/multica-ai/multica/releases/latest",
			"https://api.github.com:444/repos/multica-ai/multica/releases/latest",
		} {
			req, err := newGitHubGETRequest(rawURL, githubJSONAccept)
			if err != nil {
				t.Fatalf("newGitHubGETRequest(%q): %v", rawURL, err)
			}
			if got := req.Header.Get("Authorization"); got != "" {
				t.Fatalf("Authorization for %q = %q, want empty", rawURL, got)
			}
		}
	})

	t.Run("authenticates exact GitHub hosts on default HTTPS port", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "secret-token")
		t.Setenv("GITHUB_TOKEN", "")

		for _, rawURL := range []string{
			"https://api.github.com/repos/multica-ai/multica/releases/latest",
			"https://api.github.com:443/repos/multica-ai/multica/releases/latest",
			"https://github.com/multica-ai/multica/releases/download/v1.2.3/archive.tar.gz",
		} {
			req, err := newGitHubGETRequest(rawURL, githubBinaryAccept)
			if err != nil {
				t.Fatalf("newGitHubGETRequest(%q): %v", rawURL, err)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer secret-token" {
				t.Fatalf("Authorization for %q = %q", rawURL, got)
			}
		}
	})

	t.Run("sets stable GitHub API headers", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")

		req, err := newGitHubGETRequest("https://api.github.com/repos/multica-ai/multica/releases/latest", githubJSONAccept)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := req.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("Accept = %q", got)
		}
		if got := req.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
			t.Fatalf("X-GitHub-Api-Version = %q", got)
		}
	})
}

func TestReleaseAssetAPIURL(t *testing.T) {
	t.Run("accepts exact GitHub API asset URL", func(t *testing.T) {
		asset := &GitHubReleaseAsset{
			Name:   "archive.tar.gz",
			APIURL: "https://api.github.com/repos/multica-ai/multica/releases/assets/123",
		}
		got, err := releaseAssetAPIURL(asset)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != asset.APIURL {
			t.Fatalf("URL = %q, want %q", got, asset.APIURL)
		}
	})

	t.Run("rejects missing or untrusted asset URL", func(t *testing.T) {
		for _, asset := range []*GitHubReleaseAsset{
			nil,
			{Name: "missing.tar.gz"},
			{Name: "browser.tar.gz", APIURL: "https://github.com/multica-ai/multica/releases/download/v1.2.3/browser.tar.gz"},
			{Name: "http.tar.gz", APIURL: "http://api.github.com/repos/multica-ai/multica/releases/assets/123"},
			{Name: "evil.tar.gz", APIURL: "https://api.github.com.evil.example/releases/assets/123"},
		} {
			if _, err := releaseAssetAPIURL(asset); err == nil {
				t.Fatalf("expected error for asset %+v", asset)
			}
		}
	})
}

func TestGitHubHTTPClientRedirectAuthentication(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret-token")
	t.Setenv("GITHUB_TOKEN", "")
	client := githubHTTPClient(time.Second)

	initial, err := newGitHubGETRequest("https://github.com/multica-ai/multica/releases/download/v1.2.3/archive.tar.gz", githubBinaryAccept)
	if err != nil {
		t.Fatalf("unexpected initial request error: %v", err)
	}

	t.Run("retains auth only for trusted redirect", func(t *testing.T) {
		redirect, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/multica-ai/multica/releases/latest", nil)
		if err != nil {
			t.Fatalf("unexpected redirect request error: %v", err)
		}
		redirect.Header.Set("Authorization", initial.Header.Get("Authorization"))
		if err := client.CheckRedirect(redirect, []*http.Request{initial}); err != nil {
			t.Fatalf("unexpected redirect error: %v", err)
		}
		if got := redirect.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
	})

	t.Run("strips auth from CDN redirect", func(t *testing.T) {
		redirect, err := http.NewRequest(http.MethodGet, "https://objects.githubusercontent.com/private-release", nil)
		if err != nil {
			t.Fatalf("unexpected redirect request error: %v", err)
		}
		redirect.Header.Set("Authorization", initial.Header.Get("Authorization"))
		if err := client.CheckRedirect(redirect, []*http.Request{initial}); err != nil {
			t.Fatalf("unexpected redirect error: %v", err)
		}
		if got := redirect.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
	})

	t.Run("stops after ten redirects", func(t *testing.T) {
		redirect, err := http.NewRequest(http.MethodGet, "https://github.com/final", nil)
		if err != nil {
			t.Fatalf("unexpected redirect request error: %v", err)
		}
		via := make([]*http.Request, 10)
		for i := range via {
			via[i] = initial
		}
		if err := client.CheckRedirect(redirect, via); err == nil {
			t.Fatal("expected redirect limit error")
		}
	})
}

func TestReleaseAssetCandidates(t *testing.T) {
	tests := []struct {
		name          string
		targetVersion string
		goos          string
		goarch        string
		wantAssets    []string
	}{
		{
			name:          "darwin prefers versioned then legacy candidate",
			targetVersion: "v1.2.3",
			goos:          "darwin",
			goarch:        "arm64",
			wantAssets: []string{
				"multica-cli-1.2.3-darwin-arm64.tar.gz",
				"multica_darwin_arm64.tar.gz",
			},
		},
		{
			name:          "linux normalizes missing v in versioned candidate",
			targetVersion: "1.2.3",
			goos:          "linux",
			goarch:        "amd64",
			wantAssets: []string{
				"multica-cli-1.2.3-linux-amd64.tar.gz",
				"multica_linux_amd64.tar.gz",
			},
		},
		{
			name:          "windows uses zip assets",
			targetVersion: "1.2.3",
			goos:          "windows",
			goarch:        "amd64",
			wantAssets: []string{
				"multica-cli-1.2.3-windows-amd64.zip",
				"multica_windows_amd64.zip",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseAssetCandidates(tt.targetVersion, tt.goos, tt.goarch)
			if len(got) != len(tt.wantAssets) {
				t.Fatalf("candidate count mismatch: got %d, want %d", len(got), len(tt.wantAssets))
			}
			for i := range got {
				if got[i] != tt.wantAssets[i] {
					t.Fatalf("candidate[%d] mismatch: got %q, want %q", i, got[i], tt.wantAssets[i])
				}
			}
		})
	}
}

func TestFindReleaseAsset(t *testing.T) {
	t.Run("prefers versioned asset when both names exist", func(t *testing.T) {
		assets := []GitHubReleaseAsset{
			{Name: "multica_darwin_amd64.tar.gz", BrowserDownloadURL: "old"},
			{Name: "multica-cli-1.2.3-darwin-amd64.tar.gz", BrowserDownloadURL: "new"},
		}

		got, err := findReleaseAsset(assets, "v1.2.3", "darwin", "amd64")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "multica-cli-1.2.3-darwin-amd64.tar.gz" {
			t.Fatalf("asset mismatch: got %q", got.Name)
		}
	})

	t.Run("falls back to legacy asset when versioned is absent", func(t *testing.T) {
		assets := []GitHubReleaseAsset{
			{Name: "multica_linux_amd64.tar.gz", BrowserDownloadURL: "old"},
		}

		got, err := findReleaseAsset(assets, "1.2.3", "linux", "amd64")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "multica_linux_amd64.tar.gz" {
			t.Fatalf("asset mismatch: got %q", got.Name)
		}
	})

	t.Run("returns error when no candidate matches", func(t *testing.T) {
		_, err := findReleaseAsset([]GitHubReleaseAsset{{Name: "checksums.txt"}}, "1.2.3", "linux", "amd64")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestIsReleaseVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"bare release", "0.1.13", true},
		{"v-prefixed release", "v0.1.13", true},
		{"surrounding whitespace", "  v0.1.13  ", true},
		{"dev describe", "v0.2.15-235-gdaf0e935", false},
		{"dirty dev describe", "v0.2.15-235-gdaf0e935-dirty", false},
		{"empty", "", false},
		{"two components", "0.1", false},
		{"four components", "0.1.2.3", false},
		{"non-numeric", "1.0.x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReleaseVersion(tt.in); got != tt.want {
				t.Fatalf("IsReleaseVersion(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"patch bump", "v0.1.14", "v0.1.13", true},
		{"minor bump", "v0.2.0", "v0.1.99", true},
		{"major bump", "v1.0.0", "v0.99.99", true},
		{"same version", "v0.1.13", "v0.1.13", false},
		{"older latest", "v0.1.12", "v0.1.13", false},
		{"mixed v prefix", "0.1.14", "v0.1.13", true},
		{"current is dev describe → unparseable → false", "v0.1.14", "v0.1.13-5-gabcdef0", false},
		{"latest is dev describe → unparseable → false", "v0.1.14-1-gabcdef0", "v0.1.13", false},
		{"latest unparseable → false", "garbage", "v0.1.13", false},
		{"current unparseable → false", "v0.1.14", "garbage", false},
		{"empty latest", "", "v0.1.13", false},
		{"empty current", "v0.1.14", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewerVersion(tt.latest, tt.current); got != tt.want {
				t.Fatalf("IsNewerVersion(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestFindChecksumManifestAsset(t *testing.T) {
	t.Run("finds checksums.txt among assets", func(t *testing.T) {
		assets := []GitHubReleaseAsset{
			{Name: "multica-cli-1.2.3-darwin-arm64.tar.gz"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example/checksums.txt"},
			{Name: "multica-cli-1.2.3-linux-amd64.tar.gz"},
		}
		got, err := findChecksumManifestAsset(assets)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "checksums.txt" || got.BrowserDownloadURL != "https://example/checksums.txt" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("returns error when manifest missing", func(t *testing.T) {
		_, err := findChecksumManifestAsset([]GitHubReleaseAsset{
			{Name: "multica-cli-1.2.3-darwin-arm64.tar.gz"},
		})
		if err == nil {
			t.Fatal("expected error when checksums.txt is absent")
		}
	})
}

func TestParseChecksumManifest(t *testing.T) {
	manifest := []byte(strings.Join([]string{
		"# generated by GoReleaser",
		"",
		"aaaa1111  multica-cli-1.2.3-darwin-arm64.tar.gz",
		"bbbb2222  multica-cli-1.2.3-darwin-amd64.tar.gz",
		"cccc3333\tmulti-tab-separator.tar.gz",
		"DDDD4444  multica_linux_amd64.tar.gz",
	}, "\n"))

	t.Run("returns lowercase sha for matched entry", func(t *testing.T) {
		got, err := parseChecksumManifest(manifest, "multica-cli-1.2.3-darwin-arm64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "aaaa1111" {
			t.Fatalf("sha = %q, want aaaa1111", got)
		}
	})

	t.Run("matches a tab-separated entry", func(t *testing.T) {
		got, err := parseChecksumManifest(manifest, "multi-tab-separator.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "cccc3333" {
			t.Fatalf("sha = %q, want cccc3333", got)
		}
	})

	t.Run("downcases an uppercase entry", func(t *testing.T) {
		got, err := parseChecksumManifest(manifest, "multica_linux_amd64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "dddd4444" {
			t.Fatalf("sha = %q, want dddd4444", got)
		}
	})

	t.Run("returns error when asset is absent", func(t *testing.T) {
		_, err := parseChecksumManifest(manifest, "not-in-manifest.tar.gz")
		if err == nil {
			t.Fatal("expected error for missing asset")
		}
	})

	t.Run("skips blank lines and comments", func(t *testing.T) {
		// If parsing broke on blank/comment lines we'd never reach the
		// matching entry below them.
		if _, err := parseChecksumManifest(manifest, "multica-cli-1.2.3-darwin-arm64.tar.gz"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestVerifyAssetSHA256(t *testing.T) {
	data := []byte("hello multica")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])

	t.Run("accepts matching sha", func(t *testing.T) {
		if err := verifyAssetSHA256(data, good, "asset.tar.gz"); err != nil {
			t.Fatalf("expected ok, got %v", err)
		}
	})

	t.Run("accepts uppercase expected hex", func(t *testing.T) {
		if err := verifyAssetSHA256(data, strings.ToUpper(good), "asset.tar.gz"); err != nil {
			t.Fatalf("expected ok with uppercase expected, got %v", err)
		}
	})

	t.Run("rejects mismatched sha", func(t *testing.T) {
		err := verifyAssetSHA256([]byte("tampered"), good, "asset.tar.gz")
		if err == nil {
			t.Fatal("expected mismatch error")
		}
		if !strings.Contains(err.Error(), "asset.tar.gz") {
			t.Fatalf("error should name the asset: %v", err)
		}
	})

	t.Run("rejects empty expected", func(t *testing.T) {
		if err := verifyAssetSHA256(data, "", "asset.tar.gz"); err == nil {
			t.Fatal("expected error for empty expected sha")
		}
	})
}

func TestUpdateDownloadTimeoutOrDefault(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{
			name:    "uses default for zero",
			timeout: 0,
			want:    DefaultUpdateDownloadTimeout,
		},
		{
			name:    "uses default for negative",
			timeout: -1 * time.Second,
			want:    DefaultUpdateDownloadTimeout,
		},
		{
			name:    "keeps explicit timeout",
			timeout: 10 * time.Minute,
			want:    10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateDownloadTimeoutOrDefault(tt.timeout)
			if got != tt.want {
				t.Fatalf("timeout = %s, want %s", got, tt.want)
			}
		})
	}
}
