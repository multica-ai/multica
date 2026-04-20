package cli

import "testing"

func TestReleaseAssetCandidates(t *testing.T) {
	tests := []struct {
		name          string
		targetVersion string
		goos          string
		goarch        string
		wantAssets    []string
	}{
		{
			name:          "darwin returns legacy then renamed candidate",
			targetVersion: "v1.2.3",
			goos:          "darwin",
			goarch:        "arm64",
			wantAssets: []string{
				"multica_darwin_arm64.tar.gz",
				"multica-cli-1.2.3-darwin-arm64.tar.gz",
			},
		},
		{
			name:          "linux adds missing v only to renamed fallback",
			targetVersion: "1.2.3",
			goos:          "linux",
			goarch:        "amd64",
			wantAssets: []string{
				"multica_linux_amd64.tar.gz",
				"multica-cli-1.2.3-linux-amd64.tar.gz",
			},
		},
		{
			name:          "windows uses zip assets",
			targetVersion: "1.2.3",
			goos:          "windows",
			goarch:        "amd64",
			wantAssets: []string{
				"multica_windows_amd64.zip",
				"multica-cli-1.2.3-windows-amd64.zip",
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
	t.Run("prefers legacy asset when both names exist", func(t *testing.T) {
		assets := []GitHubReleaseAsset{
			{Name: "multica-cli-1.2.3-darwin-amd64.tar.gz", BrowserDownloadURL: "new"},
			{Name: "multica_darwin_amd64.tar.gz", BrowserDownloadURL: "old"},
		}

		got, err := findReleaseAsset(assets, "v1.2.3", "darwin", "amd64")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "multica_darwin_amd64.tar.gz" {
			t.Fatalf("asset mismatch: got %q", got.Name)
		}
	})

	t.Run("falls back to renamed asset when legacy is absent", func(t *testing.T) {
		assets := []GitHubReleaseAsset{
			{Name: "multica-cli-1.2.3-linux-amd64.tar.gz", BrowserDownloadURL: "new"},
		}

		got, err := findReleaseAsset(assets, "1.2.3", "linux", "amd64")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "multica-cli-1.2.3-linux-amd64.tar.gz" {
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
