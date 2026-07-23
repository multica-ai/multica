package daemon

// Local-runtime per-tool enforcement client coupling for Cerebro runtimes.
//
// The server resolves a local-runtime tool call against the unified tool-policy
// chain (handler.ResolveDaemonToolPolicy). This file couples every supported
// local CLI to that endpoint before dispatch.
//
// Shape (mirrors the repo-checkout IPC in health.go): each supported provider
// runs a native before-call hook — the `multica cerebro-tool-policy-hook`
// subcommand — which
// POSTs the tool call to the daemon's loopback health server (already reachable
// from inside the agent sandbox). The daemon proxies it to the server with its
// OWN credential, so no token ever enters the agent process environment. The
// daemon long-polls a pending approval and answers allow/deny. Transport errors
// fail closed. The hook exits 0 (allow) or 2 (block the tool).
//
// prepareToolPolicySpawn installs the mandatory provider adapter for Claude,
// Codex, Cursor and Gemini.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// toolPolicyResolvePath is the daemon loopback route the hook POSTs to.
	toolPolicyResolvePath = "/tool-policy/resolve"
	// toolPolicyPollInterval / WaitBudget bound the wait on an enforce-stage
	// Ask. The budget stays under Claude Code's per-hook patience so a tool
	// that needs human approval resolves to a clean allow/deny rather than a
	// transport timeout. A still-pending ask at the deadline is reported as
	// not-allowed; the approval stays open in the inbox for a retry.
	toolPolicyPollInterval = 2 * time.Second
	toolPolicyWaitBudget   = 4 * time.Minute
)

// toolPolicyResolveRequest is the IPC body the hook POSTs to the daemon. The
// daemon trusts none of it for auth — it only forwards the call to the server,
// which derives the runtime/owner from the agent row.
type toolPolicyResolveRequest struct {
	WorkspaceID     string         `json:"workspace_id"`
	AgentID         string         `json:"agent_id"`
	TaskID          string         `json:"task_id"`
	ToolName        string         `json:"tool_name"`
	ResourcePattern string         `json:"resource_pattern"`
	Args            map[string]any `json:"args"`
}

// toolPolicyResolveResponse is the daemon's answer to the hook: a single
// definitive allow/deny plus a human-readable reason for the hook's stderr.
type toolPolicyResolveResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// handleToolPolicyResolve is the daemon loopback handler the health server
// mounts at toolPolicyResolvePath. It proxies the tool call to the server and
// returns a definitive allow/deny. Transport errors fail closed.
func (d *Daemon) handleToolPolicyResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req toolPolicyResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ToolName == "" {
		http.Error(w, "tool_name is required", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}

	allowed, reason := d.resolveToolPolicy(r.Context(), req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toolPolicyResolveResponse{Allowed: allowed, Reason: reason})
}

// resolveToolPolicy performs the server round-trip + approval long-poll and
// returns the final (allowed, reason). Transport/server errors fail closed.
func (d *Daemon) resolveToolPolicy(ctx context.Context, req toolPolicyResolveRequest) (bool, string) {
	res, err := d.client.ResolveToolPolicy(ctx, req.WorkspaceID, req.AgentID, req.TaskID, req.ToolName, req.ResourcePattern, req.Args)
	if err != nil {
		return toolPolicyFailDirection(req.ToolName, err)
	}

	if res.Decision == "pending" && res.ApprovalID != "" {
		allowed, reason, waitErr := d.awaitToolPolicyApproval(ctx, req.WorkspaceID, req.ToolName, res.ApprovalID)
		if waitErr != nil {
			return toolPolicyFailDirection(req.ToolName, waitErr)
		}
		return allowed, reason
	}

	reason := res.Reason
	if !res.Allowed && reason == "" {
		reason = "blocked by tool policy"
	}
	return res.Allowed, reason
}

// toolPolicyFailDirection is the single fail-closed transport policy.
func toolPolicyFailDirection(tool string, err error) (bool, string) {
	slog.Warn("local tool-policy: resolve failed — failing closed", "tool", tool, "error", err)
	return false, "tool policy unavailable (failing closed)"
}

