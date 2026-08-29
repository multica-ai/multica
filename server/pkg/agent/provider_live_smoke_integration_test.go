//go:build agentintegration

package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestConfiguredAPIProviderSmoke exercises discovery and one real completion
// using only daemon-style environment configuration. It is opt-in because a
// hosted provider can consume quota.
func TestConfiguredAPIProviderSmoke(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_PROVIDER_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_PROVIDER_SMOKE=1 to allow live provider access")
	}

	provider := strings.TrimSpace(os.Getenv("MULTICA_PROVIDER_SMOKE_PROVIDER"))
	if provider == "" {
		provider = "ollama"
	}
	desc, ok := ProviderByID(provider)
	if !ok || !IsAPIProvider(provider) {
		t.Fatalf("provider %q is not a configured API provider", provider)
	}

	cfg, err := ResolveProviderAPIConfig(provider, providerSmokeEnv())
	if err != nil {
		t.Fatalf("resolve %s configuration: %v", provider, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	requestedModel := strings.TrimSpace(os.Getenv("MULTICA_PROVIDER_SMOKE_MODEL"))
	catalog, err := ListAPIModels(ctx, provider, cfg, requestedModel, nil)
	if err != nil {
		t.Fatalf("discover %s models: %v", provider, err)
	}
	if len(catalog.Models) == 0 {
		t.Fatalf("discover %s models returned no models", provider)
	}

	model := requestedModel
	if model == "" {
		model = catalog.Models[0].ID
	}
	if !containsProviderModel(catalog.Models, model) {
		t.Fatalf("provider %s did not advertise requested model %q", provider, model)
	}

	backend, err := New(provider, Config{
		APIBaseURL:   cfg.BaseURL,
		APIKey:       cfg.APIKey,
		DefaultModel: model,
	})
	if err != nil {
		t.Fatalf("new %s backend: %v", provider, err)
	}
	session, err := backend.Execute(ctx, "Reply with exactly MULTICA-PROVIDER-SMOKE-OK", ExecOptions{
		Model:   model,
		Timeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute %s: %v", provider, err)
	}

	var output strings.Builder
	for message := range session.Messages {
		if message.Type == MessageText {
			output.WriteString(message.Content)
		}
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("%s completion status = %q, error = %q", provider, result.Status, result.Error)
	}
	if !strings.Contains(output.String(), "MULTICA-PROVIDER-SMOKE-OK") && !strings.Contains(result.Output, "MULTICA-PROVIDER-SMOKE-OK") {
		t.Fatalf("%s output did not contain the smoke marker", provider)
	}
	t.Logf("live provider smoke passed: provider=%s model=%s endpoint=%s", provider, model, desc.DefaultBaseURL)
}

func providerSmokeEnv() map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func containsProviderModel(models []Model, want string) bool {
	for _, model := range models {
		if model.ID == want {
			return true
		}
	}
	return false
}
