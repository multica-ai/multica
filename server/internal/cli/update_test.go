package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

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

func TestUpdateAPIBase(t *testing.T) {
	t.Run("defaults to GitHub API", func(t *testing.T) {
		t.Setenv(updateAPIBaseEnv, "")
		if got := updateAPIBase(); got != defaultUpdateAPIBase {
			t.Fatalf("updateAPIBase() = %q, want %q", got, defaultUpdateAPIBase)
		}
	})

	t.Run("uses environment override", func(t *testing.T) {
		t.Setenv(updateAPIBaseEnv, "http://localhost:9876")
		if got := updateAPIBase(); got != "http://localhost:9876" {
			t.Fatalf("updateAPIBase() = %q", got)
		}
	})

	t.Run("trims whitespace and trailing slashes", func(t *testing.T) {
		t.Setenv(updateAPIBaseEnv, "  http://localhost:9876///  ")
		if got := updateAPIBase(); got != "http://localhost:9876" {
			t.Fatalf("updateAPIBase() = %q", got)
		}
	})

	t.Run("whitespace falls back to default", func(t *testing.T) {
		t.Setenv(updateAPIBaseEnv, "   ")
		if got := updateAPIBase(); got != defaultUpdateAPIBase {
			t.Fatalf("updateAPIBase() = %q, want %q", got, defaultUpdateAPIBase)
		}
	})
}

func TestUpdateReleaseURL(t *testing.T) {
	t.Run("builds default GitHub release tag URL", func(t *testing.T) {
		t.Setenv(updateAPIBaseEnv, "")
		want := "https://api.github.com/repos/multica-ai/multica/releases/tags/v1.2.3"
		if got := updateReleaseURL("releases/tags/v1.2.3"); got != want {
			t.Fatalf("updateReleaseURL() = %q, want %q", got, want)
		}
	})

	t.Run("builds relay latest release URL", func(t *testing.T) {
		t.Setenv(updateAPIBaseEnv, "http://localhost:9876/")
		want := "http://localhost:9876/repos/multica-ai/multica/releases/latest"
		if got := updateReleaseURL("/releases/latest"); got != want {
			t.Fatalf("updateReleaseURL() = %q, want %q", got, want)
		}
	})
}

func TestRewriteAssetDownloadURL(t *testing.T) {
	original := "https://github.com/multica-ai/multica/releases/download/v1.2.3/multica-cli-1.2.3-linux-amd64.tar.gz"

	t.Run("passes through without override", func(t *testing.T) {
		t.Setenv(updateDownloadBaseEnv, "")
		if got := rewriteAssetDownloadURL(original); got != original {
			t.Fatalf("rewriteAssetDownloadURL() = %q, want passthrough", got)
		}
	})

	t.Run("rewrites to relay asset base", func(t *testing.T) {
		t.Setenv(updateDownloadBaseEnv, "http://localhost:9876/assets")
		want := "http://localhost:9876/assets/multica-cli-1.2.3-linux-amd64.tar.gz"
		if got := rewriteAssetDownloadURL(original); got != want {
			t.Fatalf("rewriteAssetDownloadURL() = %q, want %q", got, want)
		}
	})

	t.Run("trims trailing slash from relay base", func(t *testing.T) {
		t.Setenv(updateDownloadBaseEnv, "http://localhost:9876/assets/")
		want := "http://localhost:9876/assets/multica-cli-1.2.3-linux-amd64.tar.gz"
		if got := rewriteAssetDownloadURL(original); got != want {
			t.Fatalf("rewriteAssetDownloadURL() = %q, want %q", got, want)
		}
	})

	t.Run("whitespace override passes through", func(t *testing.T) {
		t.Setenv(updateDownloadBaseEnv, "   ")
		if got := rewriteAssetDownloadURL(original); got != original {
			t.Fatalf("rewriteAssetDownloadURL() = %q, want passthrough", got)
		}
	})

	t.Run("empty filename passes through", func(t *testing.T) {
		t.Setenv(updateDownloadBaseEnv, "http://localhost:9876/assets")
		raw := "https://github.com/"
		if got := rewriteAssetDownloadURL(raw); got != raw {
			t.Fatalf("rewriteAssetDownloadURL() = %q, want passthrough", got)
		}
	})
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
