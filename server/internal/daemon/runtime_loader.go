package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RuntimeManifest describes a user-installed agent runtime extension.
// It mirrors the AionUi extension manifest pattern: a JSON file that
// declares an ACP-compatible CLI and its capabilities, discovered by
// scanning ~/.multica/runtimes/*/runtime.json at daemon startup.
//
// Schema version: v1
type RuntimeManifest struct {
	SchemaVersion   int                       `json:"schema_version,omitempty"`
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Version         string                    `json:"version"`
	Description     string                    `json:"description,omitempty"`
	Provider        string                    `json:"provider"`
	Transport       string                    `json:"transport"` // "acp-stdio" or "stream-json"
	LaunchHeader    string                    `json:"launch_header,omitempty"`
	Command         RuntimeManifestCommand    `json:"command"`
	Capabilities    *RuntimeManifestCaps      `json:"capabilities,omitempty"`
	Models          []RuntimeManifestModel    `json:"models,omitempty"`
	ModelsDiscovery *ModelsDiscoveryConfig    `json:"models_discovery,omitempty"`
	ConfigFile      string                    `json:"config_file,omitempty"` // "AGENTS.md", "CLAUDE.md", "" to skip
	SkillsRoot      string                    `json:"skills_root,omitempty"` // relative to home or absolute
	IconURL         string                    `json:"icon_url,omitempty"`    // remote URL for the provider icon
	Pricing         map[string]RuntimePricing `json:"pricing,omitempty"`
	MinCLIVersion   string                    `json:"min_cli_version,omitempty"` // e.g. "0.2.0"
	Env             map[string]string         `json:"env,omitempty"`
}

// ModelsDiscoveryConfig declares how the runtime discovers available models
// at runtime (via CLI command, ACP session/new handshake, or a provider-local
// product catalog). When present, the daemon prefers dynamic discovery over
// the static `models` array; the static list serves as a fallback if discovery
// fails.
type ModelsDiscoveryConfig struct {
	// Method is "cli", "acp", or "codebuddy-product". When omitted,
	// auto-inferred from the manifest transport: stream-json → "cli",
	// acp-stdio → "acp".
	Method string `json:"method,omitempty"`
	// CLI is populated when method="cli" (stream-json transports).
	CLI *CLIDiscoveryConfig `json:"cli,omitempty"`
	// CacheTTLSeconds controls how long a successful discovery result is
	// cached. 0 or omitted → use daemon default (60s).
	CacheTTLSeconds int `json:"cache_ttl_seconds,omitempty"`
}

// CLIDiscoveryConfig holds configuration for CLI-based model discovery.
// The daemon shells out to `command.executable <args>` and parses stdout
// as a JSON object containing a `models` array.
type CLIDiscoveryConfig struct {
	// Args appended to command.executable for model discovery.
	// Example: ["--list-models", "--format", "json"]
	Args []string `json:"args"`
	// TimeoutSeconds caps the CLI execution. 0 → 15s default.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// RuntimeManifestCommand describes how to launch the ACP-compatible CLI.
type RuntimeManifestCommand struct {
	Executable         string            `json:"executable"`
	Args               []string          `json:"args,omitempty"`
	BlockedArgs        map[string]string `json:"blocked_args,omitempty"`         // flag → "flag" or "value"
	ExtraArgsAllowlist []string          `json:"extra_args_allowlist,omitempty"` // daemon-level args allowed through
}

// RuntimeManifestCaps declares which optional features the runtime supports.
// Capabilities are advisory: declaring `thinking: true` does not magically
// teach the daemon a new flag — it tells the wire layer to forward
// `opts.ThinkingLevel` instead of dropping it. Capabilities the daemon does
// not yet read are reserved for forward compatibility (frontend filtering).
type RuntimeManifestCaps struct {
	Thinking           bool `json:"thinking,omitempty"`
	McpConfig          bool `json:"mcp_config,omitempty"`
	InlineSystemPrompt bool `json:"inline_system_prompt,omitempty"`
	SessionResume      bool `json:"session_resume,omitempty"`
	MaxTurns           bool `json:"max_turns,omitempty"`
	ModelSelection     bool `json:"model_selection,omitempty"`
	LocalSkills        bool `json:"local_skills,omitempty"`
	SlashCommands      bool `json:"slash_commands,omitempty"`
	ToolCalls          bool `json:"tool_calls,omitempty"`
	Attachments        bool `json:"attachments,omitempty"`
	ImageInput         bool `json:"image_input,omitempty"`
	WebSearch          bool `json:"web_search,omitempty"`
	CustomArgs         bool `json:"custom_args,omitempty"`
	ExtraArgs          bool `json:"extra_args,omitempty"`
}

// RuntimeManifestModel describes a model exposed by the runtime.
type RuntimeManifestModel struct {
	ID       string   `json:"id"`
	Label    string   `json:"label,omitempty"`
	Default  bool     `json:"default,omitempty"`
	Thinking []string `json:"thinking,omitempty"` // e.g. ["none","low","medium","high"]
}

// RuntimePricing is per-model pricing in USD per million tokens.
type RuntimePricing struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
}

