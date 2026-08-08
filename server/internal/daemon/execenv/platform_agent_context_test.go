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
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(contextDir, "context.json")); !os.IsNotExist(err) {
		t.Fatalf("context sidecar survived cleanup: %v", err)
	}
	if info, err := os.Stat(contextDir); err != nil || !info.IsDir() {
		t.Fatalf("pre-existing parent was removed: info=%v err=%v", info, err)
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
			errs <- writePlatformAgentContext(workDir, ctx, &sidecarManifest{})
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
