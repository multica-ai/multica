package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// taskMarkerHint renders the tail every refusal path appends when a
// task-scoped workdir marker is the only reason the CLI considers itself inside
// a daemon-managed task, and "" otherwise:
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
// A marker alone looks the same whether a crashed local_directory run left it
// in the user's repository or a live task's own process had its identity
// stripped on purpose. Only the first may be removed: an agent that removes its
// own task's marker turns off, for the rest of its run, the guard that keeps it
// off the human's profiles and daemon (MUL-8857). So the hint offers removal
// only when nothing says the task is still there.
func taskMarkerHint() string {
	markerPath := identitylessTaskMarkerPath()
	if markerPath == "" {
		return ""
	}
	if owner := taskMarkerOwner(markerPath); owner != "" {
		return fmt.Sprintf("; the daemon task marker at %s belongs to %s, so without the task's MULTICA_* identity this command may not use local profiles or daemons — the marker is not a leftover, do not remove it", markerPath, owner)
	}
	return leftoverMarkerSuffix(markerPath)
}

// leftoverDaemonTaskMarkerPath returns the marker path when the refusal
// describes that marker as a possible leftover, and "" otherwise.
func leftoverDaemonTaskMarkerPath() string {
	markerPath := identitylessTaskMarkerPath()
	if markerPath == "" || taskMarkerOwner(markerPath) != "" {
		return ""
	}
	return markerPath
}

// identitylessTaskMarkerPath returns the workdir marker path when a
// task-scoped marker is the ONLY reason the CLI considers itself inside a
// daemon-managed task. It returns "" otherwise.
//
// Any MULTICA_* task identity in the environment means the daemon really did
// launch this process, and the plain refusal already says what is going on.
// MULTICA_DAEMON_PORT counts here even though it is not sufficient on its own
// for identity: it still says a daemon environment is present.
//
// A marker that is not task-scoped is the permanent workspaces root marker,
// which never expires and must never be described as removable.
func identitylessTaskMarkerPath() string {
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

// taskMarkerOwner describes the task a marker still belongs to, or returns ""
// when it could be a leftover.
//
// dev-env strips the task's identity from everything it launches (make
// env-exec, its components, the Desktop it starts and that Desktop's daemon),
// so a process it launched is running for the task that called it.
//
// A marker under a daemon workspaces root is in a directory only tasks run in,
// and one a user could act on is in their own repository: the MUL-6132 leftover
// is a local_directory run's. Removing a marker under the root would not lift
// the refusal anyway, since the root marker above it refuses the same command,
// and the daemon's GC owns everything there.
func taskMarkerOwner(markerPath string) string {
	if name := launchingDevEnv(); name != "" {
		return fmt.Sprintf("the agent task running dev-env environment %q, which strips that identity on purpose", name)
	}
	if root := enclosingWorkspacesRoot(markerPath); root != "" {
		return fmt.Sprintf("an agent task workdir under the daemon workspaces root %s", root)
	}
	return ""
}

// enclosingWorkspacesRoot returns the directory holding a workspaces root
// marker above the workdir of the task marker at markerPath, or "".
func enclosingWorkspacesRoot(markerPath string) string {
	dir := filepath.Dir(taskMarkerWorkDir(markerPath))
	for {
		found := daemonTaskContextMarkerPathFrom(dir)
		if found == "" {
			return ""
		}
		workDir := taskMarkerWorkDir(found)
		if !markerIsTaskScoped(found) {
			return workDir
		}
		parent := filepath.Dir(workDir)
		if parent == workDir {
			return ""
		}
		dir = parent
	}
}

// taskMarkerWorkDir is the directory a marker at markerPath was written for.
func taskMarkerWorkDir(markerPath string) string {
	dir := markerPath
	for range strings.Split(filepath.ToSlash(execenv.TaskContextMarkerRelPath), "/") {
		dir = filepath.Dir(dir)
	}
	return dir
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

// leftoverMarkerSuffix is taskMarkerHint for a marker that may be a leftover.
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
