package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCodeArtsConfiguredModelsSupportsJSONC(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".codeartsdoer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{
  // A custom OpenCode-compatible provider remains useful in CodeArts TUI.
  "provider": {
    "mimo": {
      "models": {
        "mimo-v2.5-pro": { "name": "MiMo V2.5 Pro", },
        "mimo-v2.5": { "name": "MiMo V2.5" }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(configDir, "codearts_cli.jsonc"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	models, err := loadCodeArtsConfiguredModels(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	if models[0].ID != "mimo/mimo-v2.5" || models[0].Label != "MiMo V2.5" || models[0].Provider != "mimo" {
		t.Fatalf("first model = %+v", models[0])
	}
	if models[1].ID != "mimo/mimo-v2.5-pro" || models[1].Label != "MiMo V2.5 Pro" {
		t.Fatalf("second model = %+v", models[1])
	}
}
