package agent

import (
	"fmt"
	"strconv"
	"strings"
)

// InstructionDelivery describes how the daemon delivers its runtime brief.
// File delivery is preferred because the provider reads the native context
// file from the task workdir. Legacy inline delivery is retained only for a
// provider/version pair whose native file path is not yet reliable.
type InstructionDelivery string

const (
	InstructionFile                 InstructionDelivery = "file"
	InstructionInline               InstructionDelivery = "inline"
	InstructionFileWithLegacyInline InstructionDelivery = "file+legacy-inline"
)

// OpenClawMinimumSupportedVersion is the compatibility boundary for the
// workspace-pinned OpenClaw config and native AGENTS.md/skills discovery.
// Releases older than this keep the legacy inline instruction fallback.
const OpenClawMinimumSupportedVersion = "2026.5.5"

// ProviderCapability is the canonical provider capability contract. Runtime
// conditionals and generated documentation should consume this table rather
// than maintaining independent provider switch lists.
//
// Paths are relative to the task workdir unless they use an environment/home
// field. UserSkillEnvRelative is the path below UserSkillEnvVar when that
// variable is set.
type ProviderCapability struct {
	Type                  string
	DisplayName           string
	SupportsMCPConfig     bool
	MCPDelivery           string
	MCPConfigSource       string
	NativeSkillCatalog    bool
	RuntimeConfigFile     string
	WorkspaceSkillPath    string
	UserSkillHomeRelative string
	UserSkillEnvVar       string
	UserSkillEnvRelative  string
	InstructionDelivery   InstructionDelivery
	LegacyInlineBefore    string
	SandboxApproval       string
	Compatibility         string
}

