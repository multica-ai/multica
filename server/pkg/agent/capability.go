package agent

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
		StreamDisplay:    true,
		ToolCallStream:   true,
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
	"copilot": {
		StreamDisplay:    true,
		ToolCallStream:   true,
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
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"hermes": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"gemini": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"pi": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"cursor": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"kimi": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"kiro": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
	"DeepSeek-TUI": {
		StreamDisplay:    true,
		ToolCallStream:   true,
		ResumeSession:    true,
		StructuredOutput: true,
	},
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
