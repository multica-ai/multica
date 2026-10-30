package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
)

// leftoverDaemonTaskMarkerPath returns the workdir marker path when a marker is
// the ONLY reason the CLI considers itself inside a daemon-managed task AND
// that marker could actually be a leftover. It returns "" otherwise.
//
// Two things disqualify a marker from the leftover treatment:
//
// Any MULTICA_* task identity in the environment means the daemon really did
// launch this process, so the marker is doing its job. MULTICA_DAEMON_PORT
// counts here even though it is not sufficient on its own for identity: it
// still says a daemon environment is present, which is not a leftover's
// fingerprint.
//
// A marker that is not task-scoped is the permanent workspaces root marker,
// which never expires and must never be described as removable.
func leftoverDaemonTaskMarkerPath() string {
	if inAgentExecutionContext() ||
		strings.TrimSpace(os.Getenv(cli.TaskConfigRootEnv)) != "" ||
		strings.TrimSpace(os.Getenv("MULTICA_DAEMON_PORT")) != "" {
		return ""
	}
	markerPath := daemonTaskContextMarkerPath()
	if markerPath == "" || !markerIsTaskScoped(markerPath) {
		return ""
	}
	return markerPath
}

// markerIsTaskScoped reports whether the marker at path belongs to one specific
// task, as opposed to being the workspaces root marker.
//
// Two different markers share this filename, and daemonTaskContextMarkerPath
// tells them apart by nothing but managed_by. The per-task marker carries the
// identity of the task that wrote it and dies with that task. The workspaces
// root marker carries no identity at all: the daemon re-creates it on every
// start as a permanent, node-wide guard so a subprocess that escapes above its
// workdir still fails closed. Telling anyone to delete THAT — a user, or an
// agent subprocess that lost its environment and is reading the error — closes
// the hole it exists to cover, so it gets the plain refusal instead.
//
// Identity means agent_id OR issue_id, not both: a chat task has no issue, and
// requiring one would misfile its marker as the root marker and lose the hint
// exactly where a leftover is possible. The root marker has neither field, so
// either one present is enough to separate them.
func markerIsTaskScoped(markerPath string) bool {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return false
	}
	var marker struct {
		AgentID string `json:"agent_id"`
		IssueID string `json:"issue_id"`
	}
	if json.Unmarshal(data, &marker) != nil {
		return false
	}
	return strings.TrimSpace(marker.AgentID) != "" || strings.TrimSpace(marker.IssueID) != ""
}

// leftoverMarkerSuffix renders the shared tail every refusal path appends when
// a task-scoped workdir marker is the only daemon signal:
//
//   - newAPIClient, for API commands
//   - requireHumanLocalCommand, for the local daemon/profile commands
//   - requireTaskLocalConfigRoot, for config show/set and auth status
//
// All three refusals have the same cause and the same remedy, so they say the
// same thing. Keeping the wording in one place is what stops them from drifting
// again — before MUL-6132 only the API path named the file, so whether a user
// could act on the error depended on which command they happened to run first
// (MUL-6132 review).
//
// The CLI deliberately stops at naming the file rather than deleting it.
// Removing the marker is removing a fail-closed guard, and no check the CLI can
// run proves it is safe to: the daemon increments its active task count only
// after a claim returns, so a probe can read zero for a task that is already
// claimed and about to write this very marker. Retirement therefore belongs to
// the daemon, which can serialize it against its own claim path.
func leftoverMarkerSuffix(markerPath string) string {
	return fmt.Sprintf("; detected a daemon task marker at %s — if you are not running inside an agent task this is likely a leftover, remove it and retry", markerPath)
}
