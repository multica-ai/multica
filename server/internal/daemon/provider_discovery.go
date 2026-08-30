package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const (
	apiProviderProbeTimeout = 3 * time.Second
	apiProviderProbeMaxBody = 1 << 20
)

var (
	providerProbeClient = &http.Client{
		// Discovery carries a daemon-owned bearer credential. A provider endpoint
		// must never be able to redirect the probe to another origin with that
		// credential attached.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// probeAPIProviderEndpoint is injectable so daemon discovery tests remain
	// local and deterministic. Production uses the provider's bounded /models
	// endpoint, which validates both reachability and model-catalog shape.
	probeAPIProviderEndpoint = defaultProbeAPIProviderEndpoint

	apiProviderProbeFailuresMu sync.RWMutex
	apiProviderProbeFailures   map[string]string
)

// probeAPIProviders discovers configured API and local-model providers. Hosted
// providers require their configured credential; local providers use their
// catalog default endpoint and may be keyless. A failed optional provider is
// omitted without affecting CLI discovery or daemon startup.
func probeAPIProviders() map[string]AgentEntry {
	env := providerEnvironment()
	providers := agent.ProviderCatalog()
	entries := make(map[string]AgentEntry)
	failures := make(map[string]string)

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, desc := range providers {
		if desc.Kind != agent.ProviderKindOpenAICompatible && desc.Kind != agent.ProviderKindOpenCodeAPI {
			continue
		}
		desc := desc
		explicitlyConfigured := providerExplicitlyConfigured(desc, env)
		cfg, err := agent.ResolveProviderAPIConfig(desc.ID, env)
		if err != nil {
			if explicitlyConfigured {
				mu.Lock()
				failures[desc.ID] = sanitizedProviderOfflineReason(err)
				mu.Unlock()
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), apiProviderProbeTimeout)
			defer cancel()
			if err := probeAPIProviderEndpoint(ctx, desc.ID, cfg); err != nil {
				if explicitlyConfigured {
					mu.Lock()
					failures[desc.ID] = sanitizedProviderOfflineReason(err)
					mu.Unlock()
				}
				return
			}
			entry := AgentEntry{
				Model:      strings.TrimSpace(env[apiProviderModelEnv(desc.ID)]),
				APIBaseURL: cfg.BaseURL,
				apiKey:     cfg.APIKey,
			}
			mu.Lock()
			entries[desc.ID] = entry
			mu.Unlock()
		}()
	}
	wg.Wait()
	publishAPIProviderProbeFailures(failures)
	return entries
}

func providerExplicitlyConfigured(desc agent.ProviderDescriptor, env map[string]string) bool {
	if strings.TrimSpace(env[desc.BaseURLEnv]) != "" || strings.TrimSpace(env[desc.APIKeyEnv]) != "" {
		return true
	}
	for _, name := range desc.OptionalKeyEnv {
		if strings.TrimSpace(env[name]) != "" {
			return true
		}
	}
	return false
}

func publishAPIProviderProbeFailures(failures map[string]string) {
	copyOfFailures := make(map[string]string, len(failures))
	for provider, reason := range failures {
		copyOfFailures[provider] = reason
	}
	apiProviderProbeFailuresMu.Lock()
	apiProviderProbeFailures = copyOfFailures
	apiProviderProbeFailuresMu.Unlock()
}

func apiProviderProbeFailuresSnapshot() map[string]string {
	apiProviderProbeFailuresMu.RLock()
	defer apiProviderProbeFailuresMu.RUnlock()
	if len(apiProviderProbeFailures) == 0 {
		return nil
	}
	result := make(map[string]string, len(apiProviderProbeFailures))
	for provider, reason := range apiProviderProbeFailures {
		result[provider] = reason
	}
	return result
}

func sanitizedProviderOfflineReason(err error) string {
	if err == nil {
		return "provider unavailable"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "provider probe timed out"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "requires "):
		return "provider credential is not configured"
	case strings.Contains(message, "untrusted host"),
		strings.Contains(message, "base url"),
		strings.Contains(message, "not an api provider"):
		return "provider configuration is invalid"
	case strings.Contains(message, "no data catalog"),
		strings.Contains(message, "not json"),
		strings.Contains(message, "exceeds"):
		return "provider model catalog is invalid"
	case strings.Contains(message, "http "):
		return "provider probe returned an error"
	default:
		return "provider endpoint is unavailable"
	}
}

func providerEnvironment() map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func apiProviderModelEnv(provider string) string {
	return "MULTICA_" + strings.NewReplacer("-", "_").Replace(strings.ToUpper(provider)) + "_MODEL"
}

func defaultProbeAPIProviderEndpoint(ctx context.Context, provider string, cfg agent.ProviderAPIConfig) error {
	desc, ok := agent.ProviderByID(provider)
	if !ok || !agent.IsAPIProvider(provider) {
		return fmt.Errorf("provider %q is not an API provider", provider)
	}
	validated, err := agent.ResolveProviderAPIProfileConfig(
		provider,
		map[string]string{desc.APIKeyEnv: cfg.APIKey},
		cfg.BaseURL,
		desc.APIKeyEnv,
	)
	if err != nil {
		return fmt.Errorf("provider %q configuration: %w", provider, err)
	}
	cfg = validated
	modelsURL, err := modelsEndpoint(cfg.BaseURL)
	if err != nil {
		return fmt.Errorf("provider %q models endpoint: %w", provider, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return fmt.Errorf("provider %q models request: %w", provider, err)
	}
	req.Header.Set("Accept", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := providerProbeClient.Do(req)
	if err != nil {
		return fmt.Errorf("provider %q models probe: %w", provider, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiProviderProbeMaxBody+1))
	if err != nil {
		return fmt.Errorf("provider %q models response: %w", provider, err)
	}
	if len(body) > apiProviderProbeMaxBody {
		return fmt.Errorf("provider %q models response exceeds %d bytes", provider, apiProviderProbeMaxBody)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("provider %q models probe returned HTTP %d", provider, resp.StatusCode)
	}
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("provider %q models response is not JSON: %w", provider, err)
	}
	if len(payload.Data) == 0 || string(payload.Data) == "null" {
		return fmt.Errorf("provider %q models response has no data catalog", provider)
	}
	return nil
}

func modelsEndpoint(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if strings.Contains(baseURL, "\\") {
		return "", fmt.Errorf("base URL must not contain backslashes")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("base URL must be an absolute HTTP(S) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("base URL must not contain credentials, query parameters, or fragments")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("base URL must not contain parent path segments")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
