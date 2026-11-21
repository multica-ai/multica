package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResidentGatewayKeepsGatewayStateMirrored(t *testing.T) {
	shared := t.TempDir()
	entries := []string{"gateway.pid", "gateway.lock", "gateway_state.json", "sessions", "logs", "cron"}
	for _, name := range entries {
		if name == "sessions" || name == "logs" || name == "cron" {
			if err := os.Mkdir(filepath.Join(shared, name), 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(shared, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"1", "", "true"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("MULTICA_HERMES_RESIDENT_GATEWAY", mode)
			home := t.TempDir()
			if err := prepareHermesGatewayState(home); err != nil {
				t.Fatal(err)
			}
			if err := mirrorSharedHermesHome(shared, home, nil); err != nil {
				t.Fatal(err)
			}
			for _, name := range entries {
				info, err := os.Lstat(filepath.Join(home, name))
				if mode == "1" {
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Errorf("resident: %s not mirrored as a link: %v", name, err)
					}
				} else if !os.IsNotExist(err) {
					t.Errorf("default: %s should stay isolated: %v", name, err)
				}
			}
			_, err := os.Stat(filepath.Join(home, hermesGatewayStateMarker))
			if mode == "1" && !os.IsNotExist(err) {
				t.Error("resident: gateway isolation marker written")
			} else if mode != "1" && err != nil {
				t.Errorf("default: missing gateway isolation marker: %v", err)
			}
		})
	}
}
