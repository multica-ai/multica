package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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

func TestFindManifestAsset(t *testing.T) {
	manifest := &UpdateManifest{
		Version: "v1.2.3",
		Assets: []UpdateManifestAsset{
			{OS: "linux", Arch: "amd64", ArchiveName: "multica_linux_amd64.tar.gz", URL: "old"},
			{OS: "linux", Arch: "amd64", ArchiveName: "multica-cli-1.2.3-linux-amd64.tar.gz", URL: "new"},
			{OS: "darwin", Arch: "arm64", URL: "darwin"},
		},
	}

	got, err := findManifestAsset(manifest, "linux", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL != "new" {
		t.Fatalf("asset mismatch: got %q", got.URL)
	}

	got, err = findManifestAsset(manifest, "darwin", "arm64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL != "darwin" {
		t.Fatalf("asset mismatch: got %q", got.URL)
	}
}

func TestShouldUpdate(t *testing.T) {
	manifest := &UpdateManifest{Version: "v1.3.0"}

	should, err := ShouldUpdate("v1.2.9", manifest)
	if err != nil {
		t.Fatalf("ShouldUpdate returned error: %v", err)
	}
	if !should {
		t.Fatal("expected update to be required")
	}

	should, err = ShouldUpdate("v1.3.0", manifest)
	if err != nil {
		t.Fatalf("ShouldUpdate returned error: %v", err)
	}
	if should {
		t.Fatal("expected update to be skipped for equal version")
	}
}

func TestFetchUpdateManifestFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept header = %q", r.Header.Get("Accept"))
		}
		fmt.Fprint(w, `{"version":"v1.2.3","published_at":"2026-05-09T00:00:00Z","assets":[]}`)
	}))
	t.Cleanup(srv.Close)

	manifest, err := FetchUpdateManifestFromURL(srv.URL)
	if err != nil {
		t.Fatalf("FetchUpdateManifestFromURL() error = %v", err)
	}
	if manifest.Version != "v1.2.3" {
		t.Fatalf("Version = %q, want v1.2.3", manifest.Version)
	}
}

func TestFetchUpdateManifestFromURLRequiresVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"assets":[]}`)
	}))
	t.Cleanup(srv.Close)

	_, err := FetchUpdateManifestFromURL(srv.URL)
	if err == nil {
		t.Fatal("expected error for manifest without version")
	}
}

func TestResolveInstalledBinaryPathPrefersManagedInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	managed, err := resolveManagedInstallPath()
	if err != nil {
		t.Fatalf("resolveManagedInstallPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(managed, []byte("bin"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := ResolveInstalledBinaryPath()
	if err != nil {
		t.Fatalf("ResolveInstalledBinaryPath() error = %v", err)
	}
	if got != managed {
		t.Fatalf("ResolveInstalledBinaryPath() = %q, want %q", got, managed)
	}
}
