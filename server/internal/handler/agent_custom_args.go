package handler

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// customArgsForRuntime applies platform-owned defaults immediately before the
// agent custom_args JSON is persisted. Registration metadata is a persistence
// and UI-preview hint only. At task start the daemon derives execution
// authority again from its actual GOOS, effective argv, and the config copied
// by Prepare, so a stale registration hint cannot weaken launch behavior.
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

// normalizedRuntimeOnlyCustomArgs re-reads and normalizes the row locked by a
// runtime-only update. The caller must hold that row lock through UpdateAgent;
// otherwise a concurrent custom_args replacement can land between this read and
// the write and be overwritten by a stale normalized copy.
func normalizedRuntimeOnlyCustomArgs(existing db.Agent, runtime db.AgentRuntime) ([]byte, bool, error) {
	var customArgs []string
	if len(existing.CustomArgs) > 0 {
		if err := json.Unmarshal(existing.CustomArgs, &customArgs); err != nil {
			return nil, false, err
		}
	}
	customArgs, managed := customArgsForRuntime(
		runtime,
		customArgs,
		existing.IsCodexWindowsSandboxArgManaged,
	)
	encoded, err := json.Marshal(customArgs)
	if err != nil {
		return nil, false, err
	}
	return encoded, managed, nil
}
