package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestAntigravitySupplementHookCommandGate(t *testing.T) {
	originalResolveSelfExecutable := resolveSelfExecutable
	t.Cleanup(func() { resolveSelfExecutable = originalResolveSelfExecutable })
	resolveSelfExecutable = func() (string, error) { return "/usr/local/bin/multica", nil }

	workdir := t.TempDir()
	command, ok := antigravitySupplementHookCommand(&execenv.Environment{WorkDir: workdir})
	if runtime.GOOS == "windows" {
		if ok {
			t.Fatal("antigravity steering offered on Windows")
		}
		return
	}
	if want := []string{"/usr/local/bin/multica", agent.AntigravityHookHelperArg}; !ok || !slices.Equal(command, want) {
		t.Fatalf("managed workdir: command=%q ok=%v, want %q", command, ok, want)
	}

	for name, env := range map[string]*execenv.Environment{
		"nil env":         nil,
		"local directory": {WorkDir: "/Users/me/project", LocalDirectory: true},
		"local worktree":  {WorkDir: "/Users/me/project-wt", LocalWorktree: &execenv.LocalWorktree{}},
	} {
		if _, ok := antigravitySupplementHookCommand(env); ok {
			t.Fatalf("%s: antigravity steering offered", name)
		}
	}

	// Skills share .agents/ and must not block steering; a hooks.json Multica
	// did not write must.
	if err := os.MkdirAll(filepath.Join(workdir, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := antigravitySupplementHookCommand(&execenv.Environment{WorkDir: workdir}); !ok {
		t.Fatal("steering refused for a workdir holding only skills under .agents")
	}
	if err := os.WriteFile(filepath.Join(workdir, ".agents", "hooks.json"), []byte(`{"user":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := antigravitySupplementHookCommand(&execenv.Environment{WorkDir: workdir}); ok {
		t.Fatal("steering offered for a workdir whose own hooks.json blocks the managed hooks")
	}
}
