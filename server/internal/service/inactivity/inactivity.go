// Package inactivity implements the MUL-4059 "max-inactivity timeout"
// half of the safeguard. It exposes:
//
//   - ComputeTaskMaxInactivity: the task > agent > workspace > server-default
//     resolution chain for the per-task cap. Enqueue paths call this once
//     when creating a row so the value is immutable for the lifetime of
//     the run (operators don't expect a global config bump to retroactively
//     kill their long-running jobs).
//
//   - ApplyAgentActivity: a one-line helper that writes last_activity_at
//     on a task row. The daemon message / progress / session / usage
//     handlers all call this so the server-side inactivity sweeper sees
//     the task as alive.
//
// The actual sweeping lives in server/cmd/server/runtime_sweeper.go
// (sweepInactiveTasks). This package owns the *value* and the *write
// path*, not the scan loop — separation of concerns matches the
// existing empty_claim_cache / wakeup pattern.
package inactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Defaults holds the server-side default timeout. Loaded once at
// startup from environment / flag (AGENT_TASK_MAX_INACTIVITY_SECS)
// and passed in here. The default is 1200s (20 min) — see GATE 1
// decision in MUL-4059.
type Defaults struct {
	// DefaultMaxInactivitySecs is the floor when no workspace, agent,
	// or task override is configured. The 1200 default matches the
	// existing DefaultCodexSemanticInactivityTimeout (10 min) with a
	// little extra slack for long writes / build / install / test
	// cycles that the codex semantic marker would also flag.
	DefaultMaxInactivitySecs int
}

// DefaultDefaultMaxInactivitySecs is the 20-minute default documented in
// the MUL-4059 design. Picked to be just under the existing 2.5h
// `runningTimeoutSeconds` (which stays as the hard backstop for a
// daemon that died without reporting) but well above any realistic
// single-task wall-clock cap.
//
// We keep the value as an exported constant so tests can assert
// "default == 1200" without importing the env-reading helper, and
// so dashboards can label the column with the same number.
const DefaultDefaultMaxInactivitySecs = 1200

// ComputeTaskMaxInactivity resolves the per-task inactivity cap using
// the precedence chain:
//
//   task override (caller-supplied) > agent.settings.task_max_inactivity_secs
//     > workspace.settings.task_max_inactivity_secs
//       > defaults.DefaultMaxInactivitySecs (or 1200 if unset / non-positive)
//
// A non-positive value at any level means "fall through to the next
// lower-precedence source". We never persist a zero / negative cap on
// the row — that would let a typo silently disable the safeguard. The
// function returns the resolved int and a string explaining which
// source supplied it (useful for the audit log; the task row itself
// only stores the resolved number).
//
// This function is pure: no DB writes, no env reads. The caller
// (TaskService.Enqueue*) calls it once when constructing the task
// row and stuffs the result into CreateAgentTaskParams.MaxInactivitySecs.
//
// taskOverride is the explicit override the enqueue path carries
// through (e.g. an autopilot task marked "long-running"). May be 0
// for "no override".
func ComputeTaskMaxInactivity(taskOverride int, agent db.Agent, workspace db.Workspace, defaults Defaults) int {
	candidates := []struct {
		value int
		name  string
	}{
		{taskOverride, "task"},
		{readInactivityFromAgentSettings(agent.RuntimeConfig), "agent"},
		{readInactivityFromWorkspaceSettings(workspace.Settings), "workspace"},
	}
	for _, c := range candidates {
		if c.value > 0 {
			return c.value
		}
	}
	if defaults.DefaultMaxInactivitySecs > 0 {
		return defaults.DefaultMaxInactivitySecs
	}
	return DefaultDefaultMaxInactivitySecs
}

// readInactivityFromAgentSettings extracts task_max_inactivity_secs
// from the agent.runtime_config JSONB blob. The agent table does not
// currently have a dedicated `settings` column — runtime_config carries
// the agent-level knobs (model, thinking_level, mcp_config, etc.) so
// per-agent inactivity overrides live there too. Empty / missing key
// returns 0 (fall through). Non-int values are logged and treated as
// "not set"; the same defensive pattern as contextguard.
func readInactivityFromAgentSettings(raw []byte) int {
	return readPositiveIntFromJSON(raw, "task_max_inactivity_secs")
}

