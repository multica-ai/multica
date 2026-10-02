package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestOpenCodeLaunchUsesCurrentVersion(t *testing.T) {
	// No installed CLI is resolved or executed; the probe is scoped to a fixture.
	path := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(path, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	original := detectAgentVersion
	t.Cleanup(func() { detectAgentVersion = original })
	for _, tc := range []struct {
		name, cached, detected string
		probeErr               error
		wantErr, healed        bool
	}{
		{name: "upgrade", cached: "1.18.25", detected: "opencode v2.0.18"},
		{name: "cached healed path also refreshes", cached: "1.18.25", detected: "opencode v2.0.18", healed: true},
		{name: "downgrade", cached: "opencode v2.0.18", detected: "1.18.25"},
		{name: "unreadable", cached: "1.18.25", probeErr: errors.New("probe failed"), wantErr: true},
		{name: "unknown", cached: "opencode v2.0.18", detected: "nightly", wantErr: true},
		{name: "unsupported", cached: "opencode v2.0.18", detected: "0.1.0", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newSelfHealTestDaemon()
			d.setAgentVersion("opencode", tc.cached)
			if tc.healed {
				d.resolvedPaths["opencode"] = healedAgent{path: path, version: tc.cached}
			}
			calls := 0
			detectAgentVersion = func(_ context.Context, cmd agent.Command) (string, error) {
				calls++
				if cmd.Path != path {
					t.Fatalf("probed %q, want %q", cmd.Path, path)
				}
				return tc.detected, tc.probeErr
			}
			got, version, err := d.resolveAgentEntryForLaunch(context.Background(), "opencode", AgentEntry{Path: path})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if calls != 1 {
				t.Fatalf("probes=%d", calls)
			}
			if !tc.wantErr && (got.Path != path || version != tc.detected) {
				t.Fatalf("path=%q version=%q", got.Path, version)
			}
		})
	}
}
