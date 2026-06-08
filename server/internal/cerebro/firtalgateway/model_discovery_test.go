package firtalgateway

import "testing"

func clearDiscoveryEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"FIRTAL_REGISTRY_URL",
		"FIRTAL_REGISTRY_KEY",
		"FIRTAL_REGISTRY_ALLOW_INTERNAL",
	} {
		t.Setenv(key, "")
	}
}

// FIR-2825: when env is unset, the workspace setting is used as fallback.
func TestDiscoveryConfigUsesWorkspaceFallbackWhenEnvUnset(t *testing.T) {
	clearDiscoveryEnv(t)

	cfg, ok, err := DiscoveryConfig([]byte(`{
		"firtal_gateway": {
			"enabled": true,
			"gateway_url": "https://registry.example/",
			"api_key": "rk_workspace"
		}
	}`))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !ok {
		t.Fatal("expected configured gateway")
	}
	if cfg.BaseURL != "https://registry.example" || cfg.APIKey != "rk_workspace" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// FIR-2825: when env URL/key are set, they are authoritative and the
// workspace URL/key are ignored.
func TestDiscoveryConfigEnvWinsOverWorkspace(t *testing.T) {
	clearDiscoveryEnv(t)
	t.Setenv("FIRTAL_REGISTRY_URL", "https://operator.example/")
	t.Setenv("FIRTAL_REGISTRY_KEY", "rk_operator")

	cfg, ok, err := DiscoveryConfig([]byte(`{
		"firtal_gateway": {
			"enabled": true,
			"gateway_url": "https://workspace.example/",
			"api_key": "rk_workspace"
		}
	}`))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !ok {
		t.Fatal("expected configured gateway")
	}
	if cfg.BaseURL != "https://operator.example" {
		t.Fatalf("expected env URL to win, got %q", cfg.BaseURL)
	}
	if cfg.APIKey != "rk_operator" {
		t.Fatalf("expected env API key to win, got %q", cfg.APIKey)
	}
}
