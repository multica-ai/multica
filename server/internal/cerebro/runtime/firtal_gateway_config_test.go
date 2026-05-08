package runtime

import "testing"

func TestLoadFirtalGatewayRuntimeConfig_AutoEnablesWithServerCredentials(t *testing.T) {
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "https://registry.example")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_MODEL", "gpt-5.5")
	t.Setenv("MULTICA_SERVER_FIRTAL_GATEWAY_MAX_CONCURRENCY", "7")

	cfg, err := LoadFirtalGatewayRuntimeConfig()
	if err != nil {
		t.Fatalf("LoadFirtalGatewayRuntimeConfig() error = %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected runtime to auto-enable when URL and key are set")
	}
	if cfg.BaseURL != "https://registry.example" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Model != "gpt-5.5" {
		t.Fatalf("Model = %q", cfg.Model)
	}
	if cfg.MaxConcurrency != 7 {
		t.Fatalf("MaxConcurrency = %d", cfg.MaxConcurrency)
	}
}

func TestLoadFirtalGatewayRuntimeConfig_ExplicitFalseDisables(t *testing.T) {
	t.Setenv("MULTICA_SERVER_FIRTAL_GATEWAY_ENABLED", "false")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "https://registry.example")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")

	cfg, err := LoadFirtalGatewayRuntimeConfig()
	if err != nil {
		t.Fatalf("LoadFirtalGatewayRuntimeConfig() error = %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected explicit false to disable runtime")
	}
}

func TestWithFirtalGatewayDefaultsFillsRuntimeSafetyValues(t *testing.T) {
	cfg := withFirtalGatewayDefaults(FirtalGatewayRuntimeConfig{
		BaseURL: "https://registry.example/",
	})

	if cfg.BaseURL != "https://registry.example" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Model != defaultFirtalGatewayModel {
		t.Fatalf("Model = %q", cfg.Model)
	}
	if cfg.MaxTokens <= 0 || cfg.PollInterval <= 0 || cfg.SyncInterval <= 0 || cfg.TaskTimeout <= 0 {
		t.Fatalf("runtime defaults not filled: %+v", cfg)
	}
	if cfg.HistoryLimit != defaultFirtalGatewayHistoryLimit || cfg.MaxConcurrency != defaultFirtalGatewayMaxConcurrency {
		t.Fatalf("limits not filled: %+v", cfg)
	}
}
