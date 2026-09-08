// Package governance runs workspace-configured shell hooks before issue
// comment and status mutations. Hook failure is fail-safe default-deny per
// multica-org-governance ORGANIZATION.md §5.8.4.
package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultHookTimeout = 30 * time.Second
	defaultPreComment  = "scripts/pre-comment"
	defaultPreStatus   = "scripts/pre-status"
)

// Config carries server-level governance defaults (from deployment env).
type Config struct {
	Root    string
	Timeout time.Duration
}

// WorkspaceHooks is the resolved hook contract for one workspace.
type WorkspaceHooks struct {
	PreComment string
	PreStatus  string
	Timeout    time.Duration
}

// HookDeniedError is returned when a hook exits non-zero. Stderr is surfaced
// to API callers so the agent CLI can show actionable gate output.
type HookDeniedError struct {
	Hook   string
	Stderr string
}

func (e *HookDeniedError) Error() string {
	if strings.TrimSpace(e.Stderr) != "" {
		return strings.TrimSpace(e.Stderr)
	}
	return fmt.Sprintf("governance hook %q denied the operation", e.Hook)
}

// HookFailedError is returned when a configured hook cannot be executed
// (missing, not executable, timeout, spawn error). Fail-safe default-deny.
type HookFailedError struct {
	Hook string
	Err  error
}

func (e *HookFailedError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("governance hook %q failed: %v", e.Hook, e.Err)
	}
	return fmt.Sprintf("governance hook %q failed", e.Hook)
}

func (e *HookFailedError) Unwrap() error { return e.Err }

// ParseWorkspaceHooks resolves hook paths from workspace settings JSON with
// server-level fallbacks. When no hook is configured, the corresponding path
// is empty and the caller should skip invocation (allow).
func ParseWorkspaceHooks(settingsJSON []byte, server Config) WorkspaceHooks {
	hooks := WorkspaceHooks{Timeout: server.Timeout}
	if hooks.Timeout <= 0 {
		hooks.Timeout = defaultHookTimeout
	}

	var settings map[string]json.RawMessage
	if len(settingsJSON) > 0 {
		_ = json.Unmarshal(settingsJSON, &settings)
	}

	var gov struct {
		Hooks struct {
			PreComment string `json:"pre_comment"`
			PreStatus  string `json:"pre_status"`
		} `json:"hooks"`
		TimeoutSeconds *int `json:"timeout_seconds"`
	}
	if raw, ok := settings["governance"]; ok {
		_ = json.Unmarshal(raw, &gov)
	}
	if gov.TimeoutSeconds != nil && *gov.TimeoutSeconds > 0 {
		hooks.Timeout = time.Duration(*gov.TimeoutSeconds) * time.Second
	}

	hooks.PreComment = resolveHookPath(gov.Hooks.PreComment, server.Root, defaultPreComment)
	hooks.PreStatus = resolveHookPath(gov.Hooks.PreStatus, server.Root, defaultPreStatus)
	return hooks
}

func resolveHookPath(explicit, root, defaultRel string) string {
	path := strings.TrimSpace(explicit)
	if path == "" && root != "" {
		path = defaultRel
	}
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, path)
}

// RunPreComment executes the workspace pre-comment hook when configured.
func RunPreComment(ctx context.Context, ws WorkspaceHooks, env map[string]string, commentFile string) error {
	if ws.PreComment == "" {
		return nil
	}
	if commentFile == "" {
		return &HookFailedError{Hook: "pre_comment", Err: errors.New("comment file path is required")}
	}
	args := []string{commentFile}
	return runHook(ctx, "pre_comment", ws.PreComment, ws.Timeout, env, args)
}

// RunPreStatus executes the workspace pre-status hook when configured.
func RunPreStatus(ctx context.Context, ws WorkspaceHooks, env map[string]string, args []string) error {
	if ws.PreStatus == "" {
		return nil
	}
	return runHook(ctx, "pre_status", ws.PreStatus, ws.Timeout, env, args)
}

func runHook(ctx context.Context, name, script string, timeout time.Duration, env map[string]string, args []string) error {
	if timeout <= 0 {
		timeout = defaultHookTimeout
	}
	info, err := os.Stat(script)
	if err != nil {
		return &HookFailedError{Hook: name, Err: fmt.Errorf("hook script not found: %w", err)}
	}
	if info.IsDir() {
		return &HookFailedError{Hook: name, Err: fmt.Errorf("hook script is a directory")}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "/bin/bash", script)
	if len(args) > 0 {
		cmd.Args = append(cmd.Args, args...)
	}
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return &HookFailedError{Hook: name, Err: fmt.Errorf("hook timed out after %s", timeout)}
		}
		if _, ok := err.(*exec.ExitError); ok {
			return &HookDeniedError{Hook: name, Stderr: stderr.String()}
		}
		return &HookFailedError{Hook: name, Err: err}
	}
	return nil
}
