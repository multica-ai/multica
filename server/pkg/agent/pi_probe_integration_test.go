//go:build agentintegration && !windows

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPiSandboxWrapperProbeReal(t *testing.T) {
	requireRealAgentSmoke(t)
	pi, err := exec.LookPath("pi")
	if err != nil {
		t.Skip("pi is not installed")
	}
	pi, err = filepath.Abs(pi)
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(home)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	baseline, err := discoverPiModelsTable(ctx, NewCommand(pi, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline) == 0 {
		t.Skip("pi has no configured models to compare")
	}
	version, err := DetectVersion(ctx, NewCommand(pi, nil))
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, wrapper, []byte(`#!/bin/sh
if [ "$PWD" -ef "$HOME" ]; then
  echo 'Disallowing sandbox creation from $HOME'
  exit 1
fi
echo 'Some random log lines printed at the starting of the sandbox' >&2
exec `+shellQuote(pi)+` "$@"
`))
	gotVersion, err := DetectVersion(ctx, NewCommand(wrapper, nil))
	if err != nil || gotVersion != version {
		t.Fatalf("wrapped version = %q, %v; want %q", gotVersion, err, version)
	}
	for _, tc := range []struct {
		name     string
		discover func(context.Context, Command) ([]Model, error)
	}{
		{"RPC with table fallback", discoverPiModels},
		{"table", discoverPiModelsTable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, err := tc.discover(ctx, NewCommand(wrapper, nil))
			if err != nil {
				t.Fatal(err)
			}
			want := make(map[string]bool, len(baseline))
			for _, model := range baseline {
				want[model.ID] = true
			}
			if len(models) != len(want) {
				t.Fatalf("got %d models, want %d", len(models), len(want))
			}
			for _, model := range models {
				if !want[model.ID] {
					t.Fatalf("unexpected model %q", model.ID)
				}
			}
			t.Logf("pi %s: %d models, no wrapper log entries", version, len(models))
		})
	}
}
