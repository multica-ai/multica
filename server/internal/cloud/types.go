package cloud

import "time"

// SandboxConfig holds configuration for the Daytona sandbox manager.
type SandboxConfig struct {
	DaytonaAPIKey    string        // Daytona API authentication
	DaytonaAPIURL    string        // Optional: custom Daytona API URL (SDK default if empty)
	DefaultModel     string        // Default LLM model (default: "auto-fastest")
	DefaultMaxTurns  int           // Default max agent turns (default: 25)
	FallbackAPIKey   string        // Fallback: Google AI Studio API key when ModelRelay fails
	ImageTimeout     time.Duration // Timeout for sandbox image build (default: 8min)
}

// TaskExecConfig holds per-task execution parameters.
type TaskExecConfig struct {
	TaskID       string        // Multica task ID (for logging)
	Prompt       string        // Agent prompt
	Model        string        // LLM model override (empty = use SandboxConfig.DefaultModel)
	MaxTurns     int           // Max turns override (0 = use SandboxConfig.DefaultMaxTurns)
	SystemPrompt string        // Additional system prompt
	Timeout      time.Duration // Execution timeout (0 = 20min default)
}