// providerCapabilityTable is the single source of truth for the supported
// protocol families. Keep entries ordered like SupportedTypes so generated
// docs and validation failures are stable.
var providerCapabilityTable = []ProviderCapability{
	{
		Type:                  "claude",
		DisplayName:           "Claude Code",
		SupportsMCPConfig:     true,
		MCPDelivery:           "--mcp-config + --strict-mcp-config",
		MCPConfigSource:       "~/.claude.json:mcpServers",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "CLAUDE.md",
		WorkspaceSkillPath:    ".claude/skills",
		UserSkillHomeRelative: ".claude/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "bypassPermissions; daemon blocks approval prompts",
		Compatibility:         "native CLAUDE.md and .claude/skills discovery",
	},
	{
		Type:                  "codebuddy",
		DisplayName:           "CodeBuddy",
		SupportsMCPConfig:     true,
		MCPDelivery:           "--mcp-config; native user/project/local scopes remain enabled",
		MCPConfigSource:       "CodeBuddy native user/project/local scopes",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "CODEBUDDY.md",
		WorkspaceSkillPath:    ".codebuddy/skills",
		UserSkillHomeRelative: ".codebuddy/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "bypassPermissions; disallowed approval tools",
		Compatibility:         "native CODEBUDDY.md and .codebuddy/skills discovery",
	},
	{
		Type:                  "codex",
		DisplayName:           "Codex",
		SupportsMCPConfig:     true,
		MCPDelivery:           "task CODEX_HOME/config.toml",
		MCPConfigSource:       "CODEX_HOME/config.toml:mcp_servers",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    "CODEX_HOME/skills",
		UserSkillHomeRelative: ".codex/skills",
		UserSkillEnvVar:       "CODEX_HOME",
		UserSkillEnvRelative:  "skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "workspace-write; sandbox policy in config/args",
		Compatibility:         "AGENTS.md is read from the task cwd; skills use task CODEX_HOME",
	},
	{
		Type:                  "copilot",
		DisplayName:           "GitHub Copilot CLI",
		SupportsMCPConfig:     false,
		MCPDelivery:           "not supported by agent.mcp_config",
		MCPConfigSource:       "native Copilot configuration",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".github/skills",
		UserSkillHomeRelative: ".copilot/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "yolo/interactive policy owned by Copilot CLI",
		Compatibility:         "AGENTS.md and .github/skills project discovery",
	},
	{
		Type:                  "opencode",
		DisplayName:           "OpenCode",
		SupportsMCPConfig:     true,
		MCPDelivery:           "OPENCODE_CONFIG_CONTENT",
		MCPConfigSource:       "XDG_CONFIG_HOME/opencode/opencode.json:mcp",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".opencode/skills",
		UserSkillHomeRelative: ".config/opencode/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "provider-native permission policy",
		Compatibility:         "--dir/PWD anchors AGENTS.md and .opencode/skills at the task workdir",
	},
	{
		Type:                  "deveco",
		DisplayName:           "DevEco Code",
		SupportsMCPConfig:     false,
		MCPDelivery:           "not supported by agent.mcp_config",
		MCPConfigSource:       "native DevEco configuration",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".deveco/skills",
		UserSkillHomeRelative: ".config/deveco/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "provider-native permission policy",
		Compatibility:         "OpenCode-compatible AGENTS.md and .deveco/skills discovery",
	},
	{
		Type:                  "openclaw",
		DisplayName:           "OpenClaw",
		SupportsMCPConfig:     true,
		MCPDelivery:           "per-task openclaw-config.json wrapper",
		MCPConfigSource:       "CLAWDBOT_CONFIG_PATH or OPENCLAW_STATE_DIR/openclaw.json:mcp.servers",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    "skills",
		UserSkillHomeRelative: ".openclaw/skills",
		InstructionDelivery:   InstructionFileWithLegacyInline,
		LegacyInlineBefore:    OpenClawMinimumSupportedVersion,
		SandboxApproval:       "--local plus daemon-owned agent config",
		Compatibility:         "supported >= 2026.5.5: workspace-pinned AGENTS.md + skills/ only; older releases retain inline fallback",
	},
	{
		Type:                  "hermes",
		DisplayName:           "Hermes Agent",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "HERMES_HOME native profile",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    "HERMES_HOME/skills",
		UserSkillHomeRelative: ".hermes/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "per-task HERMES_HOME is seeded and AGENTS.md is read from the ACP cwd",
	},
	{
		Type:                  "pi",
		DisplayName:           "Pi coding agent",
		SupportsMCPConfig:     false,
		MCPDelivery:           "not supported by agent.mcp_config",
		MCPConfigSource:       "Pi native extensions/config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".pi/skills",
		UserSkillHomeRelative: ".pi/agent/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "provider-native tool policy",
		Compatibility:         "AGENTS.md and .pi/skills are loaded from the task cwd",
	},
	{
		Type:                  "cursor",
		DisplayName:           "Cursor Agent",
		SupportsMCPConfig:     true,
		MCPDelivery:           "task Cursor MCP config/auth sidecars",
		MCPConfigSource:       "~/.cursor/mcp.json:mcpServers",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".cursor/skills",
		UserSkillHomeRelative: ".cursor/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "--yolo",
		Compatibility:         "AGENTS.md and .cursor/skills are anchored with --workspace",
	},
	{
		Type:                  "kimi",
		DisplayName:           "Kimi CLI",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "Kimi native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".kimi/skills",
		UserSkillHomeRelative: ".kimi/skills",
		InstructionDelivery:   InstructionInline,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "inline fallback remains required while Kimi cwd discovery is opaque",
	},
	{
		Type:                  "reasonix",
		DisplayName:           "Reasonix",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "REASONIX_HOME native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".reasonix/skills",
		UserSkillHomeRelative: ".reasonix/skills",
		UserSkillEnvVar:       "REASONIX_HOME",
		UserSkillEnvRelative:  "skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "AGENTS.md and .reasonix/skills follow the effective REASONIX_HOME",
	},
	{
		Type:                  "kiro",
		DisplayName:           "Kiro CLI",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "Kiro native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".kiro/skills",
		UserSkillHomeRelative: ".kiro/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "Kiro ACP smoke uses root AGENTS.md and .kiro/skills",
	},
	{
		Type:                  "antigravity",
		DisplayName:           "Antigravity",
		SupportsMCPConfig:     false,
		MCPDelivery:           "not supported by agent.mcp_config",
		MCPConfigSource:       "Gemini/Antigravity native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".agents/skills",
		UserSkillHomeRelative: ".gemini/antigravity-cli/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "provider-native permission policy",
		Compatibility:         "AGENTS.md and .agents/skills use Antigravity workspace discovery",
	},
	{
		Type:                  "qoder",
		DisplayName:           "Qoder CLI",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "Qoder native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".qoder/skills",
		UserSkillHomeRelative: ".qoder/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "Qoder project skills use .qoder/skills",
	},
	{
		Type:                  "qoderclicn",
		DisplayName:           "Qoder CLI CN",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "Qoder CN native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".qoder/skills",
		UserSkillHomeRelative: ".qoder-cn/skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "Qoder CN project skills use .qoder/skills and a separate user root",
	},
	{
		Type:                  "traecli",
		DisplayName:           "Trae CLI",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "Trae CLI native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".traecli/skills",
		UserSkillHomeRelative: ".traecli/skills",
		InstructionDelivery:   InstructionInline,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "Trae reads .trae/rules rather than AGENTS.md; inline delivery remains required",
	},
	{
		Type:                  "grok",
		DisplayName:           "Grok Build",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "GROK_HOME native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    ".grok/skills",
		UserSkillHomeRelative: ".grok/skills",
		UserSkillEnvVar:       "GROK_HOME",
		UserSkillEnvRelative:  "skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "--always-approve",
		Compatibility:         "Grok Build reads AGENTS.md and .grok/skills from the task cwd",
	},
	{
		Type:                  "qwen",
		DisplayName:           "Qwen Code",
		SupportsMCPConfig:     true,
		MCPDelivery:           "Qwen native config/session parameters",
		MCPConfigSource:       "QWEN_HOME native config",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "QWEN.md",
		WorkspaceSkillPath:    ".qwen/skills",
		UserSkillHomeRelative: ".qwen/skills",
		UserSkillEnvVar:       "QWEN_HOME",
		UserSkillEnvRelative:  "skills",
		InstructionDelivery:   InstructionFile,
		SandboxApproval:       "provider-native permission policy",
		Compatibility:         "QWEN.md is the runtime context file; .qwen/skills is project-local",
	},
	{
		Type:                  "qwenpaw",
		DisplayName:           "QwenPaw",
		SupportsMCPConfig:     true,
		MCPDelivery:           "ACP session/new MCP parameters",
		MCPConfigSource:       "QWENPAW_WORKING_DIR native skill/config store",
		NativeSkillCatalog:    true,
		RuntimeConfigFile:     "AGENTS.md",
		WorkspaceSkillPath:    "skill_pool",
		UserSkillHomeRelative: ".qwenpaw/skill_pool",
		UserSkillEnvVar:       "QWENPAW_WORKING_DIR",
		UserSkillEnvRelative:  "skill_pool",
		InstructionDelivery:   InstructionInline,
		SandboxApproval:       "ACP permission policy",
		Compatibility:         "QwenPaw workspace skill_pool is isolated; inline delivery remains required",
	},
}

