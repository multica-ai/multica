package execenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestPreparePlatformAgentWritesValidatedPrivateContext(t *testing.T) {
	ctx := validPlatformAgentContext("lead-researcher")
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-1",
		AgentName:      "Lead Researcher",
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-1",
			PlatformAgentContext: ctx,
		},
	}, discardLogger())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	path := filepath.Join(env.WorkDir, ".platform-agent", "context.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read context: %v", err)
	}
	want := `{
  "schema_version": "platform-agent.runtime-context/v1",
  "extension": {
    "key": "research-team",
    "version": "1.0.0",
    "release_id": "release-1",
    "digest": "sha256:abc"
  },
  "agent": {
    "source_key": "lead-researcher"
  },
  "commands": [
    {
      "name": "summarize",
      "description": "Summary command.",
      "content": "Summarize findings.",
      "metadata": {
        "owner": "platform"
      }
    }
  ]
}`
	if string(data) != want {
		t.Fatalf("context.json =\n%s\nwant:\n%s", data, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("context mode = %o, want 600", got)
		}
	}
	if info, err := os.Stat(filepath.Join(env.WorkDir, ".agent_context", "skills")); err != nil || !info.IsDir() {
		t.Fatalf("platform skills root must exist even when empty: info=%v err=%v", info, err)
	}
}

func TestPrepareOtherProviderDoesNotWritePlatformContext(t *testing.T) {
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-other",
		AgentName:      "Other",
		Provider:       "claude",
		Task: TaskContextForEnv{
			AgentID:              "agent-other",
			PlatformAgentContext: validPlatformAgentContext("must-not-leak"),
		},
	}, discardLogger())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(env.WorkDir, ".platform-agent")); !os.IsNotExist(err) {
		t.Fatalf("non-platform provider created .platform-agent: %v", err)
	}
}

func TestPreparePlatformAgentMissingContextFailsClosed(t *testing.T) {
	root := t.TempDir()
	_, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "workspace-1",
		TaskID:         "task-missing",
		AgentName:      "Missing",
		Provider:       "platform-agent-cli",
		Task:           TaskContextForEnv{AgentID: "agent-missing"},
	}, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "platform agent context") {
		t.Fatalf("Prepare() error = %v, want platform context failure", err)
	}
}

func TestWritePlatformAgentContextRejectsNullCommands(t *testing.T) {
	ctx := validPlatformAgentContext("null-commands")
	ctx.Commands = nil
	workDir := t.TempDir()
	err := writePlatformAgentContext(workDir, ctx, &sidecarManifest{})
	if err == nil || !strings.Contains(err.Error(), "commands") {
		t.Fatalf("writePlatformAgentContext() error = %v, want commands error", err)
	}
	if _, statErr := os.Lstat(filepath.Join(workDir, ".platform-agent", "context.json")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid context was materialized: %v", statErr)
	}
}

