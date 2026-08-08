package execenv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func installSidecarRaceHooks(t *testing.T, hooks sidecarRaceTestHooks) {
	t.Helper()
	previous := sidecarRaceHooks.Swap(&hooks)
	t.Cleanup(func() { sidecarRaceHooks.Store(previous) })
}

func rootedManifestForTest(t *testing.T, workDir string) *sidecarManifest {
	t.Helper()
	manifest := &sidecarManifest{}
	if err := manifest.bindRoot(workDir); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func persistRootedManifestForTest(t *testing.T, envRoot string, manifest *sidecarManifest) {
	t.Helper()
	if err := writePlatformSidecarManifest(envRoot, manifest); err != nil {
		t.Fatal(err)
	}
	if err := manifest.closeRoot(); err != nil {
		t.Fatal(err)
	}
}

func assertIdentityExistsUnder(t *testing.T, root string, expected os.FileInfo) {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if os.SameFile(expected, info) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("replacement filesystem identity was deleted")
	}
}

func TestReusePlatformSidecarsDoesNotDeleteSkillDirSwappedAfterOwnershipCheck(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedDir := filepath.Join(workDir, ".agent_context", "skills", "managed")
	manifest := rootedManifestForTest(t, workDir)
	if err := recordMkdirAll(managedDir, 0o755, manifest); err != nil {
		t.Fatal(err)
	}
	if err := recordWriteFile(filepath.Join(managedDir, "SKILL.md"), []byte("daemon skill"), 0o600, manifest); err != nil {
		t.Fatal(err)
	}
	persistRootedManifestForTest(t, envRoot, manifest)

	var once sync.Once
	var replacementInfo os.FileInfo
	installSidecarRaceHooks(t, sidecarRaceTestHooks{
		beforeDetach: func(operation, rel string) {
			if operation != "reuse-dir" || rel != filepath.Join(".agent_context", "skills", "managed") {
				return
			}
			once.Do(func() {
				if err := os.Rename(managedDir, managedDir+"-daemon"); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(managedDir, "nested"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(managedDir, "nested", "sentinel.txt"), []byte("user replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
				replacementInfo, _ = os.Lstat(managedDir)
			})
		},
	})

	got := Reuse(ReuseParams{
		WorkspacesRoot: envRoot,
		WorkDir:        workDir,
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-race",
			PlatformAgentContext: validPlatformAgentContext("reuse-race"),
		},
	}, discardLogger())
	if got != nil {
		t.Fatalf("Reuse() = %+v, want fail-closed nil", got)
	}
	if replacementInfo == nil {
		t.Fatal("reuse race hook did not run")
	}
	assertIdentityExistsUnder(t, filepath.Join(workDir, ".agent_context", "skills"), replacementInfo)
	if data, err := os.ReadFile(filepath.Join(managedDir+"-daemon", "SKILL.md")); err != nil || string(data) != "daemon skill" {
		t.Fatalf("original managed skill changed: %q, %v", data, err)
	}
}

func TestPlatformFileCleanupAndRollbackPreserveCheckActionReplacement(t *testing.T) {
	for _, lifecycle := range []string{"cleanup", "rollback"} {
		t.Run(lifecycle, func(t *testing.T) {
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.Mkdir(workDir, 0o755); err != nil {
				t.Fatal(err)
			}
			manifest := &sidecarManifest{}
			if err := writePlatformAgentContext(workDir, validPlatformAgentContext("file-"+lifecycle), manifest); err != nil {
				t.Fatal(err)
			}
			if lifecycle == "cleanup" {
				persistRootedManifestForTest(t, envRoot, manifest)
			}

			contextPath := filepath.Join(workDir, ".platform-agent", "context.json")
			var once sync.Once
			installSidecarRaceHooks(t, sidecarRaceTestHooks{
				beforeDetach: func(operation, rel string) {
					if operation != lifecycle+"-file" || rel != filepath.Join(".platform-agent", "context.json") {
						return
					}
					once.Do(func() {
						if err := os.Rename(contextPath, contextPath+"-daemon"); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(contextPath, []byte("user replacement"), 0o600); err != nil {
							t.Fatal(err)
						}
					})
				},
			})

			var err error
			if lifecycle == "cleanup" {
				err = CleanupSidecarsAt(envRoot, workDir)
			} else {
				err = rollbackSidecarManifest(manifest)
			}
			if err == nil {
				t.Fatalf("%s error = nil after replacement race", lifecycle)
			}
			if data, readErr := os.ReadFile(contextPath); readErr != nil || string(data) != "user replacement" {
				t.Fatalf("%s deleted replacement: %q, %v", lifecycle, data, readErr)
			}
			if _, statErr := os.Lstat(contextPath + "-daemon"); statErr != nil {
				t.Fatalf("%s changed original sidecar: %v", lifecycle, statErr)
			}
		})
	}
}

func TestPlatformEmptyDirCleanupAndRollbackPreserveCheckActionReplacement(t *testing.T) {
	for _, lifecycle := range []string{"cleanup", "rollback"} {
		t.Run(lifecycle, func(t *testing.T) {
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.Mkdir(workDir, 0o755); err != nil {
				t.Fatal(err)
			}
			manifest := rootedManifestForTest(t, workDir)
			target := filepath.Join(workDir, "owned-empty")
			if err := recordMkdirAll(target, 0o700, manifest); err != nil {
				t.Fatal(err)
			}
			if lifecycle == "cleanup" {
				persistRootedManifestForTest(t, envRoot, manifest)
			}

			var once sync.Once
			var replacementInfo os.FileInfo
			installSidecarRaceHooks(t, sidecarRaceTestHooks{
				beforeDetach: func(operation, rel string) {
					if operation != lifecycle+"-dir" || rel != "owned-empty" {
						return
					}
					once.Do(func() {
						if err := os.Rename(target, target+"-daemon"); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(target, 0o711); err != nil {
							t.Fatal(err)
						}
						replacementInfo, _ = os.Lstat(target)
					})
				},
			})

			var err error
			if lifecycle == "cleanup" {
				err = CleanupSidecarsAt(envRoot, workDir)
			} else {
				err = rollbackSidecarManifest(manifest)
			}
			if err == nil {
				t.Fatalf("%s error = nil after empty-directory replacement race", lifecycle)
			}
			if replacementInfo == nil {
				t.Fatal("directory race hook did not run")
			}
			assertIdentityExistsUnder(t, workDir, replacementInfo)
			if _, statErr := os.Lstat(target + "-daemon"); statErr != nil {
				t.Fatalf("%s changed original sidecar directory: %v", lifecycle, statErr)
			}
		})
	}
}

func TestPlatformManifestPublishPreservesReplacementInstalledAfterLink(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := rootedManifestForTest(t, workDir)
	defer manifest.closeRoot()

	manifestPath := filepath.Join(envRoot, sidecarManifestFile)
	var once sync.Once
	installSidecarRaceHooks(t, sidecarRaceTestHooks{
		afterLink: func(operation, rel string) {
			if operation != "publish-manifest" || rel != sidecarManifestFile {
				return
			}
			once.Do(func() {
				if err := os.Rename(manifestPath, manifestPath+"-daemon"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifestPath, []byte("user replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			})
		},
	})

	if err := writePlatformSidecarManifest(envRoot, manifest); err == nil {
		t.Fatal("writePlatformSidecarManifest() error = nil after manifest replacement")
	}
	if data, err := os.ReadFile(manifestPath); err != nil || string(data) != "user replacement" {
		t.Fatalf("manifest publisher deleted replacement: %q, %v", data, err)
	}
}

func TestPlatformMarkerRefreshPreservesReplacementInstalledBeforeDetach(t *testing.T) {
	workDir := t.TempDir()
	markerPath := filepath.Join(workDir, TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte(`{"managed_by":"multica-daemon-task","agent_id":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := rootedManifestForTest(t, workDir)
	defer manifest.closeRoot()

	var once sync.Once
	installSidecarRaceHooks(t, sidecarRaceTestHooks{
		beforeDetach: func(operation, rel string) {
			if operation != "replace-file" || rel != TaskContextMarkerRelPath {
				return
			}
			once.Do(func() {
				if err := os.Rename(markerPath, markerPath+"-daemon"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(markerPath, []byte("user replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			})
		},
	})

	err := writeTaskContextMarker(workDir, TaskContextForEnv{AgentID: "new"}, manifest)
	if err == nil {
		t.Fatal("writeTaskContextMarker() error = nil after marker replacement")
	}
	if data, readErr := os.ReadFile(markerPath); readErr != nil || string(data) != "user replacement" {
		t.Fatalf("marker refresh deleted replacement: %q, %v", data, readErr)
	}
}

func TestPlatformMarkerRollbackPreservesReplacementInstalledBeforeRestore(t *testing.T) {
	workDir := t.TempDir()
	markerPath := filepath.Join(workDir, TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte(`{"managed_by":"multica-daemon-task","agent_id":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := rootedManifestForTest(t, workDir)
	if err := writeTaskContextMarker(workDir, TaskContextForEnv{AgentID: "new"}, manifest); err != nil {
		t.Fatal(err)
	}

	var once sync.Once
	installSidecarRaceHooks(t, sidecarRaceTestHooks{
		beforeDetach: func(operation, rel string) {
			if operation != "restore-file" || rel != TaskContextMarkerRelPath {
				return
			}
			once.Do(func() {
				if err := os.Rename(markerPath, markerPath+"-daemon"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(markerPath, []byte("user replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			})
		},
	})

	if err := rollbackSidecarManifest(manifest); err == nil {
		t.Fatal("rollbackSidecarManifest() error = nil after marker replacement")
	}
	if data, err := os.ReadFile(markerPath); err != nil || string(data) != "user replacement" {
		t.Fatalf("marker rollback deleted replacement: %q, %v", data, err)
	}
}

func TestPlatformContextPublishPreservesReplacementInstalledAfterLink(t *testing.T) {
	workDir := t.TempDir()
	contextPath := filepath.Join(workDir, ".platform-agent", "context.json")
	var once sync.Once
	installSidecarRaceHooks(t, sidecarRaceTestHooks{
		afterLink: func(operation, rel string) {
			if operation != "publish-owned-file" || rel != filepath.Join(".platform-agent", "context.json") {
				return
			}
			once.Do(func() {
				if err := os.Rename(contextPath, contextPath+"-daemon"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(contextPath, []byte("user replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			})
		},
	})

	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("link-race"), manifest); err == nil {
		t.Fatal("writePlatformAgentContext() error = nil after link replacement")
	}
	defer manifest.closeRoot()
	if data, err := os.ReadFile(contextPath); err != nil || string(data) != "user replacement" {
		t.Fatalf("context publisher deleted replacement: %q, %v", data, err)
	}
}

func TestRootedRecordWriteFileDoesNotRecordReplacementInstalledAfterCreate(t *testing.T) {
	workDir := t.TempDir()
	manifest := rootedManifestForTest(t, workDir)
	defer manifest.closeRoot()
	target := filepath.Join(workDir, "owned.txt")

	var once sync.Once
	installSidecarRaceHooks(t, sidecarRaceTestHooks{
		beforeRecord: func(operation, rel string) {
			if operation != "record-owned-file" || rel != "owned.txt" {
				return
			}
			once.Do(func() {
				if err := os.Rename(target, target+"-daemon"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte("user replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			})
		},
	})

	err := recordWriteFile(target, []byte("daemon file"), 0o600, manifest)
	if err == nil {
		t.Fatal("recordWriteFile() error = nil after final-path replacement")
	}
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != "user replacement" {
		t.Fatalf("recordWriteFile deleted replacement: %q, %v", data, readErr)
	}
	for _, path := range manifest.Files {
		if path == "owned.txt" {
			t.Fatal("recordWriteFile recorded a replacement it does not own")
		}
	}
}

func TestPlatformOwnershipMismatchLeavesRetryManifest(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := rootedManifestForTest(t, workDir)
	if err := recordWriteFile(filepath.Join(workDir, "owned.txt"), []byte("daemon"), 0o600, manifest); err != nil {
		t.Fatal(err)
	}
	persistRootedManifestForTest(t, envRoot, manifest)
	if err := os.WriteFile(filepath.Join(workDir, "owned.txt"), []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := CleanupSidecarsAt(envRoot, workDir)
	if err == nil {
		t.Fatal("CleanupSidecarsAt() error = nil for ownership mismatch")
	}
	if !errors.Is(err, errSidecarOwnershipMismatch) {
		t.Fatalf("CleanupSidecarsAt() error = %v, want ownership mismatch", err)
	}
	if _, statErr := os.Lstat(filepath.Join(envRoot, sidecarManifestFile)); statErr != nil {
		t.Fatalf("ownership mismatch removed retry manifest: %v", statErr)
	}
}
