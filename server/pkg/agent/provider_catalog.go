package agent

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ProviderKind identifies the transport a runtime uses.
type ProviderKind string

const (
	ProviderKindCLI              ProviderKind = "cli"
	ProviderKindOpenAICompatible ProviderKind = "openai-compatible"
	// ProviderKindOpenCodeAPI identifies the OpenCode-hosted API family. It is
	// kept distinct from generic OpenAI-compatible providers because Console,
	// Zen, and Go have separate credentials and endpoints, and Zen/Go expose
	// more than one wire protocol by model.
	ProviderKindOpenCodeAPI ProviderKind = "opencode-api"
)

// ProviderCapability describes a behavior that has been verified for a
// provider family. A catalog entry without a capability is not permission to
// assume that behavior at a boundary such as model selection or MCP setup.
type ProviderCapability string

const (
	ProviderCapabilityPrompt         ProviderCapability = "prompt"
	ProviderCapabilityStreaming      ProviderCapability = "streaming"
	ProviderCapabilityCompletion     ProviderCapability = "completion"
	ProviderCapabilityCancellation   ProviderCapability = "cancellation"
	ProviderCapabilityModelDiscovery ProviderCapability = "model-discovery"
	ProviderCapabilityReasoning      ProviderCapability = "reasoning"
	ProviderCapabilityUsage          ProviderCapability = "usage"
	ProviderCapabilityTools          ProviderCapability = "tools"
	ProviderCapabilityMCP            ProviderCapability = "mcp"
	ProviderCapabilityResume         ProviderCapability = "resume"
)

// ProviderDescriptor is the canonical cross-stack description of one runtime
// provider. CLI providers use the existing native backend. API providers use
// the shared OpenAI-compatible adapter and never require a local executable.
type ProviderDescriptor struct {
	ID             string
	DisplayName    string
	Kind           ProviderKind
	LaunchHeader   string
	DefaultBaseURL string
	BaseURLEnv     string
	APIKeyEnv      string
	OptionalKeyEnv []string
	RequiresKey    bool
	LocalOnly      bool
	Capabilities   []ProviderCapability
}

// ProviderAPIConfig is the resolved endpoint configuration for one API
// provider. Credentials are kept in memory for the duration of a task and are
// never included in runtime registration payloads or logs.
type ProviderAPIConfig struct {
	BaseURL string
	APIKey  string
}

