package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRuntimeMcpPasswdHomeMismatchWarns pins the warning-only diagnostic for
// #9230/#9233: when $HOME has no config for the provider but the passwd
// entry's home does, the daemon logs where the config actually lives —
// without loading it ($HOME stays authoritative) and without warning when the
// two homes agree or the passwd home has nothing either. A container baking a
// different home into the image otherwise reads as an empty inventory to
// puzzle over.
func TestRuntimeMcpPasswdHomeMismatchWarns(t *testing.T) {
	realHome := t.TempDir()
	passwdHome := t.TempDir()
	orig := osUserHome
	osUserHome = func() (string, error) { return passwdHome, nil }
	t.Cleanup(func() { osUserHome = orig })

	captureLogs := func() *strings.Builder {
		var logs strings.Builder
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
		t.Cleanup(func() { slog.SetDefault(prev) })
		return &logs
	}

	for _, tt := range []struct {
		name       string
		seedPasswd bool
		sameHome   bool
		wantWarn   bool
	}{
		{name: "config only under the passwd home warns", seedPasswd: true, wantWarn: true},
		{name: "passwd home without a config stays quiet"},
		{name: "identical homes stay quiet", sameHome: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			os.RemoveAll(filepath.Join(passwdHome, ".claude.json"))
			if tt.seedPasswd {
				if err := os.WriteFile(filepath.Join(passwdHome, ".claude.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.sameHome {
				osUserHome = func() (string, error) { return realHome, nil }
			}

			logs := captureLogs()
			servers, ok, err := loadRuntimeMcpServerConfigs("claude")
			if err != nil || !ok {
				t.Fatalf("claude load: ok=%v err=%v", ok, err)
			}
			// The passwd home's config is never loaded, whatever the case.
			if len(servers) != 0 {
				t.Fatalf("passwd-home config must not be loaded, got %d servers", len(servers))
			}
			if got := strings.Contains(logs.String(), "passwd"); got != tt.wantWarn {
				t.Fatalf("warning = %v, want %v; log:\n%s", got, tt.wantWarn, logs.String())
			}
		})
	}
}
