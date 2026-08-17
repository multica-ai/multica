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

// normalizedAgentCustomArgsForUpdate derives custom_args from an agent row
// re-read under the update transaction's row lock. runtime is the effective
// runtime after applying the request (nil for an unbound agent). requestedArgs
// is nil for a runtime-only update and non-nil for an explicit replacement.
// requestObservedManaged is the provenance snapshot seen before the explicit
// replacement waited for the lock; it authenticates a legacy client's echoed
// managed prefix even if a concurrent runtime switch removes that prefix while
// this request is waiting. Runtime selection and persisted normalization still
// come exclusively from the locked row.
// The caller must hold the row lock through UpdateAgent so runtime and argv can
// never be persisted from different snapshots.
func normalizedAgentCustomArgsForUpdate(
	existing db.Agent,
	runtime *db.AgentRuntime,
	requestedArgs *[]string,
	requestedManaged *bool,
	requestObservedManaged bool,
) ([]byte, bool, error) {
	managed := existing.IsCodexWindowsSandboxArgManaged
	var customArgs []string
	if requestedArgs == nil {
		if len(existing.CustomArgs) > 0 {
			if err := json.Unmarshal(existing.CustomArgs, &customArgs); err != nil {
				return nil, false, err
			}
		}
	} else {
		customArgs = append([]string(nil), (*requestedArgs)...)
		switch {
		case requestedManaged != nil && *requestedManaged:
			// A client may echo true only to retain already-proven ownership
			// of the exact prefix it observed; it cannot manufacture
			// provenance from state committed while this request waited.
			managed = requestObservedManaged && agent.HasManagedCodexWindowsSandboxPrefix(customArgs)
		case requestedManaged != nil:
			managed = false
		default:
			// Compatibility with clients that echo the managed pair but
			// predate the provenance field. Authenticate against the
			// snapshot this request observed, then apply the locked runtime.
			managed = requestObservedManaged && agent.HasManagedCodexWindowsSandboxPrefix(customArgs)
		}
	}

	if runtime == nil {
		customArgs, managed = agent.NormalizeCodexWindowsSandboxCustomArgs(
			"", managed, false, customArgs,
		)
	} else {
		customArgs, managed = customArgsForRuntime(*runtime, customArgs, managed)
	}
	encoded, err := json.Marshal(customArgs)
	if err != nil {
		return nil, false, err
	}
	return encoded, managed, nil
}
