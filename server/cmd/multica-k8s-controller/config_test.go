package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig_FromEnvAndFile(t *testing.T) {
	cfgDir := t.TempDir()
	cfgYAML := []byte(`
workspaces:
  - id: 11111111-1111-1111-1111-111111111111
    provider: claude
    agentName: Lambda
    runtimeImage: ghcr.io/chrissnell/multica-runtime-claude:v0.3.0-mk1
    pvcSize: 5Gi
    storageClass: ""
imagePullSecret: ghcr-pull
`)
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.yaml"), cfgYAML, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MULTICA_SERVER_URL", "http://multica-backend.multica.svc:8080")
	t.Setenv("MULTICA_TOKEN", "tk")
	t.Setenv("POD_NAMESPACE", "multica")
	t.Setenv("CONTROLLER_CONFIG_DIR", cfgDir)

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got.ServerBaseURL != "http://multica-backend.multica.svc:8080" {
		t.Errorf("ServerBaseURL = %q", got.ServerBaseURL)
	}
	if got.Token != "tk" {
		t.Errorf("Token mismatch")
	}
	if got.Namespace != "multica" {
		t.Errorf("Namespace = %q", got.Namespace)
	}
	if len(got.Workspaces) != 1 || got.Workspaces[0].Provider != "claude" {
		t.Errorf("Workspaces parsed wrong: %+v", got.Workspaces)
	}
	if got.PollInterval != 3*time.Second {
		t.Errorf("PollInterval default = %v", got.PollInterval)
	}
}