// SupportedProviderTypes returns a fresh ordered list of protocol families.
func SupportedProviderTypes() []string {
	out := make([]string, 0, len(providerCapabilityTable))
	for _, capability := range providerCapabilityTable {
		out = append(out, capability.Type)
	}
	return out
}

// AllProviderCapabilities returns a copy of the canonical capability rows.
func AllProviderCapabilities() []ProviderCapability {
	return append([]ProviderCapability(nil), providerCapabilityTable...)
}

// ProviderCapabilityFor returns the capability row for a protocol family or a
// built-in runtime identity such as omp. Built-in identities inherit the
// protocol contract and override only their runtime-specific paths/label.
func ProviderCapabilityFor(provider string) (ProviderCapability, bool) {
	for _, capability := range providerCapabilityTable {
		if capability.Type == provider {
			return capability, true
		}
	}
	if desc, ok := BuiltinRuntimeByID(provider); ok {
		if capability, ok := ProviderCapabilityFor(desc.ProtocolFamily); ok {
			capability.Type = provider
			capability.DisplayName = desc.DisplayName
			capability.WorkspaceSkillPath = desc.SkillsDir
			capability.UserSkillHomeRelative = desc.UserSkillsDir
			return capability, true
		}
	}
	return ProviderCapability{}, false
}

