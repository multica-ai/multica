package handler

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// customArgsForRuntime applies platform-owned defaults immediately before the
// agent custom_args JSON is persisted. Runtime metadata carries the daemon OS
// plus whether fixed arguments or the shared config copied into tasks already
// own the setting; the daemon independently applies the same rule from
// runtime.GOOS, effective argv, and the copied config at launch, so a missing
// or stale metadata hint cannot weaken execution behavior.
func customArgsForRuntime(runtime db.AgentRuntime, customArgs []string, managed bool) ([]string, bool) {
	if !strings.EqualFold(strings.TrimSpace(runtime.Provider), "codex") {
		return agent.NormalizeCodexWindowsSandboxCustomArgs("", managed, false, customArgs)
	}

	var metadata struct {
		OS                                  string `json:"os"`
		CodexWindowsSandboxArgConfigured    bool   `json:"codex_windows_sandbox_arg_configured"`
		CodexWindowsSandboxConfigConfigured bool   `json:"codex_windows_sandbox_config_configured"`
	}
	if err := json.Unmarshal(runtime.Metadata, &metadata); err != nil {
		return agent.NormalizeCodexWindowsSandboxCustomArgs("", managed, false, customArgs)
	}
	return agent.NormalizeCodexWindowsSandboxCustomArgs(
		strings.ToLower(strings.TrimSpace(metadata.OS)),
		managed,
		metadata.CodexWindowsSandboxArgConfigured || metadata.CodexWindowsSandboxConfigConfigured,
		customArgs,
	)
}