const defaultRuntimeManifestSchemaVersion = 1

// SupportedRuntimeManifestSchemaVersions lists the runtime.json schemas this
// daemon can safely execute. Missing schema_version is treated as v1 so older
// manifests keep loading.
var SupportedRuntimeManifestSchemaVersions = map[int]struct{}{
	defaultRuntimeManifestSchemaVersion: {},
}

// SupportedTransports lists the transport strings the daemon understands. A
// manifest declaring an unknown transport is rejected at load time rather
// than at task spawn time so misconfiguration surfaces during startup.
var SupportedTransports = map[string]struct{}{
	"acp-stdio":   {},
	"stream-json": {},
}

func runtimeManifestSchemaVersion(m RuntimeManifest) int {
	if m.SchemaVersion == 0 {
		return defaultRuntimeManifestSchemaVersion
	}
	return m.SchemaVersion
}

func isSupportedRuntimeManifestSchemaVersion(version int) bool {
	_, ok := SupportedRuntimeManifestSchemaVersions[version]
	return ok
}

func supportedRuntimeManifestSchemaVersionsLabel() string {
	versions := make([]string, 0, len(SupportedRuntimeManifestSchemaVersions))
	for v := range SupportedRuntimeManifestSchemaVersions {
		versions = append(versions, fmt.Sprintf("%d", v))
	}
	sort.Strings(versions)
	return strings.Join(versions, ", ")
}

func isSupportedRuntimeTransport(transport string) bool {
	_, ok := SupportedTransports[transport]
	return ok
}

func supportedRuntimeTransportsLabel() string {
	supported := make([]string, 0, len(SupportedTransports))
	for k := range SupportedTransports {
		supported = append(supported, k)
	}
	sort.Strings(supported)
	return strings.Join(supported, ", ")
}