// ProviderSupportsMCPConfig is the shared MCP capability predicate used by
// server discovery and UI mirrors. Unknown providers fail closed.
func ProviderSupportsMCPConfig(provider string) bool {
	capability, ok := ProviderCapabilityFor(provider)
	return ok && capability.SupportsMCPConfig
}

// InstructionNeedsInline reports whether the runtime brief must be included in
// ExecOptions.SystemPrompt for the provider/version pair. An unknown version
// keeps the legacy fallback enabled for compatibility rows.
func InstructionNeedsInline(provider, version string) bool {
	capability, ok := ProviderCapabilityFor(provider)
	if !ok {
		return false
	}
	switch capability.InstructionDelivery {
	case InstructionInline:
		return true
	case InstructionFileWithLegacyInline:
		return !VersionAtLeast(version, capability.LegacyInlineBefore)
	default:
		return false
	}
}

// VersionAtLeast compares the first three numeric components of version and
// minimum. It returns false when either value cannot be parsed, which keeps a
// legacy fallback enabled instead of silently dropping instructions.
func VersionAtLeast(version, minimum string) bool {
	got, ok := numericVersion(version)
	if !ok {
		return false
	}
	want, ok := numericVersion(minimum)
	if !ok {
		return false
	}
	for i := range got {
		if got[i] < want[i] {
			return false
		}
		if got[i] > want[i] {
			return true
		}
	}
	return true
}

func numericVersion(raw string) ([3]int, bool) {
	var out [3]int
	raw = strings.TrimSpace(raw)
	start := -1
	for i := 0; i < len(raw); i++ {
		if raw[i] >= '0' && raw[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return out, false
	}
	parts := strings.Split(raw[start:], ".")
	if len(parts) < 3 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		end := 0
		for end < len(parts[i]) && parts[i][end] >= '0' && parts[i][end] <= '9' {
			end++
		}
		if end == 0 {
			return out, false
		}
		value, err := strconv.Atoi(parts[i][:end])
		if err != nil {
			return out, false
		}
		out[i] = value
	}
	return out, true
}

// ProviderCapabilityMatrixMarkdown renders the checked-in documentation
// mirror. The package test compares this output with docs/provider-capabilities.md
// so documentation drift is an executable failure rather than a review guess.
func ProviderCapabilityMatrixMarkdown() string {
	var b strings.Builder
	b.WriteString("# Provider capability matrix\n\n")
	b.WriteString("This file is generated from `server/pkg/agent/provider_capabilities.go`. Do not edit it by hand.\n\n")
	b.WriteString("| Provider | MCP support/delivery | Sandbox/approval | Runtime config | Workspace skills | User skills | Instruction delivery | Compatibility |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, capability := range providerCapabilityTable {
		mcp := "no: " + capability.MCPDelivery
		if capability.SupportsMCPConfig {
			mcp = "yes: " + capability.MCPDelivery
		}
		instructions := string(capability.InstructionDelivery)
		if capability.LegacyInlineBefore != "" {
			instructions += " before " + capability.LegacyInlineBefore
		}
		fmt.Fprintf(&b, "| %s | %s | %s | `%s` | `%s` | `%s` | %s | %s |\n",
			capability.DisplayName,
			mcp,
			capability.SandboxApproval,
			capability.RuntimeConfigFile,
			capability.WorkspaceSkillPath,
			userSkillPathLabel(capability),
			instructions,
			capability.Compatibility,
		)
	}
	b.WriteString("\n")
	b.WriteString("MCP config source details:\n\n")
	for _, capability := range providerCapabilityTable {
		fmt.Fprintf(&b, "- `%s`: `%s`\n", capability.Type, capability.MCPConfigSource)
	}
	return b.String()
}

func userSkillPathLabel(capability ProviderCapability) string {
	if capability.UserSkillEnvVar != "" {
		return "$" + capability.UserSkillEnvVar + "/" + capability.UserSkillEnvRelative
	}
	return "~/" + strings.TrimPrefix(capability.UserSkillHomeRelative, "./")
}
