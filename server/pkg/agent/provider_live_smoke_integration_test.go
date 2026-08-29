//go:build agentintegration

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	providerSmokeProviderEnv       = "MULTICA_PROVIDER_SMOKE_PROVIDER"
	providerSmokeModelEnv          = "MULTICA_PROVIDER_SMOKE_MODEL"
	providerSmokeMarker            = "MULTICA-PROVIDER-SMOKE-OK"
	providerSmokeSecretLeakMessage = "live provider smoke diagnostics contained the configured credential"
)

var errProviderSmokeSecretLeak = errors.New(providerSmokeSecretLeakMessage)

// TestConfiguredAPIProviderSmoke exercises discovery and one real completion
// using only daemon-style environment configuration. It is opt-in because a
// hosted provider can consume quota.
func TestConfiguredAPIProviderSmoke(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_PROVIDER_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_PROVIDER_SMOKE=1 to allow live provider access")
	}

	provider, model, err := providerSmokeSelection(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	desc, ok := ProviderByID(provider)
	if !ok || !IsAPIProvider(provider) {
		t.Fatalf("provider %q is not a configured API provider", provider)
	}

	env, err := providerSmokeEnv(provider, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveProviderAPIConfig(provider, env)
	if err != nil {
		t.Fatalf("resolve %s configuration: %v", provider, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := newProviderSmokeHTTPClient()
	t.Cleanup(providerSmokeTransportCleanup(client))

	catalog, err := ListAPIModels(ctx, provider, cfg, model, client)
	if err != nil {
		if leakErr := providerSmokeLeakError(cfg.APIKey, nil, Result{Error: err.Error()}, ""); leakErr != nil {
			t.Fatal(leakErr)
		}
		t.Fatalf("discover %s models: %s", provider, sanitizeProviderOutput(err.Error(), cfg.APIKey))
	}
	if len(catalog.Models) == 0 {
		t.Fatalf("discover %s models returned no models", provider)
	}
	if !containsProviderModel(catalog.Models, model) {
		t.Fatalf("provider %s did not advertise requested model %q", provider, sanitizeProviderOutput(model, cfg.APIKey))
	}

	var logs providerSmokeLogCapture
	backend, err := New(provider, Config{
		APIBaseURL:   cfg.BaseURL,
		APIKey:       cfg.APIKey,
		HTTPClient:   client,
		DefaultModel: model,
		Logger:       slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		if leakErr := providerSmokeLeakError(cfg.APIKey, nil, Result{Error: err.Error()}, logs.String()); leakErr != nil {
			t.Fatal(leakErr)
		}
		t.Fatalf("new %s backend: %s", provider, sanitizeProviderOutput(err.Error(), cfg.APIKey))
	}
	startedAt := time.Now()
	session, err := backend.Execute(ctx, "Reply with exactly "+providerSmokeMarker, ExecOptions{
		Model:   model,
		Timeout: 90 * time.Second,
	})
	if err != nil {
		if leakErr := providerSmokeLeakError(cfg.APIKey, nil, Result{Error: err.Error()}, logs.String()); leakErr != nil {
			t.Fatal(leakErr)
		}
		t.Fatalf("execute %s: %s", provider, sanitizeProviderOutput(err.Error(), cfg.APIKey))
	}

	messages, result, drainErr := drainProviderSmokeSession(ctx, session)
	if leakErr := providerSmokeLeakError(cfg.APIKey, messages, result, logs.String()); leakErr != nil {
		t.Fatal(leakErr)
	}
	if drainErr != nil {
		t.Fatalf("drain %s session: %v", provider, drainErr)
	}

	var output strings.Builder
	streamed := false
	for _, message := range messages {
		if message.Type == MessageText {
			streamed = true
			output.WriteString(message.Content)
		}
	}
	if result.Status != "completed" {
		t.Fatalf("%s completion status = %q, error = %q", provider, result.Status, sanitizeProviderOutput(result.Error, cfg.APIKey))
	}
	if !streamed {
		t.Fatalf("%s completion emitted no streamed text messages", provider)
	}
	if !strings.Contains(output.String(), providerSmokeMarker) && !strings.Contains(result.Output, providerSmokeMarker) {
		t.Fatalf("%s output did not contain the smoke marker", provider)
	}
	t.Logf(
		"live provider smoke passed: provider=%s model=%s streamed=%t status=%s marker=true duration=%s",
		desc.ID,
		sanitizeProviderOutput(model, cfg.APIKey),
		streamed,
		result.Status,
		time.Since(startedAt).Round(time.Millisecond),
	)
}

func providerSmokeSelection(lookup func(string) string) (string, string, error) {
	provider := strings.TrimSpace(lookup(providerSmokeProviderEnv))
	if provider == "" {
		return "", "", fmt.Errorf("%s is required", providerSmokeProviderEnv)
	}
	model := strings.TrimSpace(lookup(providerSmokeModelEnv))
	if model == "" {
		return "", "", fmt.Errorf("%s is required", providerSmokeModelEnv)
	}
	return provider, model, nil
}

func providerSmokeEnv(provider string, lookup func(string) string) (map[string]string, error) {
	desc, ok := ProviderByID(provider)
	if !ok || !IsAPIProvider(provider) {
		return nil, fmt.Errorf("provider %q is not a configured API provider", provider)
	}
	keys := append([]string{desc.BaseURLEnv, desc.APIKeyEnv}, desc.OptionalKeyEnv...)
	env := make(map[string]string, len(keys))
	for _, key := range keys {
		env[key] = lookup(key)
	}
	return env, nil
}

func newProviderSmokeHTTPClient() *http.Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	return &http.Client{Transport: base.Clone()}
}

func providerSmokeTransportCleanup(client *http.Client) func() {
	return func() {
		if client != nil {
			client.CloseIdleConnections()
		}
	}
}

func drainProviderSmokeSession(ctx context.Context, session *Session) ([]Message, Result, error) {
	if session == nil {
		return nil, Result{}, errors.New("live provider smoke returned a nil session")
	}
	messagesCh := session.Messages
	resultCh := session.Result
	messages := make([]Message, 0)
	var result Result
	resultCount := 0
	for messagesCh != nil || resultCh != nil {
		select {
		case <-ctx.Done():
			return messages, result, fmt.Errorf("live provider smoke session drain: %w", ctx.Err())
		case message, ok := <-messagesCh:
			if !ok {
				messagesCh = nil
				continue
			}
			messages = append(messages, message)
		case final, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}
			resultCount++
			if resultCount == 1 {
				result = final
			}
		}
	}
	if resultCount != 1 {
		return messages, result, fmt.Errorf("live provider smoke returned %d results, want exactly one", resultCount)
	}
	return messages, result, nil
}

func providerSmokeLeakError(secret string, messages []Message, result Result, logs string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	payload, err := json.Marshal(struct {
		Messages []Message `json:"messages"`
		Result   Result    `json:"result"`
	}{Messages: messages, Result: result})
	if err != nil || strings.Contains(string(payload), secret) || strings.Contains(logs, secret) {
		return errProviderSmokeSecretLeak
	}
	return nil
}

type providerSmokeLogCapture struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (c *providerSmokeLogCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.Write(data)
}

func (c *providerSmokeLogCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}

func containsProviderModel(models []Model, want string) bool {
	for _, model := range models {
		if model.ID == want {
			return true
		}
	}
	return false
}
