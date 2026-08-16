package handler

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// customArgsForRuntime applies platform-owned defaults immediately before the
// agent custom_args JSON is persisted. Runtime metadata carries the daemon OS
// plus whether fixed arguments already own the setting; the daemon
// independently applies the same rule from runtime.GOOS and effective argv at
// launch, so a missing or stale metadata hint cannot weaken execution behavior.
func customArgsForRuntime(runtime db.AgentRuntime, customArgs []string) []string {
	if !strings.EqualFold(strings.TrimSpace(runtime.Provider), "codex") {
		return agent.NormalizeCodexWindowsSandboxCustomArgs("", false, customArgs)
	}

	var metadata struct {
		OS                               string `json:"os"`
		CodexWindowsSandboxArgConfigured bool   `json:"codex_windows_sandbox_arg_configured"`
	}
	if err := json.Unmarshal(runtime.Metadata, &metadata); err != nil {
		return agent.NormalizeCodexWindowsSandboxCustomArgs("", false, customArgs)
	}
	return agent.NormalizeCodexWindowsSandboxCustomArgs(
		strings.ToLower(strings.TrimSpace(metadata.OS)),
		metadata.CodexWindowsSandboxArgConfigured,
		customArgs,
	)
}
