package agent

import "sort"

// Capability declares the runtime-level features a provider supports. The
// registry is intentionally about behavior, not provider-specific integration
// details such as config filenames, skill roots, or protocol wiring.
type Capability struct {
	StreamDisplay    bool
	ToolCallStream   bool
	Approval         bool
	ResumeSession    bool
	PlanMode         bool
	StructuredOutput bool
}

var capabilityRegistry = map[string]Capability{
	"claude": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		Approval:         true,
		ResumeSession:    true,
		PlanMode:         true,
		StructuredOutput: true,
	},
	"codebuddy": {
		Approval:         true,
		ResumeSession:    true,
		PlanMode:         true,
		StructuredOutput: true,
	},
	"codex": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		Approval:         true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"antigravity": {
		ResumeSession: true,
	},
	"copilot": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		Approval:         true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"opencode": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"openclaw": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"wujieclaw": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"hermes": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"gemini": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"pi": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"cursor": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"kimi": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"kiro": {
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"qoderclicn": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"DeepSeek-TUI": {
		StructuredOutput: true,
	},
	"antigravity": {
		ResumeSession: true,
	},
	"mmx": {
		StructuredOutput: true,
	},
}

// registeredProviders returns the sorted list of provider names in the
// capability registry. Exported indirectly via RegisteredProviders in agent.go.
func registeredProviders() []string {
	names := make([]string, 0, len(capabilityRegistry))
	for name := range capabilityRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CapabilityFor reports the registered capability for provider.
func CapabilityFor(provider string) (Capability, bool) {
	capability, ok := capabilityRegistry[provider]
	return capability, ok
}

// CapabilityOrDefault returns the registered capability when present, or the
// zero-value capability for unknown providers.
func CapabilityOrDefault(provider string) Capability {
	capability, ok := CapabilityFor(provider)
	if !ok {
		return Capability{}
	}
	return capability
}
