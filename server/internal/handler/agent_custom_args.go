package handler

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// customArgsForRuntime applies platform-owned defaults immediately before the
// agent custom_args JSON is persisted. Runtime metadata carries the daemon OS
// for display and persistence decisions; the daemon independently applies the
// same default from runtime.GOOS at launch, so a missing or stale metadata hint
// cannot weaken execution behavior.
func customArgsForRuntime(runtime db.AgentRuntime, customArgs []string) []string {
	if !strings.EqualFold(strings.TrimSpace(runtime.Provider), "codex") {
		return append([]string(nil), customArgs...)
	}

	var metadata struct {
		OS string `json:"os"`
	}
	if err := json.Unmarshal(runtime.Metadata, &metadata); err != nil {
		return append([]string(nil), customArgs...)
	}
	return agent.EnsureCodexWindowsSandboxCustomArgs(
		strings.ToLower(strings.TrimSpace(metadata.OS)),
		nil,
		customArgs,
	)
}
