package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func withResolvedExecutable(t *testing.T, path string) {
	t.Helper()
	original := resolveAgentSelfExecutable
	resolveAgentSelfExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { resolveAgentSelfExecutable = original })
}

func withACLGrantRecorder(t *testing.T, calls *[]string, err error) {
	t.Helper()
	original := grantWindowsCodexSandboxUsersRX
	grantWindowsCodexSandboxUsersRX = func(path string, inheritable bool) error {
		*calls = append(*calls, path)
		if inheritable {
			(*calls)[len(*calls)-1] += "|inherit"
		}
		return err
	}
	t.Cleanup(func() { grantWindowsCodexSandboxUsersRX = original })
}

func TestAgentCLIDirForTask_WindowsCodexStagesTaskScopedCLI(t *testing.T) {
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "multica.exe")
	if err := os.WriteFile(source, []byte("fake windows cli"), 0o755); err != nil {
		t.Fatalf("write source cli: %v", err)
	}
	withResolvedExecutable(t, source)
	var aclCalls []string
	withACLGrantRecorder(t, &aclCalls, nil)

	envRoot := t.TempDir()
	got := agentCLIDirForTaskForGOOS("codex", envRoot, "windows", nil)

	wantDir := filepath.Join(envRoot, "bin")
	if got != wantDir {
		t.Fatalf("agent CLI dir = %q, want %q", got, wantDir)
	}
	dest := filepath.Join(wantDir, "multica.exe")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read staged cli: %v", err)
	}
	if string(data) != "fake windows cli" {
		t.Fatalf("staged cli content = %q", string(data))
	}
	wantCalls := []string{wantDir + "|inherit", dest}
	if len(aclCalls) != len(wantCalls) {
		t.Fatalf("ACL calls = %#v, want %#v", aclCalls, wantCalls)
	}
	for i := range wantCalls {
		if aclCalls[i] != wantCalls[i] {
			t.Fatalf("ACL calls = %#v, want %#v", aclCalls, wantCalls)
		}
	}
}

func TestAgentCLIDirForTask_WindowsCodexKeepsStagedCLIWhenACLGrantFails(t *testing.T) {
	source := filepath.Join(t.TempDir(), "multica.exe")
	if err := os.WriteFile(source, []byte("fake windows cli"), 0o755); err != nil {
		t.Fatalf("write source cli: %v", err)
	}
	withResolvedExecutable(t, source)
	var aclCalls []string
	withACLGrantRecorder(t, &aclCalls, errors.New("group missing"))

	envRoot := t.TempDir()
	got := agentCLIDirForTaskForGOOS("codex", envRoot, "windows", nil)

	wantDir := filepath.Join(envRoot, "bin")
	if got != wantDir {
		t.Fatalf("agent CLI dir = %q, want %q", got, wantDir)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "multica.exe")); err != nil {
		t.Fatalf("staged CLI should remain usable after ACL grant failure: %v", err)
	}
	if len(aclCalls) != 2 {
		t.Fatalf("ACL grant should have been attempted for dir and file, got %#v", aclCalls)
	}
}

func TestAgentCLIDirForTask_NonWindowsUsesSelfExecutableDir(t *testing.T) {
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "multica")
	if err := os.WriteFile(source, []byte("fake cli"), 0o755); err != nil {
		t.Fatalf("write source cli: %v", err)
	}
	withResolvedExecutable(t, source)
	var aclCalls []string
	withACLGrantRecorder(t, &aclCalls, nil)

	got := agentCLIDirForTaskForGOOS("codex", t.TempDir(), "linux", nil)

	if got != sourceDir {
		t.Fatalf("agent CLI dir = %q, want %q", got, sourceDir)
	}
	if len(aclCalls) != 0 {
		t.Fatalf("non-windows path should not touch ACLs, got %#v", aclCalls)
	}
}

func TestAgentCLIDirForTask_WindowsNonCodexUsesSelfExecutableDir(t *testing.T) {
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "multica.exe")
	if err := os.WriteFile(source, []byte("fake cli"), 0o755); err != nil {
		t.Fatalf("write source cli: %v", err)
	}
	withResolvedExecutable(t, source)

	got := agentCLIDirForTaskForGOOS("claude", t.TempDir(), "windows", nil)

	if got != sourceDir {
		t.Fatalf("agent CLI dir = %q, want %q", got, sourceDir)
	}
}

func TestAgentCLIDirForTask_HintWinsOverSelfExecutable(t *testing.T) {
	selfDir := t.TempDir()
	withResolvedExecutable(t, filepath.Join(selfDir, "multica"))
	hintDir := t.TempDir()
	hint := filepath.Join(hintDir, "multica")
	if err := os.WriteFile(hint, []byte("hint cli"), 0o755); err != nil {
		t.Fatalf("write hint cli: %v", err)
	}
	t.Setenv(MulticaCLIPathEnv, hint)

	got := agentCLIDirForTaskForGOOS("claude", t.TempDir(), "linux", nil)

	if got != hintDir {
		t.Fatalf("agent CLI dir = %q, want hint dir %q", got, hintDir)
	}
}
