package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// TaskEnvFileEnv points a daemon-managed `multica` CLI invocation at a private
// 0o600 JSON overlay file holding the active turn's volatile per-task
// environment variables (MULTICA_TOKEN, MULTICA_TASK_ID, etc.).
//
// Warm pooled provider processes (Codex app-server, persistent Claude Code)
// outlive a single task. Rather than baking Task 1's short-lived token into the
// pooled process's static OS environment — where Turn 2 would inherit a stale
// credential and an idle process would hold a token on disk/environ — the
// daemon omits TaskEnvVolatileKeys from the pooled process's static env, sets
// MULTICA_TASK_ENV_FILE, atomically updates the file before each turn, and
// clears it as soon as the turn finishes.
const TaskEnvFileEnv = "MULTICA_TASK_ENV_FILE"

// TaskEnvVolatileKeys is the exhaustive whitelist of per-task environment
// variables that change from turn to turn within a warm provider session and
// are delivered through TaskEnvFileEnv.
//
// Enforcing this whitelist on both write and read ensures a task-env file can
// never overwrite static session/host variables such as PATH, HOME,
// MULTICA_SERVER_URL, or MULTICA_AGENT_ID.
var TaskEnvVolatileKeys = []string{
	"MULTICA_TOKEN",
	"MULTICA_TASK_ID",
	"MULTICA_TASK_CONFIG_ROOT",
	"MULTICA_TASK_SLOT",
	"MULTICA_AUTOPILOT_RUN_ID",
	"MULTICA_AUTOPILOT_ID",
	"MULTICA_QUICK_CREATE_TASK_ID",
	"MULTICA_QUICK_CREATE_ATTACHMENT_IDS",
	"MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_TYPE",
	"MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_ID",
}

var taskEnvVolatileKeySet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(TaskEnvVolatileKeys))
	for _, key := range TaskEnvVolatileKeys {
		set[key] = struct{}{}
	}
	return set
}()

// IsTaskEnvVolatileKey reports whether key belongs to the per-turn dynamic
// environment overlay for warm pooled sessions.
func IsTaskEnvVolatileKey(key string) bool {
	_, ok := taskEnvVolatileKeySet[key]
	return ok
}

type taskEnvFilePayload struct {
	Active bool              `json:"active"`
	Env    map[string]string `json:"env,omitempty"`
}

// WriteTaskEnvFile atomically writes the active turn's whitelisted volatile
// variables to path (mode 0o600).
func WriteTaskEnvFile(path string, sourceEnv map[string]string) error {
	filtered := make(map[string]string, len(TaskEnvVolatileKeys))
	for _, key := range TaskEnvVolatileKeys {
		if val, ok := sourceEnv[key]; ok && val != "" {
			filtered[key] = val
		}
	}
	return writeTaskEnvFilePayload(path, taskEnvFilePayload{
		Active: true,
		Env:    filtered,
	})
}

// ClearTaskEnvFile atomically marks path inactive and removes all volatile
// credentials so an idle pooled provider process holds no live task token.
func ClearTaskEnvFile(path string) error {
	return writeTaskEnvFilePayload(path, taskEnvFilePayload{Active: false})
}

func writeTaskEnvFilePayload(path string, payload taskEnvFilePayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal task env overlay: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".task-env-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp task env overlay: %w", err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(0o600); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chmod temp task env overlay: %w", err)
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp task env overlay: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp task env overlay: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace task env overlay: %w", err)
	}
	cleanupTmp = false
	return nil
}

// ApplyTaskEnvFileOverlay applies the dynamic per-turn task environment when
// TaskEnvFileEnv is set in the current process environment.
//
// When TaskEnvFileEnv is unset (normal cold runs and human CLI usage), this is
// a complete no-op. When set, every key in TaskEnvVolatileKeys is first cleared
// so stale values can never survive, and only whitelisted keys from an active
// overlay are populated. If the file is missing or inactive, volatile keys stay
// unset so the CLI fails closed under inDaemonManagedExecutionContext().
func ApplyTaskEnvFileOverlay() error {
	path := strings.TrimSpace(os.Getenv(TaskEnvFileEnv))
	if path == "" {
		return nil
	}
	for _, key := range TaskEnvVolatileKeys {
		_ = os.Unsetenv(key)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", TaskEnvFileEnv, err)
	}
	var payload taskEnvFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse %s: %w", TaskEnvFileEnv, err)
	}
	if !payload.Active {
		return nil
	}
	for key, val := range payload.Env {
		if !IsTaskEnvVolatileKey(key) {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("set %s from %s: %w", key, TaskEnvFileEnv, err)
		}
	}
	return nil
}

// PreparePooledChildEnv clones baseEnv for a long-lived pooled provider
// process: volatile per-task credentials are stripped from the static OS
// environment (they travel via taskEnvFile instead), TaskEnvFileEnv is set,
// and temp-directory variables are repointed to sessionTempDir so they outlive
// Task 1's per-task temp directory cleanup.
func PreparePooledChildEnv(baseEnv map[string]string, taskEnvFile, sessionTempDir string) map[string]string {
	out := make(map[string]string, len(baseEnv)+2)
	for k, v := range baseEnv {
		if IsTaskEnvVolatileKey(k) {
			continue
		}
		out[k] = v
	}
	out[TaskEnvFileEnv] = taskEnvFile
	if sessionTempDir != "" {
		out["TMPDIR"] = sessionTempDir
		if runtime.GOOS != "windows" {
			out["TMP"] = sessionTempDir
			out["TEMP"] = sessionTempDir
		} else if _, hasClaudeTmp := out["CLAUDE_CODE_TMPDIR"]; hasClaudeTmp {
			out["CLAUDE_CODE_TMPDIR"] = sessionTempDir
		}
	}
	return out
}