var providerCatalog = []ProviderDescriptor{
	{ID: "codex", DisplayName: "ChatGPT subscription", Kind: ProviderKindCLI, LaunchHeader: "codex app-server", Capabilities: nativeProviderCapabilities(true)},
	{ID: "claude", DisplayName: "Claude subscription", Kind: ProviderKindCLI, LaunchHeader: "claude (stream-json)", Capabilities: nativeProviderCapabilities(true)},
	{ID: "antigravity", DisplayName: "Google Antigravity subscription", Kind: ProviderKindCLI, LaunchHeader: "agy -p (non-interactive)", Capabilities: nativeProviderCapabilities(false)},
	{ID: "cursor", DisplayName: "Cursor subscription", Kind: ProviderKindCLI, LaunchHeader: "cursor-agent (stream-json)", Capabilities: nativeProviderCapabilities(true)},
	{ID: "grok", DisplayName: "Grok subscription", Kind: ProviderKindCLI, LaunchHeader: "grok agent stdio", Capabilities: nativeProviderCapabilities(true)},
	{ID: "opencode", DisplayName: "OpenCode CLI", Kind: ProviderKindCLI, LaunchHeader: "opencode run (json)", Capabilities: nativeProviderCapabilities(true)},
	{
		ID: "opencode-api", DisplayName: "OpenCode Console API", Kind: ProviderKindOpenCodeAPI,
		LaunchHeader: "OpenCode Console inference API", DefaultBaseURL: "https://opencode.ai/inference/openai/v1",
		BaseURLEnv: "OPENCODE_API_BASE_URL", APIKeyEnv: "OPENCODE_API_KEY", OptionalKeyEnv: []string{"OPENCODE_API_TOKEN"}, RequiresKey: true,
		Capabilities: apiProviderCapabilities(),
	},
	{
		ID: "opencode-zen", DisplayName: "OpenCode Zen", Kind: ProviderKindOpenCodeAPI,
		LaunchHeader: "OpenCode Zen API", DefaultBaseURL: "https://opencode.ai/zen/v1",
		BaseURLEnv: "OPENCODE_ZEN_BASE_URL", APIKeyEnv: "OPENCODE_ZEN_API_KEY", OptionalKeyEnv: []string{"OPENCODE_ZEN_TOKEN"}, RequiresKey: true,
		Capabilities: apiProviderCapabilities(),
	},
	{
		ID: "opencode-go", DisplayName: "OpenCode Go", Kind: ProviderKindOpenCodeAPI,
		LaunchHeader: "OpenCode Go API", DefaultBaseURL: "https://opencode.ai/zen/go/v1",
		BaseURLEnv: "OPENCODE_GO_BASE_URL", APIKeyEnv: "OPENCODE_GO_API_KEY", OptionalKeyEnv: []string{"OPENCODE_GO_TOKEN"}, RequiresKey: true,
		Capabilities: apiProviderCapabilities(),
	},
	{
		ID: "openrouter", DisplayName: "OpenRouter", Kind: ProviderKindOpenAICompatible,
		LaunchHeader: "OpenRouter API", DefaultBaseURL: "https://openrouter.ai/api/v1",
		BaseURLEnv: "OPENROUTER_BASE_URL", APIKeyEnv: "OPENROUTER_API_KEY", RequiresKey: true,
		Capabilities: apiProviderCapabilities(),
	},
	{
		ID: "vercel-ai-gateway", DisplayName: "Vercel AI Gateway", Kind: ProviderKindOpenAICompatible,
		LaunchHeader: "Vercel AI Gateway API", DefaultBaseURL: "https://ai-gateway.vercel.sh/v1",
		BaseURLEnv: "AI_GATEWAY_BASE_URL", APIKeyEnv: "AI_GATEWAY_API_KEY", OptionalKeyEnv: []string{"VERCEL_OIDC_TOKEN"}, RequiresKey: true,
		Capabilities: apiProviderCapabilities(),
	},
	{
		ID: "ollama", DisplayName: "Ollama", Kind: ProviderKindOpenAICompatible,
		LaunchHeader: "Ollama OpenAI-compatible API", DefaultBaseURL: "http://127.0.0.1:11434/v1",
		BaseURLEnv: "OLLAMA_BASE_URL", APIKeyEnv: "OLLAMA_API_KEY", RequiresKey: false, LocalOnly: true,
		Capabilities: apiProviderCapabilities(),
	},
	{
		ID: "lmstudio", DisplayName: "LM Studio", Kind: ProviderKindOpenAICompatible,
		LaunchHeader: "LM Studio OpenAI-compatible API", DefaultBaseURL: "http://127.0.0.1:1234/v1",
		BaseURLEnv: "LMSTUDIO_BASE_URL", APIKeyEnv: "LMSTUDIO_API_KEY", RequiresKey: false, LocalOnly: true,
		Capabilities: apiProviderCapabilities(),
	},
	{
		ID: "nvidia-nim", DisplayName: "NVIDIA NIM", Kind: ProviderKindOpenAICompatible,
		LaunchHeader: "NVIDIA NIM OpenAI API", DefaultBaseURL: "https://integrate.api.nvidia.com/v1",
		BaseURLEnv: "NVIDIA_NIM_BASE_URL", APIKeyEnv: "NVIDIA_API_KEY", RequiresKey: true,
		Capabilities: apiProviderCapabilities(),
	},
}

func nativeProviderCapabilities(mcp bool) []ProviderCapability {
	capabilities := []ProviderCapability{
		ProviderCapabilityPrompt,
		ProviderCapabilityStreaming,
		ProviderCapabilityCompletion,
		ProviderCapabilityCancellation,
		ProviderCapabilityModelDiscovery,
	}
	if mcp {
		capabilities = append(capabilities, ProviderCapabilityMCP)
	}
	return capabilities
}

func apiProviderCapabilities() []ProviderCapability {
	return []ProviderCapability{
		ProviderCapabilityPrompt,
		ProviderCapabilityStreaming,
		ProviderCapabilityCompletion,
		ProviderCapabilityCancellation,
		ProviderCapabilityModelDiscovery,
	}
}