func TestPreparePlatformAgentContextCollisionPreservesUserPath(t *testing.T) {
	workDir := t.TempDir()
	contextDir := filepath.Join(workDir, ".platform-agent")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(contextDir, "context.json")
	const userData = "user-owned-context"
	if err := os.WriteFile(path, []byte(userData), 0o640); err != nil {
		t.Fatal(err)
	}

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-collision",
		AgentName:      "Collision",
		Provider:       "platform-agent-cli",
		LocalWorkDir:   workDir,
		Task: TaskContextForEnv{
			AgentID:              "agent-collision",
			PlatformAgentContext: validPlatformAgentContext("lead"),
		},
	}, discardLogger())
	if err == nil || !errors.Is(err, errPathPreExists) {
		t.Fatalf("Prepare() error = %v, want errPathPreExists", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != userData {
		t.Fatalf("collision changed user bytes to %q", data)
	}
}

func TestPreparePlatformAgentRejectsPreexistingPathTypeMatrix(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, workDir string) func(t *testing.T)
	}{
		{
			name: "context regular file",
			setup: func(t *testing.T, workDir string) func(t *testing.T) {
				path := filepath.Join(workDir, ".platform-agent", "context.json")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("user-file"), 0o600); err != nil {
					t.Fatal(err)
				}
				return func(t *testing.T) {
					data, err := os.ReadFile(path)
					if err != nil || string(data) != "user-file" {
						t.Fatalf("regular file changed: %q, %v", data, err)
					}
				}
			},
		},
		{
			name: "context directory",
			setup: func(t *testing.T, workDir string) func(t *testing.T) {
				path := filepath.Join(workDir, ".platform-agent", "context.json")
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
				sentinel := filepath.Join(path, "user.txt")
				if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
					t.Fatal(err)
				}
				return func(t *testing.T) {
					if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserve" {
						t.Fatalf("context directory changed: %q, %v", data, err)
					}
				}
			},
		},
		{
			name: "context leaf symlink",
			setup: func(t *testing.T, workDir string) func(t *testing.T) {
				parent := filepath.Join(workDir, ".platform-agent")
				if err := os.MkdirAll(parent, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside.json")
				if err := os.WriteFile(outside, []byte("outside-leaf"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(parent, "context.json")); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
				return func(t *testing.T) {
					if data, err := os.ReadFile(outside); err != nil || string(data) != "outside-leaf" {
						t.Fatalf("outside leaf changed: %q, %v", data, err)
					}
				}
			},
		},
		{
			name: "platform parent file",
			setup: func(t *testing.T, workDir string) func(t *testing.T) {
				path := filepath.Join(workDir, ".platform-agent")
				if err := os.WriteFile(path, []byte("parent-file"), 0o600); err != nil {
					t.Fatal(err)
				}
				return func(t *testing.T) {
					if data, err := os.ReadFile(path); err != nil || string(data) != "parent-file" {
						t.Fatalf("parent file changed: %q, %v", data, err)
					}
				}
			},
		},
		{
			name: "platform parent symlink",
			setup: func(t *testing.T, workDir string) func(t *testing.T) {
				outside := t.TempDir()
				outsideContext := filepath.Join(outside, "context.json")
				if err := os.WriteFile(outsideContext, []byte("outside-parent"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(workDir, ".platform-agent")); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
				return func(t *testing.T) {
					if data, err := os.ReadFile(outsideContext); err != nil || string(data) != "outside-parent" {
						t.Fatalf("outside parent changed: %q, %v", data, err)
					}
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			verify := test.setup(t, workDir)
			_, err := Prepare(PrepareParams{
				WorkspacesRoot: t.TempDir(),
				WorkspaceID:    "workspace-preexisting",
				TaskID:         "task-" + strings.ReplaceAll(test.name, " ", "-"),
				AgentName:      "Preexisting",
				Provider:       "platform-agent-cli",
				LocalWorkDir:   workDir,
				Task: TaskContextForEnv{
					AgentID:              "agent-preexisting",
					PlatformAgentContext: validPlatformAgentContext("preexisting"),
				},
			}, discardLogger())
			if err == nil {
				t.Fatal("Prepare() error = nil, want preexisting path rejection")
			}
			verify(t)
		})
	}
}

func TestPreparePlatformAgentRollsBackContextWhenLaterMarkerFails(t *testing.T) {
	workDir := t.TempDir()
	markerPath := filepath.Join(workDir, TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const userMarker = `{"managed_by":"user"}`
	if err := os.WriteFile(markerPath, []byte(userMarker), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-rollback",
		TaskID:         "task-marker-collision",
		AgentName:      "Rollback",
		Provider:       "platform-agent-cli",
		LocalWorkDir:   workDir,
		Task: TaskContextForEnv{
			AgentID:              "agent-rollback",
			PlatformAgentContext: validPlatformAgentContext("rollback"),
		},
	}, discardLogger())
	if err == nil || !errors.Is(err, errPathPreExists) {
		t.Fatalf("Prepare() error = %v, want marker collision", err)
	}
	if _, statErr := os.Lstat(filepath.Join(workDir, ".platform-agent")); !os.IsNotExist(statErr) {
		t.Fatalf("failed Prepare left platform sidecars behind: %v", statErr)
	}
	data, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != userMarker {
		t.Fatalf("user marker changed to %q", data)
	}
}

func TestFinalizePlatformSidecarsRollsBackWhenManifestPersistenceFails(t *testing.T) {
	for _, provider := range []string{"platform-agent-cli", "claude"} {
		t.Run(provider, func(t *testing.T) {
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				t.Fatal(err)
			}
			manifest := &sidecarManifest{}
			ownedPath := filepath.Join(workDir, "owned.txt")
			if provider == "platform-agent-cli" {
				if err := writePlatformAgentContext(workDir, validPlatformAgentContext("manifest-failure"), manifest); err != nil {
					t.Fatal(err)
				}
				ownedPath = filepath.Join(workDir, ".platform-agent", "context.json")
			} else if err := recordWriteFile(ownedPath, []byte("legacy"), 0o600, manifest); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(envRoot, sidecarManifestFile), 0o755); err != nil {
				t.Fatal(err)
			}

			err := finalizeSidecarManifest(provider, envRoot, workDir, manifest, discardLogger())
			if provider == "platform-agent-cli" {
				if err == nil {
					t.Fatal("finalizeSidecarManifest() error = nil, want persistence failure")
				}
				if _, statErr := os.Lstat(ownedPath); !os.IsNotExist(statErr) {
					t.Fatalf("platform rollback left %s: %v", ownedPath, statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("non-platform finalize error = %v, want warning-only behavior", err)
			}
			if _, statErr := os.Stat(ownedPath); statErr != nil {
				t.Fatalf("non-platform sidecar was rolled back: %v", statErr)
			}
		})
	}
}

func TestFinalizePlatformSidecarsRejectsManifestSymlinkWithoutClobberingTarget(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("manifest-symlink"), manifest); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "user.json")
	const outsideData = "outside-user-manifest"
	if err := os.WriteFile(outside, []byte(outsideData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(envRoot, sidecarManifestFile)); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	if err := finalizeSidecarManifest("platform-agent-cli", envRoot, workDir, manifest, discardLogger()); err == nil {
		t.Fatal("finalizeSidecarManifest() error = nil for manifest symlink")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != outsideData {
		t.Fatalf("manifest persistence changed outside target: %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(workDir, ".platform-agent")); !os.IsNotExist(err) {
		t.Fatalf("failed finalize left untracked platform sidecars: %v", err)
	}
}

func TestCleanupPlatformSidecarsRejectsManifestSymlinkWithoutFollowingTarget(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	contextPath := filepath.Join(workDir, ".platform-agent", "context.json")
	if err := os.MkdirAll(filepath.Dir(contextPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contextPath, []byte("daemon-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideManifest := filepath.Join(t.TempDir(), "user.json")
	manifestData, err := json.Marshal(&sidecarManifest{
		Rooted: true,
		Files:  []string{filepath.Join(".platform-agent", "context.json")},
		Dirs:   []string{".platform-agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideManifest, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideManifest, filepath.Join(envRoot, sidecarManifestFile)); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	if err := CleanupSidecarsAt(envRoot, workDir); err == nil {
		t.Fatal("CleanupSidecarsAt() error = nil for manifest symlink")
	}
	if data, err := os.ReadFile(contextPath); err != nil || string(data) != "daemon-owned" {
		t.Fatalf("cleanup followed symlink manifest and changed sidecar: %q, %v", data, err)
	}
	if data, err := os.ReadFile(outsideManifest); err != nil || string(data) != string(manifestData) {
		t.Fatalf("cleanup changed outside manifest target: %q, %v", data, err)
	}
}

func TestPreparePlatformAgentRestoresRefreshedMarkerWhenLaterWriteFails(t *testing.T) {
	workDir := t.TempDir()
	markerPath := filepath.Join(workDir, TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const originalMarker = "{\n  \"managed_by\": \"multica-daemon-task\",\n  \"agent_id\": \"prior-agent\"\n}\n"
	if err := os.WriteFile(markerPath, []byte(originalMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".agent_context"), []byte("collision"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-marker-restore",
		TaskID:         "task-marker-restore",
		AgentName:      "Marker Restore",
		Provider:       "platform-agent-cli",
		LocalWorkDir:   workDir,
		Task: TaskContextForEnv{
			AgentID:              "new-agent",
			PlatformAgentContext: validPlatformAgentContext("marker-restore"),
		},
	}, discardLogger())
	if err == nil {
		t.Fatal("Prepare() error = nil after forced post-marker failure")
	}
	data, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatalf("rollback removed pre-existing daemon marker: %v", readErr)
	}
	if string(data) != originalMarker {
		t.Fatalf("rollback restored marker bytes %q, want %q", data, originalMarker)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(markerPath)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("rollback marker mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestReusePlatformAgentReplacesDaemonOwnedContext(t *testing.T) {
	root := t.TempDir()
	first, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "workspace-1",
		TaskID:         "task-reuse",
		AgentName:      "First",
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-1",
			PlatformAgentContext: validPlatformAgentContext("first-agent"),
		},
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}

	reused := Reuse(ReuseParams{
		WorkspacesRoot: root,
		WorkDir:        first.WorkDir,
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-2",
			PlatformAgentContext: validPlatformAgentContext("second-agent"),
		},
	}, discardLogger())
	if reused == nil {
		t.Fatal("Reuse() = nil, want refreshed environment")
	}
	data, err := os.ReadFile(filepath.Join(reused.WorkDir, ".platform-agent", "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "first-agent") || !strings.Contains(string(data), "second-agent") {
		t.Fatalf("reused context not replaced: %s", data)
	}
}

func TestReusePlatformAgentInvalidContextFailsClosed(t *testing.T) {
	root := t.TempDir()
	first, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "workspace-1",
		TaskID:         "task-reuse-invalid",
		AgentName:      "First",
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-1",
			PlatformAgentContext: validPlatformAgentContext("first-agent"),
		},
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	invalid := validPlatformAgentContext("second-agent")
	invalid.SchemaVersion = "wrong/v1"
	if reused := Reuse(ReuseParams{
		WorkspacesRoot: root,
		WorkDir:        first.WorkDir,
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-2",
			PlatformAgentContext: invalid,
		},
	}, discardLogger()); reused != nil {
		t.Fatalf("Reuse() = %+v, want nil for invalid context", reused)
	}
}

func TestReusePlatformAgentFailsClosedOnEverySidecarLifecycleError(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, envRoot, workDir string)
	}{
		{
			name: "managed skill rollback",
			setup: func(t *testing.T, envRoot, _ string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(envRoot, sidecarManifestFile), []byte(`{"dirs":`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "sidecar cleanup",
			setup: func(t *testing.T, envRoot, workDir string) {
				t.Helper()
				blocked := filepath.Join(workDir, "non-empty-recorded-as-file")
				if err := os.MkdirAll(blocked, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(blocked, "user.txt"), []byte("preserve"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := writeSidecarManifest(envRoot, &sidecarManifest{Files: []string{blocked}}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "manifest persistence",
			setup: func(t *testing.T, envRoot, _ string) {
				t.Helper()
				target := filepath.Join(envRoot, "missing-parent", "manifest.json")
				if err := os.Symlink(target, filepath.Join(envRoot, sidecarManifestFile)); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, provider := range []string{"platform-agent-cli", "claude"} {
				t.Run(provider, func(t *testing.T) {
					envRoot := t.TempDir()
					workDir := filepath.Join(envRoot, "workdir")
					if err := os.MkdirAll(workDir, 0o755); err != nil {
						t.Fatal(err)
					}
					test.setup(t, envRoot, workDir)
					task := TaskContextForEnv{AgentID: "agent-reuse-lifecycle"}
					if provider == "platform-agent-cli" {
						task.PlatformAgentContext = validPlatformAgentContext("reuse-lifecycle")
					}
					got := Reuse(ReuseParams{
						WorkspacesRoot: envRoot,
						WorkDir:        workDir,
						Provider:       provider,
						Task:           task,
					}, discardLogger())
					if provider == "platform-agent-cli" && got != nil {
						t.Fatalf("Reuse() = %+v, want nil on %s failure", got, test.name)
					}
					if provider == "platform-agent-cli" {
						if _, statErr := os.Lstat(filepath.Join(workDir, ".platform-agent", "context.json")); !os.IsNotExist(statErr) {
							t.Fatalf("failed-closed Reuse left platform context behind: %v", statErr)
						}
					}
					if provider != "platform-agent-cli" && got == nil {
						t.Fatalf("non-platform Reuse() = nil; existing warning-only behavior changed for %s", test.name)
					}
				})
			}
		})
	}
}

func TestCleanupSidecarsRemovesPlatformContextAndPreservesExistingParent(t *testing.T) {
	workDir := t.TempDir()
	contextDir := filepath.Join(workDir, ".platform-agent")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-cleanup",
		AgentName:      "Cleanup",
		Provider:       "platform-agent-cli",
		LocalWorkDir:   workDir,
		Task: TaskContextForEnv{
			AgentID:              "agent-cleanup",
			PlatformAgentContext: validPlatformAgentContext("lead"),
		},
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := CleanupSidecarsAt(env.RootDir, env.WorkDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(contextDir, "context.json")); !os.IsNotExist(err) {
		t.Fatalf("context sidecar survived cleanup: %v", err)
	}
	if info, err := os.Stat(contextDir); err != nil || !info.IsDir() {
		t.Fatalf("pre-existing parent was removed: info=%v err=%v", info, err)
	}
}

func TestCleanupPlatformSidecarsDoesNotFollowSwappedParentOutsideWorkdir(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("cleanup-swap"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatal(err)
	}
	if err := manifest.closeRoot(); err != nil {
		t.Fatal(err)
	}

	platformDir := filepath.Join(workDir, ".platform-agent")
	originalDir := filepath.Join(workDir, ".platform-agent-owned")
	if err := os.Rename(platformDir, originalDir); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideContext := filepath.Join(outside, "context.json")
	const outsideData = "outside-user-context"
	if err := os.WriteFile(outsideContext, []byte(outsideData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, platformDir); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	err := CleanupSidecarsAt(envRoot, workDir)
	if err == nil {
		t.Fatal("CleanupSidecarsAt() error = nil after parent swap")
	}
	if _, statErr := os.Stat(filepath.Join(envRoot, sidecarManifestFile)); statErr != nil {
		t.Fatalf("failed cleanup removed its retry manifest: %v", statErr)
	}
	data, readErr := os.ReadFile(outsideContext)
	if readErr != nil {
		t.Fatalf("outside context was deleted: %v", readErr)
	}
	if string(data) != outsideData {
		t.Fatalf("outside context changed to %q", data)
	}
	if _, statErr := os.Stat(filepath.Join(originalDir, "context.json")); statErr != nil {
		t.Fatalf("original daemon sidecar disappeared despite failed cleanup: %v", statErr)
	}
}

func TestCleanupPlatformSidecarsDoesNotFollowSwappedParentInsideWorkdir(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("cleanup-internal-swap"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatal(err)
	}
	if err := manifest.closeRoot(); err != nil {
		t.Fatal(err)
	}

	platformDir := filepath.Join(workDir, ".platform-agent")
	originalDir := filepath.Join(workDir, ".platform-agent-owned")
	if err := os.Rename(platformDir, originalDir); err != nil {
		t.Fatal(err)
	}
	decoyDir := filepath.Join(workDir, "decoy")
	if err := os.Mkdir(decoyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decoyContext := filepath.Join(decoyDir, "context.json")
	if err := os.WriteFile(decoyContext, []byte("user-decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("decoy", platformDir); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	if err := CleanupSidecarsAt(envRoot, workDir); err == nil {
		t.Fatal("CleanupSidecarsAt() error = nil after internal parent swap")
	}
	if data, err := os.ReadFile(decoyContext); err != nil || string(data) != "user-decoy" {
		t.Fatalf("cleanup followed internal parent swap: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(originalDir, "context.json")); err != nil {
		t.Fatalf("cleanup mistook swapped parent for original sidecar: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(envRoot, sidecarManifestFile)); err != nil {
		t.Fatalf("failed cleanup removed retry manifest: %v", err)
	}
}

func TestCleanupPlatformSidecarsRejectsSwappedLeafSymlink(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("cleanup-leaf-swap"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatal(err)
	}
	if err := manifest.closeRoot(); err != nil {
		t.Fatal(err)
	}

	contextPath := filepath.Join(workDir, ".platform-agent", "context.json")
	originalPath := filepath.Join(workDir, ".platform-agent", "context-owned.json")
	if err := os.Rename(contextPath, originalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("context-owned.json", contextPath); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	if err := CleanupSidecarsAt(envRoot, workDir); err == nil {
		t.Fatal("CleanupSidecarsAt() error = nil after leaf swap")
	}
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("cleanup changed original context behind swapped leaf: %v", err)
	}
	if info, err := os.Lstat(contextPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("cleanup removed swapped leaf without failing closed: info=%v err=%v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(envRoot, sidecarManifestFile)); err != nil {
		t.Fatalf("failed cleanup removed retry manifest: %v", err)
	}
}

func TestRollbackPlatformSidecarsDoesNotFollowSwappedParentOutsideWorkdir(t *testing.T) {
	workDir := t.TempDir()
	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("rollback-swap"), manifest); err != nil {
		t.Fatal(err)
	}
	platformDir := filepath.Join(workDir, ".platform-agent")
	originalDir := filepath.Join(workDir, ".platform-agent-owned")
	if err := os.Rename(platformDir, originalDir); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideContext := filepath.Join(outside, "context.json")
	if err := os.WriteFile(outsideContext, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, platformDir); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	if err := rollbackSidecarManifest(manifest); err == nil {
		t.Fatal("rollbackSidecarManifest() error = nil after parent swap")
	}
	if data, err := os.ReadFile(outsideContext); err != nil || string(data) != "outside" {
		t.Fatalf("rollback changed outside context: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(originalDir, "context.json")); err != nil {
		t.Fatalf("rollback mistook swapped path for the original sidecar: %v", err)
	}
}

func TestRollbackPlatformSidecarsDoesNotFollowSwappedParentInsideWorkdir(t *testing.T) {
	workDir := t.TempDir()
	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("rollback-internal-swap"), manifest); err != nil {
		t.Fatal(err)
	}
	platformDir := filepath.Join(workDir, ".platform-agent")
	originalDir := filepath.Join(workDir, ".platform-agent-owned")
	if err := os.Rename(platformDir, originalDir); err != nil {
		t.Fatal(err)
	}
	decoyDir := filepath.Join(workDir, "decoy")
	if err := os.Mkdir(decoyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decoyContext := filepath.Join(decoyDir, "context.json")
	if err := os.WriteFile(decoyContext, []byte("user-decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("decoy", platformDir); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	if err := rollbackSidecarManifest(manifest); err == nil {
		t.Fatal("rollbackSidecarManifest() error = nil after internal parent swap")
	}
	if data, err := os.ReadFile(decoyContext); err != nil || string(data) != "user-decoy" {
		t.Fatalf("rollback followed internal parent swap: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(originalDir, "context.json")); err != nil {
		t.Fatalf("rollback mistook swapped parent for original sidecar: %v", err)
	}
}

func TestRollbackPlatformSidecarsRejectsSwappedLeafSymlink(t *testing.T) {
	workDir := t.TempDir()
	manifest := &sidecarManifest{}
	if err := writePlatformAgentContext(workDir, validPlatformAgentContext("rollback-leaf-swap"), manifest); err != nil {
		t.Fatal(err)
	}
	contextPath := filepath.Join(workDir, ".platform-agent", "context.json")
	originalPath := filepath.Join(workDir, ".platform-agent", "context-owned.json")
	if err := os.Rename(contextPath, originalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("context-owned.json", contextPath); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	if err := rollbackSidecarManifest(manifest); err == nil {
		t.Fatal("rollbackSidecarManifest() error = nil after leaf swap")
	}
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("rollback changed original context behind swapped leaf: %v", err)
	}
	if info, err := os.Lstat(contextPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("rollback removed swapped leaf without failing closed: info=%v err=%v", info, err)
	}
}

func TestReusePlatformAgentDoesNotFollowSwappedSkillParentOutsideWorkdir(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	managedSkillDir := filepath.Join(workDir, ".agent_context", "skills", "managed")
	if err := os.MkdirAll(managedSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedSkillDir, "SKILL.md"), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, &sidecarManifest{
		Rooted: true,
		Dirs:   []string{filepath.Join(".agent_context", "skills", "managed")},
	}); err != nil {
		t.Fatal(err)
	}
	skillsParent := filepath.Join(workDir, ".agent_context", "skills")
	originalSkillsParent := filepath.Join(workDir, ".agent_context", "skills-owned")
	if err := os.Rename(skillsParent, originalSkillsParent); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideSkill := filepath.Join(outside, "managed")
	if err := os.MkdirAll(outsideSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideSentinel := filepath.Join(outsideSkill, "user.txt")
	if err := os.WriteFile(outsideSentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, skillsParent); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	got := Reuse(ReuseParams{
		WorkspacesRoot: envRoot,
		WorkDir:        workDir,
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-skill-swap",
			PlatformAgentContext: validPlatformAgentContext("skill-swap"),
		},
	}, discardLogger())
	if got != nil {
		t.Fatalf("Reuse() = %+v, want fail-closed nil", got)
	}
	if data, err := os.ReadFile(outsideSentinel); err != nil || string(data) != "outside" {
		t.Fatalf("reuse changed outside skill: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(originalSkillsParent, "managed", "SKILL.md")); err != nil {
		t.Fatalf("reuse mistook swapped skill parent for managed content: %v", err)
	}
}

func TestReusePlatformAgentDoesNotFollowSwappedSkillParentInsideWorkdir(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	managedSkillDir := filepath.Join(workDir, ".agent_context", "skills", "managed")
	if err := os.MkdirAll(managedSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedSkillDir, "SKILL.md"), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, &sidecarManifest{
		Rooted: true,
		Dirs:   []string{filepath.Join(".agent_context", "skills", "managed")},
	}); err != nil {
		t.Fatal(err)
	}
	skillsParent := filepath.Join(workDir, ".agent_context", "skills")
	originalSkillsParent := filepath.Join(workDir, ".agent_context", "skills-owned")
	if err := os.Rename(skillsParent, originalSkillsParent); err != nil {
		t.Fatal(err)
	}
	decoySkill := filepath.Join(workDir, ".agent_context", "decoy", "managed")
	if err := os.MkdirAll(decoySkill, 0o755); err != nil {
		t.Fatal(err)
	}
	decoySentinel := filepath.Join(decoySkill, "user.txt")
	if err := os.WriteFile(decoySentinel, []byte("user-decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("decoy", skillsParent); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	got := Reuse(ReuseParams{
		WorkspacesRoot: envRoot,
		WorkDir:        workDir,
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-skill-internal-swap",
			PlatformAgentContext: validPlatformAgentContext("skill-internal-swap"),
		},
	}, discardLogger())
	if got != nil {
		t.Fatalf("Reuse() = %+v, want fail-closed nil", got)
	}
	if data, err := os.ReadFile(decoySentinel); err != nil || string(data) != "user-decoy" {
		t.Fatalf("reuse followed internal skill-parent swap: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(originalSkillsParent, "managed", "SKILL.md")); err != nil {
		t.Fatalf("reuse mistook swapped parent for managed content: %v", err)
	}
}

func TestReusePlatformAgentRejectsSwappedManagedSkillLeaf(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	managedSkillDir := filepath.Join(workDir, ".agent_context", "skills", "managed")
	if err := os.MkdirAll(managedSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedSkillDir, "SKILL.md"), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, &sidecarManifest{
		Rooted: true,
		Dirs:   []string{filepath.Join(".agent_context", "skills", "managed")},
	}); err != nil {
		t.Fatal(err)
	}
	originalSkillDir := filepath.Join(workDir, ".agent_context", "skills", "managed-owned")
	if err := os.Rename(managedSkillDir, originalSkillDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("managed-owned", managedSkillDir); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	got := Reuse(ReuseParams{
		WorkspacesRoot: envRoot,
		WorkDir:        workDir,
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-skill-leaf-swap",
			PlatformAgentContext: validPlatformAgentContext("skill-leaf-swap"),
		},
	}, discardLogger())
	if got != nil {
		t.Fatalf("Reuse() = %+v, want fail-closed nil", got)
	}
	if _, err := os.Stat(filepath.Join(originalSkillDir, "SKILL.md")); err != nil {
		t.Fatalf("reuse changed original skill behind swapped leaf: %v", err)
	}
	if info, err := os.Lstat(managedSkillDir); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("reuse removed swapped skill leaf without failing closed: info=%v err=%v", info, err)
	}
}

func TestPlatformRootOperationsRejectSymlinkWorkdir(t *testing.T) {
	t.Run("publication", func(t *testing.T) {
		outside := t.TempDir()
		workDir := filepath.Join(t.TempDir(), "workdir-link")
		if err := os.Symlink(outside, workDir); err != nil {
			t.Skipf("symlink fixture unavailable: %v", err)
		}
		manifest := &sidecarManifest{}
		err := writePlatformAgentContext(workDir, validPlatformAgentContext("symlink-root"), manifest)
		if err == nil {
			t.Fatal("writePlatformAgentContext() error = nil for symlink workdir")
		}
		if _, statErr := os.Lstat(filepath.Join(outside, ".platform-agent")); !os.IsNotExist(statErr) {
			t.Fatalf("publication escaped through symlink workdir: %v", statErr)
		}
	})

	t.Run("cleanup", func(t *testing.T) {
		envRoot := t.TempDir()
		outside := t.TempDir()
		outsideContext := filepath.Join(outside, ".platform-agent", "context.json")
		if err := os.MkdirAll(filepath.Dir(outsideContext), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outsideContext, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		workDir := filepath.Join(t.TempDir(), "workdir-link")
		if err := os.Symlink(outside, workDir); err != nil {
			t.Skipf("symlink fixture unavailable: %v", err)
		}
		if err := writeSidecarManifest(envRoot, &sidecarManifest{
			Rooted: true,
			Files:  []string{filepath.Join(".platform-agent", "context.json")},
		}); err != nil {
			t.Fatal(err)
		}

		if err := CleanupSidecarsAt(envRoot, workDir); err == nil {
			t.Fatal("CleanupSidecarsAt() error = nil for symlink workdir")
		}
		if data, err := os.ReadFile(outsideContext); err != nil || string(data) != "outside" {
			t.Fatalf("cleanup changed outside context: %q, %v", data, err)
		}
	})
}

func TestPlatformManifestRecordsOnlyRootRelativePaths(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "workspace-rooted-manifest",
		TaskID:         "task-rooted-manifest",
		AgentName:      "Rooted Manifest",
		Provider:       "platform-agent-cli",
		Task: TaskContextForEnv{
			AgentID:              "agent-rooted-manifest",
			PlatformAgentContext: validPlatformAgentContext("rooted-manifest"),
		},
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.RootDir, sidecarManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var manifest sidecarManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.Rooted {
		t.Fatalf("platform manifest is not marked root-confined: %s", data)
	}
	if len(manifest.Files) == 0 || len(manifest.Dirs) == 0 {
		t.Fatalf("platform manifest omitted owned paths: %+v", manifest)
	}
	for _, path := range append(append([]string(nil), manifest.Files...), manifest.Dirs...) {
		if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			t.Fatalf("platform manifest contains non-rooted path %q: %s", path, data)
		}
	}
}

func TestCleanupSidecarsAtSafelyReadsLegacyAbsoluteManifest(t *testing.T) {
	t.Run("owned path", func(t *testing.T) {
		envRoot := t.TempDir()
		workDir := filepath.Join(envRoot, "workdir")
		legacyDir := filepath.Join(workDir, ".platform-agent")
		legacyContext := filepath.Join(legacyDir, "context.json")
		if err := os.MkdirAll(legacyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacyContext, []byte("legacy-owned"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeSidecarManifest(envRoot, &sidecarManifest{Files: []string{legacyContext}, Dirs: []string{legacyDir}}); err != nil {
			t.Fatal(err)
		}
		if err := CleanupSidecarsAt(envRoot, workDir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(legacyDir); !os.IsNotExist(err) {
			t.Fatalf("legacy owned sidecars survived cleanup: %v", err)
		}
	})

	t.Run("outside path", func(t *testing.T) {
		envRoot := t.TempDir()
		workDir := filepath.Join(envRoot, "workdir")
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "user.txt")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeSidecarManifest(envRoot, &sidecarManifest{Files: []string{outside}}); err != nil {
			t.Fatal(err)
		}
		if err := CleanupSidecarsAt(envRoot, workDir); err == nil {
			t.Fatal("CleanupSidecarsAt() accepted legacy path outside trusted workdir")
		}
		if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
			t.Fatalf("outside legacy target changed: %q, %v", data, err)
		}
	})
}

func TestAtomicPlatformPublishRejectsSwappedParentHandle(t *testing.T) {
	workDir := t.TempDir()
	parentPath := filepath.Join(workDir, ".platform-agent")
	if err := os.Mkdir(parentPath, 0o700); err != nil {
		t.Fatal(err)
	}
	workRoot, err := os.OpenRoot(workDir)
	if err != nil {
		t.Fatal(err)
	}
	defer workRoot.Close()
	expectedParent, err := workRoot.Lstat(".platform-agent")
	if err != nil {
		t.Fatal(err)
	}
	parentRoot, err := workRoot.OpenRoot(".platform-agent")
	if err != nil {
		t.Fatal(err)
	}
	defer parentRoot.Close()

	originalParent := filepath.Join(workDir, ".platform-agent-owned")
	if err := os.Rename(parentPath, originalParent); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, parentPath); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	err = atomicWriteFileNoClobberAt(workRoot, parentRoot, expectedParent, ".platform-agent", "context.json", []byte(`{"safe":true}`), 0o600)
	if err == nil {
		t.Fatal("atomicWriteFileNoClobberAt() error = nil after parent swap")
	}
	if _, statErr := os.Lstat(filepath.Join(outside, "context.json")); !os.IsNotExist(statErr) {
		t.Fatalf("publish escaped into outside directory: %v", statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(originalParent, "context.json")); !os.IsNotExist(statErr) {
		t.Fatalf("failed publish left an untracked context in original parent: %v", statErr)
	}
}

func TestWritePlatformAgentContextAtomicallyRefusesConcurrentClobber(t *testing.T) {
	workDir := t.TempDir()
	const writers = 12
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := validPlatformAgentContext("agent-" + string(rune('a'+i)))
			<-start
			manifest := &sidecarManifest{}
			err := writePlatformAgentContext(workDir, ctx, manifest)
			_ = manifest.closeRoot()
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errPathPreExists):
		default:
			t.Fatalf("unexpected concurrent writer error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful writers = %d, want 1", successes)
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".platform-agent", "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded PlatformAgentContextForEnv
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("published partial JSON: %v: %q", err, data)
	}
	if !strings.HasPrefix(decoded.Agent.SourceKey, "agent-") {
		t.Fatalf("unexpected published source key %q", decoded.Agent.SourceKey)
	}
}

func validPlatformAgentContext(sourceKey string) *PlatformAgentContextForEnv {
	return &PlatformAgentContextForEnv{
		SchemaVersion: PlatformAgentRuntimeContextSchema,
		Extension: PlatformAgentExtensionForEnv{
			Key:       "research-team",
			Version:   "1.0.0",
			ReleaseID: "release-1",
			Digest:    "sha256:abc",
		},
		Agent: PlatformAgentIdentityForEnv{SourceKey: sourceKey},
		Commands: []PlatformAgentCommandForEnv{{
			Name:        "summarize",
			Description: "Summary command.",
			Content:     "Summarize findings.",
			Metadata:    json.RawMessage(`{"owner":"platform"}`),
		}},
	}
}
