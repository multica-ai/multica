//go:build !windows

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeProbesUseEmptyDirectory(t *testing.T) {
	// Reproduce a wrapper refusing to grant write access to the daemon's cwd.
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("MUL_TEST_PROBE_LOG", filepath.Join(dir, "probes"))
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("global-config"), 0600); err != nil {
		t.Fatal(err)
	}
	guard := `#!/bin/sh
if [ "$PWD" = "$HOME" ]; then
  echo 'Disallowing sandbox creation from $HOME' >&2
  exit 1
fi
test -z "$(ls -A)" || exit 2
test "$(cat "$HOME/config")" = global-config || exit 3
printf '%s\n' "$PWD" >> "$MUL_TEST_PROBE_LOG"
echo 'Some random log lines printed at the starting of the sandbox' >&2
`
	version := filepath.Join(dir, "version")
	writeTestExecutable(t, version, []byte(guard+"test \"$1\" = start || exit 4\ntest \"$2\" = --version || exit 5\necho 1.0.0\n"))
	// Executable resolution must happen before changing the child directory.
	got, err := DetectVersion(context.Background(), NewCommand("./version", []string{"start"}))
	if err != nil || got != "1.0.0" {
		t.Fatalf("version = %q, %v", got, err)
	}
	rpc := writeFakePiRPCModelsBinary(t)
	rpcBody, err := os.ReadFile(rpc)
	if err != nil {
		t.Fatal(err)
	}
	writeTestExecutable(t, rpc, []byte(guard+strings.TrimPrefix(string(rpcBody), "#!/bin/sh\n")))
	models, err := discoverPiModels(context.Background(), NewCommand(rpc, nil))
	if err != nil || len(models) != 3 {
		t.Fatalf("RPC models = %+v, %v", models, err)
	}
	table := filepath.Join(dir, "table")
	writeTestExecutable(t, table, []byte(guard+"test \"$1\" = --list-models || exit 6\necho 'provider model context'\necho 'custom local-model 128K'\n"))
	models, err = discoverPiModels(context.Background(), NewCommand(table, nil))
	if err != nil || len(models) != 1 || models[0].ID != "custom/local-model" {
		t.Fatalf("table models = %+v, %v", models, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "probes"))
	if err != nil {
		t.Fatal(err)
	}
	probes := strings.Fields(string(data))
	if len(probes) != 4 {
		t.Fatalf("got %d probes, want version, RPC, failed RPC and table", len(probes))
	}
	for _, probeDir := range probes {
		if _, err := os.Stat(probeDir); !os.IsNotExist(err) {
			t.Errorf("probe directory was not cleaned up: %s (%v)", probeDir, err)
		}
	}
	if cwd, _ := os.Getwd(); cwd != dir {
		t.Fatalf("parent cwd changed: %q", cwd)
	}
}

func TestDiscoverPiModelsRejectsWrapperLogs(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		for _, exitCode := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/exit%d", stream, exitCode), func(t *testing.T) {
				redirect := ""
				if stream == "stderr" {
					redirect = " >&2"
				}
				path := filepath.Join(t.TempDir(), "pi")
				script := "#!/bin/sh\necho 'Disallowing sandbox creation from $HOME'" + redirect + "\necho 'Some random log lines printed at the starting of the sandbox'" + redirect + fmt.Sprintf("\nexit %d\n", exitCode)
				writeTestExecutable(t, path, []byte(script))
				models, err := discoverPiModels(context.Background(), NewCommand(path, nil))
				if len(models) != 0 {
					t.Fatalf("logs returned as models: %+v", models)
				}
				if exitCode != 0 && err == nil {
					t.Fatal("failed wrapper must report an error")
				}
			})
		}
	}
}