// ToAgentEntry converts a runtime manifest into a daemon AgentEntry for
// registration. The transport field determines how the daemon spawns the
// CLI at task time.
func (m RuntimeManifest) ToAgentEntry() AgentEntry {
	transport := m.Transport
	if transport == "" {
		transport = "acp-stdio"
	}
	return AgentEntry{
		Path:            m.Command.Executable,
		Transport:       transport,
		ExtraArgs:       append([]string(nil), m.Command.Args...),
		ACPArgs:         append([]string(nil), m.Command.Args...),
		IsExternal:      true,
		LaunchHeader:    m.LaunchHeader,
		ConfigFile:      m.ConfigFile,
		SkillsRoot:      m.SkillsRoot,
		IconURL:         m.IconURL,
		Models:          manifestModelsToAgentModels(m.Models),
		Pricing:         m.Pricing,
		ManifestName:    m.Name,
		ManifestID:      m.ID,
		Provider:        m.Provider,
		Description:     m.Description,
		Version:         m.Version,
		MinCLIVersion:   m.MinCLIVersion,
		BlockedArgs:     copyStringMap(m.Command.BlockedArgs),
		Env:             copyStringMap(m.Env),
		ModelsDiscovery: resolveModelsDiscovery(m.ModelsDiscovery, transport),
		rawCaps:         m.Capabilities,
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// resolveModelsDiscovery normalises the user-supplied discovery config,
// inferring the method from transport when omitted.
func resolveModelsDiscovery(cfg *ModelsDiscoveryConfig, transport string) *ModelsDiscoveryConfig {
	if cfg == nil {
		return nil
	}
	// Copy so we don't mutate the parsed manifest.
	out := *cfg
	if out.Method == "" {
		switch transport {
		case "stream-json":
			out.Method = "cli"
		case "acp-stdio":
			out.Method = "acp"
		}
	}
	if out.CLI != nil && out.CLI.TimeoutSeconds <= 0 {
		out.CLI.TimeoutSeconds = 15
	}
	if out.CacheTTLSeconds <= 0 {
		out.CacheTTLSeconds = 60
	}
	return &out
}

func manifestModelsToAgentModels(models []RuntimeManifestModel) []AgentModel {
	if len(models) == 0 {
		return nil
	}
	out := make([]AgentModel, len(models))
	for i, m := range models {
		out[i] = AgentModel{
			ID:       m.ID,
			Label:    m.Label,
			Default:  m.Default,
			Thinking: m.Thinking,
		}
	}
	return out
}

// LoadRuntimeManifests scans rootDir (typically ~/.multica/runtimes/)
// for subdirectories containing a runtime.json file and returns the
// parsed manifests. Invalid or unparseable files are skipped with a
// logged warning; the caller decides how to handle an empty result.
func LoadRuntimeManifests(rootDir string) ([]RuntimeManifest, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime dir %s: %w", rootDir, err)
	}

	var manifests []RuntimeManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(rootDir, entry.Name(), "runtime.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			fmt.Fprintf(os.Stderr, "warning: failed to read runtime manifest %s: %v\n", manifestPath, err)
			continue
		}

		data, err = stripJSONComments(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid runtime manifest %s: %v\n", manifestPath, err)
			continue
		}

		var m RuntimeManifest
		if err := json.Unmarshal(data, &m); err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid runtime manifest %s: %v\n", manifestPath, err)
			continue
		}

		schemaVersion := runtimeManifestSchemaVersion(m)
		if !isSupportedRuntimeManifestSchemaVersion(schemaVersion) {
			fmt.Fprintf(os.Stderr, "warning: runtime manifest %s declares unsupported schema_version %d (supported: %s)\n",
				manifestPath, schemaVersion, supportedRuntimeManifestSchemaVersionsLabel())
			continue
		}

		// Validate required fields.
		var missing []string
		if m.ID == "" {
			missing = append(missing, "id")
		}
		if m.Name == "" {
			missing = append(missing, "name")
		}
		if m.Provider == "" {
			missing = append(missing, "provider")
		}
		if m.Transport == "" {
			missing = append(missing, "transport")
		}
		if m.Command.Executable == "" {
			missing = append(missing, "command.executable")
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "warning: runtime manifest %s missing required fields: %s\n", manifestPath, strings.Join(missing, ", "))
			continue
		}

		// Reject unknown transports up front. We allow the empty string
		// (defaults to acp-stdio in ToAgentEntry) to keep older manifests
		// readable, but anything else must be in SupportedTransports so
		// `agent.New()` can route to a real backend.
		if m.Transport != "" {
			if !isSupportedRuntimeTransport(m.Transport) {
				fmt.Fprintf(os.Stderr, "warning: runtime manifest %s declares unsupported transport %q (supported: %s)\n",
					manifestPath, m.Transport, supportedRuntimeTransportsLabel())
				continue
			}
		}

		manifests = append(manifests, m)
	}

	return manifests, nil
}

func stripJSONComments(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); {
		c := data[i]

		if inString {
			out = append(out, c)
			i++
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		if c == '"' {
			inString = true
			out = append(out, c)
			i++
			continue
		}

		if c == '/' && i+1 < len(data) {
			switch data[i+1] {
			case '/':
				i += 2
				for i < len(data) && data[i] != '\n' && data[i] != '\r' {
					i++
				}
				if i < len(data) && data[i] == '\r' {
					out = append(out, data[i])
					i++
				}
				if i < len(data) && data[i] == '\n' {
					out = append(out, data[i])
					i++
				}
				continue
			case '*':
				i += 2
				closed := false
				for i < len(data) {
					if i+1 < len(data) && data[i] == '*' && data[i+1] == '/' {
						i += 2
						closed = true
						break
					}
					if data[i] == '\r' || data[i] == '\n' {
						out = append(out, data[i])
					}
					i++
				}
				if !closed {
					return nil, fmt.Errorf("unterminated block comment")
				}
				continue
			}
		}

		out = append(out, c)
		i++
	}

	return out, nil
}

// DefaultRuntimesDir returns the default path for user-installed runtime
// extensions (~/.multica/runtimes). The directory is NOT created here;
// callers should create it if needed before scanning.
func DefaultRuntimesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".multica", "runtimes")
	}
	return filepath.Join(home, ".multica", "runtimes")
}