func cloneProviderDescriptor(desc ProviderDescriptor) ProviderDescriptor {
	desc.OptionalKeyEnv = append([]string(nil), desc.OptionalKeyEnv...)
	desc.Capabilities = append([]ProviderCapability(nil), desc.Capabilities...)
	return desc
}

// ProviderCatalog returns a defensive copy of the canonical provider catalog.
func ProviderCatalog() []ProviderDescriptor {
	result := make([]ProviderDescriptor, len(providerCatalog))
	for i, desc := range providerCatalog {
		result[i] = cloneProviderDescriptor(desc)
	}
	return result
}

// ProviderByID returns the descriptor for id.
func ProviderByID(id string) (ProviderDescriptor, bool) {
	for _, desc := range providerCatalog {
		if desc.ID == id {
			return cloneProviderDescriptor(desc), true
		}
	}
	return ProviderDescriptor{}, false
}

// ProviderSupportsCapability reports whether the catalog has verified a
// capability for provider. Unknown providers and unverified capabilities are
// false, so callers fail closed when a provider is only descriptive metadata.
func ProviderSupportsCapability(provider string, capability ProviderCapability) bool {
	desc, ok := ProviderByID(provider)
	if !ok {
		return false
	}
	for _, supported := range desc.Capabilities {
		if supported == capability {
			return true
		}
	}
	return false
}

// IsProviderType reports whether id is either an existing CLI backend or a
// first-class API provider. CLI profile validation should continue using
// IsSupportedType, which intentionally excludes API-only providers.
func IsProviderType(id string) bool {
	if IsSupportedType(id) {
		return true
	}
	_, ok := ProviderByID(id)
	return ok
}

// IsAPIProvider reports whether id is backed by an HTTP API adapter.
func IsAPIProvider(id string) bool {
	desc, ok := ProviderByID(id)
	return ok && (desc.Kind == ProviderKindOpenAICompatible || desc.Kind == ProviderKindOpenCodeAPI)
}

// ProviderCredentialEnvAllowed reports whether envName is one of the
// descriptor-owned credential variable names for provider. A profile may store
// this name, but never the value it references.
func ProviderCredentialEnvAllowed(provider, envName string) bool {
	desc, ok := ProviderByID(provider)
	if !ok || !IsAPIProvider(provider) {
		return false
	}
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return false
	}
	if envName == desc.APIKeyEnv {
		return true
	}
	for _, optional := range desc.OptionalKeyEnv {
		if envName == optional {
			return true
		}
	}
	return false
}

// ValidateProviderAPIBaseURL validates the scheme and local-provider policy for
// a profile-supplied endpoint. Credential host binding happens later, when a
// daemon-owned credential is resolved and operator trust provenance is known.
func ValidateProviderAPIBaseURL(provider, raw string) error {
	desc, ok := ProviderByID(provider)
	if !ok || !IsAPIProvider(provider) {
		return fmt.Errorf("provider %q is not an API provider", provider)
	}
	return validateAPIBaseURL(raw, desc.LocalOnly)
}

// ResolveProviderAPIProfileConfig resolves an API runtime profile using only
// the daemon's process environment. The profile can select an approved
// credential variable name and endpoint, while the secret value remains local.
func ResolveProviderAPIProfileConfig(provider string, env map[string]string, baseURL, credentialEnv string) (ProviderAPIConfig, error) {
	return ResolveProviderAPIProfileConfigWithTrustedHostOverride(provider, env, baseURL, credentialEnv, false)
}

