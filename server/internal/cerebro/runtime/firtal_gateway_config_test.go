package runtime

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

func uuidFromString(s string) (pgtype.UUID, error) {
	return util.ParseUUID(s)
}

func TestLoadFirtalGatewayRuntimeConfig_AutoEnablesWithServerCredentials(t *testing.T) {
	clearFirtalGatewayEnv(t)
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
	clearFirtalGatewayEnv(t)
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

func TestLoadFirtalGatewayRuntimeConfig_EnablesWithoutServerCredentials(t *testing.T) {
	clearFirtalGatewayEnv(t)

	cfg, err := LoadFirtalGatewayRuntimeConfig()
	if err != nil {
		t.Fatalf("LoadFirtalGatewayRuntimeConfig() error = %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected runtime worker to enable so workspace settings can supply credentials")
	}
	if cfg.BaseURL != "" || cfg.APIKey != "" {
		t.Fatalf("unexpected credentials: %+v", cfg)
	}
}

func TestLoadFirtalGatewayRuntimeConfig_RejectsUnsafeServerURL(t *testing.T) {
	clearFirtalGatewayEnv(t)
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "https://127.0.0.1")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")

	if _, err := LoadFirtalGatewayRuntimeConfig(); err == nil {
		t.Fatal("expected unsafe server gateway URL to fail")
	}
}

func clearFirtalGatewayEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL",
		"FIRTAL_AE_GATEWAY_URL",
		"FIRTAL_DATA_REGISTRY_URL",
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY",
		"FIRTAL_AE_GATEWAY_KEY",
		"FIRTAL_DATA_REGISTRY_API_KEY",
		"FIRTAL_DATA_REGISTRY_AI_MODEL",
		"FIRTAL_AE_GATEWAY_MODEL",
		"MULTICA_SERVER_FIRTAL_GATEWAY_ENABLED",
		"MULTICA_FIRTAL_GATEWAY_CLOUD_ENABLED",
		"MULTICA_SERVER_FIRTAL_GATEWAY_MAX_CONCURRENCY",
		"MULTICA_SERVER_FIRTAL_GATEWAY_WORKSPACE_IDS",
		"MULTICA_SERVER_FIRTAL_GATEWAY_TOOLS_AGENTS",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadFirtalGatewayRuntimeConfig_ParsesToolsAgentsEnv(t *testing.T) {
	clearFirtalGatewayEnv(t)
	t.Setenv("MULTICA_SERVER_FIRTAL_GATEWAY_TOOLS_AGENTS", "6fe22e7e-6a81-4403-be26-fafd89871cf6, 43501ed6-0b4d-489b-b05e-e5d07e665d91")

	cfg, err := LoadFirtalGatewayRuntimeConfig()
	if err != nil {
		t.Fatalf("LoadFirtalGatewayRuntimeConfig() error = %v", err)
	}
	if len(cfg.ToolsEnabledAgentIDs) != 2 {
		t.Fatalf("ToolsEnabledAgentIDs = %+v", cfg.ToolsEnabledAgentIDs)
	}
	kristian, err := uuidFromString("6fe22e7e-6a81-4403-be26-fafd89871cf6")
	if err != nil {
		t.Fatalf("uuidFromString: %v", err)
	}
	if !cfg.ToolsEnabledForAgent(kristian) {
		t.Fatal("expected Kristian's UUID to be on the allowlist")
	}
	other, err := uuidFromString("00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("uuidFromString: %v", err)
	}
	if cfg.ToolsEnabledForAgent(other) {
		t.Fatal("expected non-listed agent to be off the allowlist")
	}
}

func TestLoadFirtalGatewayRuntimeConfig_RejectsInvalidToolsAgentsEnv(t *testing.T) {
	clearFirtalGatewayEnv(t)
	t.Setenv("MULTICA_SERVER_FIRTAL_GATEWAY_TOOLS_AGENTS", "not-a-uuid")

	if _, err := LoadFirtalGatewayRuntimeConfig(); err == nil {
		t.Fatal("expected invalid UUID in tools allowlist to fail")
	}
}

func TestFirtalGatewayConfigFromWorkspaceSettings_UsesOwnerSettings(t *testing.T) {
	raw := []byte(`{"firtal_gateway":{"enabled":true,"gateway_url":"https://registry.example/","api_key":"rk_workspace","model":"gpt-5.5"}}`)

	cfg, ok, err := FirtalGatewayConfigFromWorkspaceSettings(raw, FirtalGatewayRuntimeConfig{
		Enabled: true,
		BaseURL: "https://fallback.example",
		APIKey:  "rk_fallback",
		Model:   "fallback-model",
	})
	if err != nil {
		t.Fatalf("FirtalGatewayConfigFromWorkspaceSettings() error = %v", err)
	}
	if !ok {
		t.Fatal("expected configured workspace")
	}
	if cfg.BaseURL != "https://registry.example" || cfg.APIKey != "rk_workspace" || cfg.Model != "gpt-5.5" {
		t.Fatalf("workspace config not applied: %+v", cfg)
	}
}

func TestFirtalGatewayConfigFromWorkspaceSettings_FallsBackToServerCredentials(t *testing.T) {
	cfg, ok, err := FirtalGatewayConfigFromWorkspaceSettings([]byte(`{}`), FirtalGatewayRuntimeConfig{
		Enabled: true,
		BaseURL: "https://fallback.example",
		APIKey:  "rk_fallback",
	})
	if err != nil {
		t.Fatalf("FirtalGatewayConfigFromWorkspaceSettings() error = %v", err)
	}
	if !ok {
		t.Fatal("expected server fallback credentials to configure workspace")
	}
	if cfg.BaseURL != "https://fallback.example" || cfg.APIKey != "rk_fallback" {
		t.Fatalf("fallback config not used: %+v", cfg)
	}
}

func TestFirtalGatewayConfigFromWorkspaceSettings_RejectsUnsafeWorkspaceURL(t *testing.T) {
	_, ok, err := FirtalGatewayConfigFromWorkspaceSettings([]byte(`{"firtal_gateway":{"enabled":true,"gateway_url":"https://169.254.169.254","api_key":"rk_workspace"}}`), FirtalGatewayRuntimeConfig{
		Enabled: true,
	})
	if err == nil {
		t.Fatal("expected unsafe workspace gateway URL to fail")
	}
	if ok {
		t.Fatal("unsafe workspace should not be marked configured")
	}
}

func TestFirtalGatewayConfigFromWorkspaceSettings_DisabledWorkspaceSkipsRuntime(t *testing.T) {
	_, ok, err := FirtalGatewayConfigFromWorkspaceSettings([]byte(`{"firtal_gateway":{"enabled":false}}`), FirtalGatewayRuntimeConfig{
		Enabled: true,
		BaseURL: "https://fallback.example",
		APIKey:  "rk_fallback",
	})
	if err != nil {
		t.Fatalf("FirtalGatewayConfigFromWorkspaceSettings() error = %v", err)
	}
	if ok {
		t.Fatal("expected disabled workspace settings to skip runtime even with server fallback")
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