// awaitToolPolicyApproval long-polls a pending local-runtime tool approval until
// a human decides or the wait budget elapses. A still-pending ask at the
// deadline is reported as not-allowed with a clear reason; the ask stays open in
// the inbox so the agent can retry once it is approved.
func (d *Daemon) awaitToolPolicyApproval(ctx context.Context, workspaceID, tool, approvalID string) (bool, string, error) {
	slog.Info("local tool-policy awaiting approval", "workspace_id", workspaceID, "tool", tool, "approval_id", approvalID)
	deadline := time.Now().Add(toolPolicyWaitBudget)
	ticker := time.NewTicker(toolPolicyPollInterval)
	defer ticker.Stop()
	for {
		allowed, decision, reason, err := d.client.PollToolPolicyApproval(ctx, workspaceID, approvalID)
		if err != nil {
			return false, "", err
		}
		if decision != "pending" {
			if !allowed && reason == "" {
				reason = "tool approval denied"
			}
			return allowed, reason, nil
		}
		if time.Now().After(deadline) {
			return false, "approval still pending — request is in your inbox, retry once approved", nil
		}
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// toolPolicySpawn carries the Claude-Code --settings file and the env the daemon
// merges into a spawn so the PreToolUse hook calls back to this daemon.
type toolPolicySpawn struct {
	SettingsPath string
}

// prepareToolPolicySpawn wires each target provider to the local resolve IPC.
//
// The hook command is this same `multica` binary invoked as the
// cerebro-tool-policy-hook subcommand, so there is no separate binary to build
// or deploy — wherever the daemon runs, the hook runs. Workspace/agent identity
// and the daemon port come from the MULTICA_* env already injected at spawn.
func (d *Daemon) prepareToolPolicySpawn(provider, workdir, providerHome string, fastMode bool) (*toolPolicySpawn, error) {
	if provider != "claude" && provider != "codex" && provider != "cursor" && provider != "gemini" {
		return nil, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve multica executable for tool-policy hook: %w", err)
	}

	settingsPath, err := writeToolPolicySettingsJSON(provider, workdir, providerHome, exe, fastMode)
	if err != nil {
		return nil, fmt.Errorf("write %s tool-policy settings: %w", provider, err)
	}

	return &toolPolicySpawn{
		SettingsPath: settingsPath,
	}, nil
}

// writeToolPolicySettingsJSON installs the provider-native configuration that
// wires cerebro-tool-policy-hook for every tool event. Claude, Gemini and
// Cursor read a task-local file; Codex reads hooks.json from its managed home.
func writeToolPolicySettingsJSON(provider, workdir, providerHome, exe string, fastMode bool) (string, error) {
	var dir, filename string
	var settings map[string]any
	command := fmt.Sprintf("%q cerebro-tool-policy-hook", exe)
	switch provider {
	case "claude":
		dir, filename = filepath.Join(workdir, ".claude"), "cerebro-tool-policy-settings.json"
		settings = claudeStyleToolPolicySettings("PreToolUse", command)
	case "gemini":
		dir, filename = filepath.Join(workdir, ".gemini"), "settings.json"
		settings = claudeStyleToolPolicySettings("BeforeTool", command)
	case "cursor":
		dir, filename = filepath.Join(workdir, ".cursor"), "hooks.json"
		settings = map[string]any{
			"version": 1,
			"hooks": map[string]any{
				"preToolUse": []map[string]any{{
					"type": "command", "command": command, "failClosed": true, "timeout": 300,
				}},
			},
		}
	case "codex":
		if providerHome == "" {
			return "", fmt.Errorf("codex home is required for tool-policy hooks")
		}
		dir, filename = providerHome, "hooks.json"
		settings = claudeStyleToolPolicySettings("PreToolUse", command)
	default:
		return "", nil
	}
	if provider == "claude" && fastMode {
		settings["fastMode"] = true
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Claude runs the hook command through a shell; double-quote the exe path
	// so a path with spaces still resolves. The path never contains a double
	// quote, so this is safe without further escaping.
	data, _ := json.MarshalIndent(settings, "", "  ")
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func claudeStyleToolPolicySettings(event, command string) map[string]any {
	return map[string]any{
		"hooks": map[string]any{
			event: []map[string]any{
				{
					"matcher": "*",
					"hooks": []map[string]any{
						{"type": "command", "command": command},
					},
				},
			},
		},
	}
}
