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

func TestAntigravityACPSupplementCapability(t *testing.T) {
	dir := t.TempDir()
	for name, env := range map[string]*execenv.Environment{
		"managed clone":   {WorkDir: dir},
		"local directory": {WorkDir: dir, LocalDirectory: true},
		"local worktree":  {WorkDir: dir, LocalWorktree: &execenv.LocalWorktree{}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, version := range []string{"1.5.0", "1.5.15", "1.4.12", ""} {
				command, supported := taskSupplementLaunch("antigravity", version, env, []string{"--multica-antigravity-acp"})
				want := version == "1.5.0" || version == "1.5.15"
				if supported != want || command != nil {
					t.Fatalf("version %q: steering=%v command=%q, want %v and no hook command", version, supported, command, want)
				}
			}
		})
	}
}
