package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestCursorSupplementHookCommandOnlyForManagedWorkdirs(t *testing.T) {
	originalResolveSelfExecutable := resolveSelfExecutable
	t.Cleanup(func() { resolveSelfExecutable = originalResolveSelfExecutable })
	resolveSelfExecutable = func() (string, error) { return "/usr/local/bin/multica", nil }

	workdir := t.TempDir()
	command, ok := cursorSupplementHookCommand(&execenv.Environment{WorkDir: workdir})
	if runtime.GOOS == "windows" {
		if ok {
			t.Fatal("cursor steering offered on Windows")
		}
		return
	}
	if want := []string{"/usr/local/bin/multica", agent.CursorHookHelperArg}; !ok || !slices.Equal(command, want) {
		t.Fatalf("managed workdir: command=%q ok=%v, want %q", command, ok, want)
	}

	for name, env := range map[string]*execenv.Environment{
		"nil env":         nil,
		"local directory": {WorkDir: "/Users/me/project", LocalDirectory: true},
		"local worktree":  {WorkDir: "/Users/me/project-wt", LocalWorktree: &execenv.LocalWorktree{}},
	} {
		if _, ok := cursorSupplementHookCommand(env); ok {
			t.Fatalf("%s: cursor steering offered", name)
		}
	}

	resolveSelfExecutable = func() (string, error) { return "", errors.New("unresolvable") }
	if _, ok := cursorSupplementHookCommand(&execenv.Environment{WorkDir: workdir}); ok {
		t.Fatal("cursor steering offered without a relay executable")
	}
}

// The adapter refuses to replace a hooks.json it did not write, so the daemon
// must not offer steering for such a workdir in the first place.
func TestCursorSupplementHookCommandRequiresInstallableHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Cursor steering is disabled on Windows")
	}
	originalResolveSelfExecutable := resolveSelfExecutable
	t.Cleanup(func() { resolveSelfExecutable = originalResolveSelfExecutable })
	resolveSelfExecutable = func() (string, error) { return "/opt/multica", nil }

	writeHooks := func(t *testing.T, data string) string {
		t.Helper()
		cwd := t.TempDir()
		if err := os.MkdirAll(filepath.Join(cwd, ".cursor"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".cursor", "hooks.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return cwd
	}

	unmanaged := writeHooks(t, `{"version":1,"hooks":{}}`)
	if _, ok := cursorSupplementHookCommand(&execenv.Environment{WorkDir: unmanaged}); ok {
		t.Fatal("steering offered for a workdir whose own hooks.json blocks the managed hooks")
	}

	leftover := writeHooks(t, `{"version":1,"hooks":{"postToolUse":[{"command":"'/opt/multica' '`+agent.CursorHookHelperArg+`'"}]}}`)
	if _, ok := cursorSupplementHookCommand(&execenv.Environment{WorkDir: leftover}); !ok {
		t.Fatal("steering refused for a workdir holding only Multica's own hooks.json")
	}

	cursorFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(cursorFile, ".cursor"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := cursorSupplementHookCommand(&execenv.Environment{WorkDir: cursorFile}); ok {
		t.Fatal("steering offered although .cursor is a file")
	}
}