// ResolveProviderAPIProfileConfigWithTrustedHostOverride resolves a profile
// with an explicit operator-owned trust decision. Environment input cannot
// enable this override; callers must pass true from a separate trusted control
// plane. Ordinary profile resolution always passes false.
func ResolveProviderAPIProfileConfigWithTrustedHostOverride(
	provider string,
	env map[string]string,
	baseURL string,
	credentialEnv string,
	trustedHostOverride bool,
) (ProviderAPIConfig, error) {
	desc, ok := ProviderByID(provider)
	if !ok || !IsAPIProvider(provider) {
		return ProviderAPIConfig{}, fmt.Errorf("provider %q is not an API provider", provider)
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = desc.DefaultBaseURL
	}
	if err := validateProviderAPIBaseURL(provider, baseURL, trustedHostOverride); err != nil {
		return ProviderAPIConfig{}, err
	}

	credentialEnv = strings.TrimSpace(credentialEnv)
	if credentialEnv == "" {
		credentialEnv = desc.APIKeyEnv
	}
	if credentialEnv != "" && !ProviderCredentialEnvAllowed(provider, credentialEnv) {
		return ProviderAPIConfig{}, fmt.Errorf("provider %q does not allow credential environment %q", provider, credentialEnv)
	}
	key := strings.TrimSpace(env[credentialEnv])
	if credentialEnv == desc.APIKeyEnv && key == "" {
		for _, optional := range desc.OptionalKeyEnv {
			if value := strings.TrimSpace(env[optional]); value != "" {
				key = value
				break
			}
		}
	}
	if desc.RequiresKey && key == "" {
		return ProviderAPIConfig{}, fmt.Errorf("provider %q requires %s", provider, credentialEnv)
	}
	return ProviderAPIConfig{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: key}, nil
}

// ResolveProviderAPIConfig resolves task-scoped environment values and applies
// the descriptor default endpoint. The caller supplies the effective task
// environment so a per-agent credential wins over the daemon process.
func ResolveProviderAPIConfig(provider string, env map[string]string) (ProviderAPIConfig, error) {
	return ResolveProviderAPIConfigWithTrustedHostOverride(provider, env, false)
}

// ResolveProviderAPIConfigWithTrustedHostOverride resolves a built-in API
// provider with an explicit operator-owned trust decision. Environment input
// supplies endpoint and credential values only and cannot self-authorize a
// different hosted origin.
func ResolveProviderAPIConfigWithTrustedHostOverride(provider string, env map[string]string, trustedHostOverride bool) (ProviderAPIConfig, error) {
	desc, ok := ProviderByID(provider)
	if !ok || (desc.Kind != ProviderKindOpenAICompatible && desc.Kind != ProviderKindOpenCodeAPI) {
		return ProviderAPIConfig{}, fmt.Errorf("provider %q is not an API provider", provider)
	}
	baseURL := strings.TrimSpace(env[desc.BaseURLEnv])
	if baseURL == "" {
		baseURL = desc.DefaultBaseURL
	}
	key := strings.TrimSpace(env[desc.APIKeyEnv])
	if key == "" {
		for _, name := range desc.OptionalKeyEnv {
			if value := strings.TrimSpace(env[name]); value != "" {
				key = value
				break
			}
		}
	}
	if err := validateProviderAPIBaseURL(provider, baseURL, trustedHostOverride); err != nil {
		return ProviderAPIConfig{}, fmt.Errorf("provider %q: %w", provider, err)
	}
	if desc.RequiresKey && key == "" {
		return ProviderAPIConfig{}, fmt.Errorf("provider %q requires %s", provider, desc.APIKeyEnv)
	}
	return ProviderAPIConfig{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: key}, nil
}

func validateProviderAPIBaseURL(provider, raw string, trustedHostOverride bool) error {
	desc, ok := ProviderByID(provider)
	if !ok || !IsAPIProvider(provider) {
		return fmt.Errorf("provider %q is not an API provider", provider)
	}
	if err := validateAPIBaseURL(raw, desc.LocalOnly); err != nil {
		return err
	}
	if desc.LocalOnly || trustedHostOverride {
		return nil
	}

	configured, _ := url.Parse(strings.TrimSpace(raw))
	trusted, _ := url.Parse(desc.DefaultBaseURL)
	if strings.EqualFold(configured.Host, trusted.Host) {
		return nil
	}
	return fmt.Errorf("provider %q base URL uses an untrusted host", provider)
}

func validateAPIBaseURL(raw string, localOnly bool) error {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "\\") {
		return fmt.Errorf("base URL must not contain backslashes")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("base URL must be an absolute HTTP(S) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base URL must use http or https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("base URL must not contain credentials, query parameters, or fragments")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == ".." {
			return fmt.Errorf("base URL must not contain parent path segments")
		}
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("HTTP base URLs are only allowed on loopback hosts")
	}
	if localOnly && (u.Scheme != "http" || !isLoopbackHost(u.Hostname())) {
		return fmt.Errorf("local provider base URL must use loopback http")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
