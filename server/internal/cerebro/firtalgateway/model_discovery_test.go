package firtalgateway

import "testing"

func TestDiscoveryConfigUsesWorkspaceSettings(t *testing.T) {
	for _, key := range []string{
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL",
		"FIRTAL_AE_GATEWAY_URL",
		"FIRTAL_DATA_REGISTRY_URL",
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY",
		"FIRTAL_AE_GATEWAY_KEY",
		"FIRTAL_DATA_REGISTRY_API_KEY",
	} {
		t.Setenv(key, "")
	}

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
