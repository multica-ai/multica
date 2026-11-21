package daemon

import (
	"context"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestRegisterRuntimesSan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakeDir := t.TempDir()
	pathSan := filepath.Join(fakeDir, "san")
	pinnedSan := filepath.Join(fakeDir, "custom-san")
	for _, path := range []string{pathSan, pinnedSan} {
		writeDaemonTestExecutable(t, path, []byte("#!/bin/sh\n[ \"$1\" = version ] || exit 7\necho 'san version 1.25.4'\n"))
	}
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return nil }
	resetShellResolveCacheForTest(t)
	t.Setenv("PATH", fakeDir)
	t.Setenv("MULTICA_SAN_MODEL", "must-not-be-used")

	for _, tc := range []struct{ name, override, path string }{
		{"PATH", "", pathSan},
		{"explicit override", pinnedSan, pinnedSan},
		{"missing override", filepath.Join(fakeDir, "missing"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_SAN_PATH", tc.override)
			entry, ok := probeAgentCLIs()["san"]
			if tc.path == "" {
				if ok {
					t.Fatalf("missing override should not fall back to PATH: %+v", entry)
				}
				return
			}
			resolved, err := filepath.EvalSymlinks(entry.Path)
			if err != nil {
				t.Fatal(err)
			}
			wantPath, err := filepath.EvalSymlinks(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !ok || resolved != wantPath || entry.Model != "" {
				t.Fatalf("probe entry = %+v (found %v), want path %q and no model", entry, ok, tc.path)
			}

			fx := newBatchFixture(t)
			detectAgentVersion = agent.DetectVersion
			checkAgentMinVersion = agent.CheckMinVersion
			fx.daemon.cfg.Agents = map[string]AgentEntry{"san": entry}
			fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
			if err := fx.daemon.syncWorkspacesFromAPI(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			types, calls := fx.registrationFor("ws-1")
			if calls == 0 || !slices.Contains(types, "san") {
				t.Fatalf("register calls = %d, types = %v; want san", calls, types)
			}
		})
	}
}