// readInactivityFromWorkspaceSettings is the workspace.settings analogue.
// Same contract: 0 = fall through.
func readInactivityFromWorkspaceSettings(raw []byte) int {
	return readPositiveIntFromJSON(raw, "task_max_inactivity_secs")
}

// readPositiveIntFromJSON pulls a positive int out of a JSONB blob.
// Negative / zero values return 0 ("not set"). Missing key returns 0.
// Malformed JSON or wrong type is logged at warn level and returns 0 —
// the safeguard still fires under the server default. We deliberately
// do NOT propagate the parse error; an operator typo "task_max_inactivity_secs"
// (one s missing) should not break enqueue, only be visible in logs.
//
// Keeping this helper unexported: it's the only consumer of these
// two callers and adding a public surface would just bloat the API.
func readPositiveIntFromJSON(raw []byte, key string) int {
	if len(raw) == 0 {
		return 0
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		slog.Warn("inactivity: settings JSON parse failed",
			"key", key, "error", err)
		return 0
	}
	val, ok := probe[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		if v <= 0 || v != float64(int(v)) {
			return 0
		}
		return int(v)
	case int:
		if v <= 0 {
			return 0
		}
		return v
	case int64:
		if v <= 0 {
			return 0
		}
		return int(v)
	default:
		slog.Warn("inactivity: settings value is not a positive int",
			"key", key, "type", fmt.Sprintf("%T", v))
		return 0
	}
}

// ApplyAgentActivity writes last_activity_at = now() on the given
// task. Called from the daemon message / progress / session / usage
// handlers so the server-side inactivity sweeper sees the task as
// alive. We deliberately ignore errors here: a transient DB blip on
// the activity write should not block the message / progress flow
// (the daemon will retry on the next event anyway). The worst case
// is a single sweep tick that incorrectly fails a task whose activity
// did get written — the auto-retry path then resurrects it.
func ApplyAgentActivity(ctx context.Context, q Querier, taskID pgtype.UUID) {
	if !taskID.Valid {
		return
	}
	if err := q.UpdateAgentTaskActivity(ctx, taskID); err != nil {
		slog.Warn("inactivity: failed to refresh last_activity_at",
			"task_id", util.UUIDToString(taskID), "error", err)
	}
}

// Querier is the minimal surface we need from the generated sqlc
// package. Both *db.Queries and the WithTx variant satisfy this.
// Defined as an interface in this package (rather than reusing
// db.Querier) so callers don't have to depend on the full
// generated-package surface just to call ApplyAgentActivity.
type Querier interface {
	UpdateAgentTaskActivity(ctx context.Context, id pgtype.UUID) error
}

// ResolveForTask is a convenience helper that reads the resolved
// value off an existing task row. Returns the server default when
// the column is NULL (legacy rows or rows written before migration
// 120 ran). Used by the daemon soft-kill path: the claim response
// carries the resolved number, but if the daemon somehow misses it
// (older version that doesn't know about the field), it falls back
// to ResolveForTask at the moment of running the inactivity watcher.
func ResolveForTask(task db.AgentTaskQueue, defaults Defaults) int {
	if task.MaxInactivitySecs.Valid && task.MaxInactivitySecs.Int32 > 0 {
		return int(task.MaxInactivitySecs.Int32)
	}
	if defaults.DefaultMaxInactivitySecs > 0 {
		return defaults.DefaultMaxInactivitySecs
	}
	return DefaultDefaultMaxInactivitySecs
}

// Describe returns a human-readable summary of the resolved value
// for logging. Avoids the import-cycle risk of formatting inside the
// service caller.
func Describe(resolved int, source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "default"
	}
	return fmt.Sprintf("max_inactivity=%ds source=%s", resolved, source)
}