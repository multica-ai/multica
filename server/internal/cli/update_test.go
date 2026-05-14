package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

func TestExtractTarGzToDirAndInstallManagedArchive(t *testing.T) {
	stage := t.TempDir()
	archive := buildTarGzArchive(t, map[string]string{
		"multica":      "binary-v2",
		"package.json": `{"name":"multica-claude-sdk-runtime"}`,
		"node_modules/@anthropic-ai/claude-agent-sdk/sdk.mjs": "export {};",
	})
	if err := extractTarGzToDir(bytes.NewReader(archive), stage); err != nil {
		t.Fatalf("extractTarGzToDir() error = %v", err)
	}

	installDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(installDir, "multica"), []byte("binary-v1"), 0o755); err != nil {
		t.Fatalf("WriteFile() old binary error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "node_modules", "old"), 0o755); err != nil {
		t.Fatalf("MkdirAll() old node_modules error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "package.json"), []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile() old package error = %v", err)
	}

	if err := installManagedArchive(stage, installDir, "multica"); err != nil {
		t.Fatalf("installManagedArchive() error = %v", err)
	}

	gotBinary, err := os.ReadFile(filepath.Join(installDir, "multica"))
	if err != nil {
		t.Fatalf("ReadFile() binary error = %v", err)
	}
	if string(gotBinary) != "binary-v2" {
		t.Fatalf("binary contents = %q, want %q", string(gotBinary), "binary-v2")
	}
	if _, err := os.Stat(filepath.Join(installDir, "node_modules", "@anthropic-ai", "claude-agent-sdk", "sdk.mjs")); err != nil {
		t.Fatalf("expected sdk bundle file to be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "node_modules", "old")); !os.IsNotExist(err) {
		t.Fatalf("expected old node_modules to be replaced, stat err = %v", err)
	}
}

func buildTarGzArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if name == "multica" {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%q): %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%q): %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}
