package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// reasonixProjectConfigFile is Reasonix's project-scoped config. Reasonix
// resolves configuration as flag > project ./reasonix.toml > user config.toml >
// defaults, rooted at the session cwd — which for a task is the workdir — so a
// file written here binds to this task only and leaves the runtime owner's
// interactive sessions alone.
const reasonixProjectConfigFile = "reasonix.toml"

// reasonixUserConfigFile is the runtime owner's own Reasonix config; where it
// lives is resolved in reasonix_user_config.go. It is never written to — the
// daemon only reads the permissions the owner set there.
const reasonixUserConfigFile = "config.toml"

// reasonixAskTool is the Reasonix tool the per-task config denies.
const reasonixAskTool = "ask"

// reasonixProjectConfigHeader precedes the rendered [permissions] table.
//
// `ask` puts a multiple-choice question to a human mid-turn. An unattended task
// has no human: Reasonix wires an asker for every ACP session, so the runtime's
// own "no interactive user — decide for yourself" fallback is unreachable, the
// daemon can only refuse the question, and Reasonix reads a refusal as a
// dismissal and cancels the whole turn. The task fails over a decision the model
// could have made itself.
//
// A deny rule is the one lever that covers the whole turn. Reasonix checks deny
// rules before the read-only allow fallthrough, so it applies to `ask` despite
// `ask` being read-only, and the executor, the planner, and sub-agents all
// decide against the same policy — where a tool-call hook would only ever reach
// the executor. The model gets "denied by permission policy" back as an ordinary
// tool result and keeps working.
const reasonixProjectConfigHeader = `# Managed by Multica. Written per task, removed when the task env is cleaned up.
# Edits are not preserved.
#
# [permissions] restates the runtime owner's own table from the Reasonix user
# config, with the ask tool added to deny: no human can answer a question in an
# unattended task, and an unanswered question cancels the Reasonix turn. Denying
# the tool instead lets the model take its own decision and keep going.

`

// reasonixOwnerPermissions returns the [permissions] table the runtime owner
// configured, or nil when there is no config or no such table. An unreadable or
// unparseable config is an error: the caller must not write a permissions table
// it could not derive from the owner's, because the project file replaces the
// owner's rules rather than adding to them.
func reasonixOwnerPermissions(path string) (map[string]any, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read reasonix user config %s: %w", path, err)
	}
	var cfg map[string]any
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse reasonix user config %s: %w", path, err)
	}
	raw, ok := cfg["permissions"]
	if !ok || raw == nil {
		return nil, nil
	}
	perms, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reasonix user config %s: [permissions] is %T, want a table", path, raw)
	}
	return perms, nil
}

// withReasonixAskDenied copies the owner's permissions table and adds `ask` to
// its deny list. The whole table is carried over, not just deny: the project
// file overrides what it declares, so restating every key the owner set is what
// keeps their policy intact whether Reasonix merges these layers per key or per
// table.
func withReasonixAskDenied(ownerPermissions map[string]any) (map[string]any, error) {
	deny, err := reasonixDenyList(ownerPermissions["deny"])
	if err != nil {
		return nil, err
	}
	merged := make(map[string]any, len(ownerPermissions)+1)
	for k, v := range ownerPermissions {
		merged[k] = v
	}
	for _, rule := range deny {
		if rule == reasonixAskTool {
			merged["deny"] = deny
			return merged, nil
		}
	}
	merged["deny"] = append(deny, reasonixAskTool)
	return merged, nil
}

// reasonixDenyList reads the owner's deny rules as strings. A shape this cannot
// restate faithfully is an error rather than a silent reset to ["ask"], which
// would drop every rule the owner wrote.
func reasonixDenyList(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("reasonix user config: [permissions] deny is %T, want an array of strings", raw)
	}
	rules := make([]string, 0, len(entries)+1)
	for _, entry := range entries {
		rule, ok := entry.(string)
		if !ok {
			return nil, fmt.Errorf("reasonix user config: [permissions] deny entry is %T, want a string", entry)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// reasonixProjectConfig renders the task's reasonix.toml from the owner's config
// at userConfigPath (which need not exist).
func reasonixProjectConfig(userConfigPath string) ([]byte, error) {
	ownerPermissions, err := reasonixOwnerPermissions(userConfigPath)
	if err != nil {
		return nil, err
	}
	permissions, err := withReasonixAskDenied(ownerPermissions)
	if err != nil {
		return nil, err
	}
	body, err := toml.Marshal(map[string]any{"permissions": permissions})
	if err != nil {
		return nil, fmt.Errorf("encode reasonix project config: %w", err)
	}
	return append([]byte(reasonixProjectConfigHeader), body...), nil
}

// writeReasonixProjectConfig lays down the per-task reasonix.toml in workDir,
// denying the `ask` tool on top of the runtime owner's own permissions.
// taskEnv is the agent's sanitized custom_env, which decides — together with the
// daemon's own environment — which user config the Reasonix child will load
// (reasonix_user_config.go).
//
// A reasonix.toml that came with the repository is left untouched: sidecar
// cleanup is a pure deletion of paths the daemon created, so it must never
// overwrite user content. That case is a warning rather than a failure — the
// task still runs, `ask` stays available, and a question that does arrive hits
// the existing fail-closed permission handling in the reasonix backend.
//
// The same holds when the owner's config cannot be read or restated: the daemon
// writes nothing rather than a table that silently drops their deny rules.
func writeReasonixProjectConfig(workDir string, taskEnv map[string]string, manifest *sidecarManifest, logger *slog.Logger) error {
	if workDir == "" {
		return nil
	}
	userConfig := reasonixEnv(taskEnv).userConfigLoadPath()
	content, err := reasonixProjectConfig(userConfig)
	if err != nil {
		if logger != nil {
			logger.Warn("execenv: cannot restate the reasonix user permissions; leaving the task without a project config — the reasonix ask tool stays enabled for this task",
				"user_config", userConfig,
				"error", err,
			)
		}
		return nil
	}
	path := filepath.Join(workDir, reasonixProjectConfigFile)
	err = recordWriteFile(path, content, 0o644, manifest)
	if errors.Is(err, errPathPreExists) {
		if logger != nil {
			logger.Warn("execenv: project reasonix.toml already exists; leaving it untouched — the reasonix ask tool stays enabled for this task",
				"path", path,
			)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", reasonixProjectConfigFile, err)
	}
	return nil
}
