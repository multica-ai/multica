//go:build !windows

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZcodeLinuxDesktopHeadless(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		name := "direct"
		if symlink {
			name = "PATH symlink"
		}
		t.Run(name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "ZCode install", "zcode")
			script := filepath.Join(filepath.Dir(bin), "resources", "glm", "zcode.cjs")
			if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
				t.Fatal(err)
			}
			guiMarker := filepath.Join(root, "gui-started")
			t.Setenv("ZCODE_GUI_MARKER", guiMarker)
			t.Setenv("ZCODE_EXPECTED_SCRIPT", script)
			t.Setenv("ELECTRON_RUN_AS_NODE", "0")
			t.Setenv("ZCODE_DATA_BASE_DIR", root)
			t.Setenv("PATH", root) // No standalone Node.js.
			if err := os.WriteFile(bin, []byte(`#!/bin/sh
if [ "$ELECTRON_RUN_AS_NODE" != 1 ] || [ "$1" != "$ZCODE_EXPECTED_SCRIPT" ]; then
  printf 'GUI launched\n' > "$ZCODE_GUI_MARKER"
  exit 1
fi
script=$1
shift
if [ "$1" = --version ]; then
  printf '0.17.1\n'
  exit 0
fi
exec /bin/sh "$script" "$@"
`), 0o755); err != nil {
				t.Fatal(err)
			}
			if symlink {
				link := filepath.Join(root, "zcode")
				if err := os.Symlink(bin, link); err != nil {
					t.Fatal(err)
				}
				bin = link
			}
			writeCLI := func(body string) {
				t.Helper()
				body = strings.ReplaceAll(body, "| sed ", "| /usr/bin/sed ")
				if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			writeCLI(fakeZcodeDiscoveryScript)
			t.Setenv("ZCODE_DISCOVERY_CAPTURE", filepath.Join(root, "discovery.txt"))
			version, err := DetectVersion(context.Background(), NewCommand(bin, nil).ForVersionProbe("zcode"))
			if err != nil || version != "0.17.1" {
				t.Errorf("version = %q, err = %v", version, err)
			}
			catalog, err := discoverZcodeModels(context.Background(), NewCommand(bin, nil))
			if err != nil || len(catalog.Models) != 2 {
				t.Errorf("model discovery = %+v, err = %v", catalog, err)
			}
			writeCLI(fakeZcodeHappyScript(zcodeEventFrame(6, "turn.completed", `{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)))
			session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "hello", ExecOptions{Cwd: root})
			if err != nil {
				t.Errorf("execute: %v", err)
			} else if _, res := collectZcodeSession(t, session); res.Status != "completed" {
				t.Errorf("result = %+v", res)
			}
			if _, err := os.Stat(guiMarker); !os.IsNotExist(err) {
				t.Error("desktop GUI entry point was invoked")
			}
		})
	}
}

func TestZcodeLinuxDesktopMissingCLI(t *testing.T) {
	bin := writeFakeZcodeScript(t, "#!/bin/sh\nexit 42\n")
	if err := os.MkdirAll(filepath.Join(filepath.Dir(bin), "resources", "glm"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := DetectVersion(context.Background(), NewCommand(bin, nil).ForVersionProbe("zcode"))
	if err == nil || !strings.Contains(err.Error(), "MULTICA_ZCODE_PATH") {
		t.Fatalf("expected a headless CLI configuration error, got %v", err)
	}
}

func TestZcodeLinuxDesktopProviderConfig(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "zcode")
	script := filepath.Join(root, "resources", "glm", "zcode.cjs")
	builtin := filepath.Join(root, "resources", "config", "provider", "zcode-builtin.json")
	for _, path := range []string{script, builtin} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env, cleanup, err := prepareZcodeRuntimeEnv(bin, []string{"ZCODE_DATA_BASE_DIR=" + root})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got := zcodeEnvValue(env, "ZCODE_BUILTIN_PROVIDER_CONFIG_FILE"); got != builtin {
		t.Fatalf("builtin config = %q, want %q", got, builtin)
	}
	if zcodeEnvValue(env, zcodePersonalConfigEnv) == "" {
		t.Fatal("Linux Desktop did not receive its private provider snapshot")
	}
}
