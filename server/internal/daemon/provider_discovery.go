package daemon

import (
	"context"
	"encoding/json"
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
)

// probeAPIProviders discovers configured API and local-model providers. Hosted
// providers require their configured credential; local providers use their
// catalog default endpoint and may be keyless. A failed optional provider is
// omitted without affecting CLI discovery or daemon startup.
func probeAPIProviders() map[string]AgentEntry {
	env := providerEnvironment()
	providers := agent.ProviderCatalog()
	entries := make(map[string]AgentEntry)

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, desc := range providers {
		if desc.Kind != agent.ProviderKindOpenAICompatible && desc.Kind != agent.ProviderKindOpenCodeAPI {
			continue
		}
		desc := desc
		cfg, err := agent.ResolveProviderAPIConfig(desc.ID, env)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), apiProviderProbeTimeout)
			defer cancel()
			if err := probeAPIProviderEndpoint(ctx, desc.ID, cfg); err != nil {
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
	return entries
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
