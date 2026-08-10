package execenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestTrustCodexWorkdirOnlyUpdatesTaskConfig(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = 'test'\n[projects.'/tmp/other']\ntrust_level = 'trusted'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TrustCodexWorkdir(home, workdir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := toml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	projects := config["projects"].(map[string]any)
	project := projects[workdir].(map[string]any)
	if project["trust_level"] != "trusted" || config["model"] != "test" || len(projects) != 1 {
		t.Fatalf("config = %#v", config)
	}
}
