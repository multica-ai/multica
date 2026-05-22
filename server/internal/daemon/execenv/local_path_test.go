package execenv

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinkLocalPaths(t *testing.T) {
	// Create a temp dir to serve as the workdir and a real dir to serve as
	// the local_path target.
	workDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "my-project")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	daemonID := "test-daemon-001"
	logger := slog.Default()

	tests := []struct {
		name      string
		daemonID  string
		resources []ProjectResourceForEnv
		wantLink  bool   // whether local/<basename> symlink should exist
		linkTarget string // expected symlink target
	}{
		{
			name:     "matching daemon_id creates symlink",
			daemonID: daemonID,
			resources: []ProjectResourceForEnv{
				{
					ID:           "res-1",
					ResourceType: "local_path",
					ResourceRef:  mustMarshal(localPathRef{Path: targetDir, DaemonID: daemonID}),
				},
			},
			wantLink:   true,
			linkTarget: targetDir,
		},
		{
			name:     "non-matching daemon_id skips symlink",
			daemonID: daemonID,
			resources: []ProjectResourceForEnv{
				{
					ID:           "res-2",
					ResourceType: "local_path",
					ResourceRef:  mustMarshal(localPathRef{Path: targetDir, DaemonID: "other-daemon"}),
				},
			},
			wantLink: false,
		},
		{
			name:     "github_repo resource is ignored",
			daemonID: daemonID,
			resources: []ProjectResourceForEnv{
				{
					ID:           "res-3",
					ResourceType: "github_repo",
					ResourceRef:  mustMarshal(map[string]string{"url": "https://github.com/foo/bar"}),
				},
			},
			wantLink: false,
		},
		{
			name:     "empty daemon_id skips all",
			daemonID: "",
			resources: []ProjectResourceForEnv{
				{
					ID:           "res-4",
					ResourceType: "local_path",
					ResourceRef:  mustMarshal(localPathRef{Path: targetDir, DaemonID: daemonID}),
				},
			},
			wantLink: false,
		},
		{
			name:     "non-existent target path skips symlink",
			daemonID: daemonID,
			resources: []ProjectResourceForEnv{
				{
					ID:           "res-5",
					ResourceType: "local_path",
					ResourceRef:  mustMarshal(localPathRef{Path: "/no/such/path/ever", DaemonID: daemonID}),
				},
			},
			wantLink: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up previous test's symlink.
			os.RemoveAll(filepath.Join(workDir, "local"))

			symlinkLocalPaths(workDir, tt.daemonID, tt.resources, logger)

			linkPath := filepath.Join(workDir, "local", filepath.Base(targetDir))
			_, err := os.Lstat(linkPath)
			if tt.wantLink {
				if err != nil {
					t.Fatalf("expected symlink at %s, got error: %v", linkPath, err)
				}
				target, err := os.Readlink(linkPath)
				if err != nil {
					t.Fatalf("readlink %s: %v", linkPath, err)
				}
				if target != tt.linkTarget {
					t.Errorf("symlink target: got %q, want %q", target, tt.linkTarget)
				}
			} else {
				if err == nil {
					t.Errorf("expected no symlink at %s, but one exists", linkPath)
				}
			}
		})
	}
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(b)
}