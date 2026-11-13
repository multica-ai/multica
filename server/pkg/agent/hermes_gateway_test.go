package agent

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStartHermesTaskGatewayScope(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name, version, envHome, home string
	}{
		{"older ACP", "0.20.6", home, home},
		{"unknown version", "unknown", home, home},
		{"no overlay", "0.22.6", "", ""},
		{"different home", "0.22.6", home, filepath.Join(home, "other")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop, err := StartHermesTaskGateway(context.Background(), Config{
				ExecutablePath: filepath.Join(home, "must-not-run"), CLIVersion: tc.version,
				Env: map[string]string{"HERMES_HOME": tc.envHome},
			}, tc.home)
			if err != nil {
				t.Fatal(err)
			}
			stop()
		})
	}
}
