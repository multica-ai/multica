package daemon

// CEREBRO-PATCH(daemon-tool-policy-ipc): TECH-2563 — local-runtime per-tool
// enforcement client coupling for Claude Code.
//
// The server already resolves a local-runtime tool call against the unified
// tool-policy chain (handler.ResolveDaemonToolPolicy) and stages the rollout
// from workspace settings (off → observe → enforce). What was missing is the
// client half: nothing on the local CLI side actually CALLS that endpoint
// before running a tool. This file is that coupling for Claude Code.
//
// Shape (mirrors the repo-checkout IPC in health.go): Claude Code runs a
// PreToolUse hook — the `multica cerebro-tool-policy-hook` subcommand — which
// POSTs the tool call to the daemon's loopback health server (already reachable
// from inside the agent sandbox). The daemon proxies it to the server with its
// OWN credential, so no token ever enters the agent process environment. The
// daemon long-polls a pending approval, applies the staged-mode fail direction
// on a transport error, and answers the hook allow/deny. The hook exits 0
// (allow) or 2 (block the tool) per Claude's hook protocol.
//
// Gating: the hook is only wired when the workspace has opted in — the claim
// carries the resolved stage (LocalToolPolicyStage) and prepareToolPolicySpawn
// returns nil for the default "off". So when the feature is off there is no
// hook, no round-trip, and no new failure mode: true "no behaviour change".

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
// which derives the runtime/owner from the agent row. stage is the spawn-time
// rollout stage, used ONLY to pick the fail direction when the server is
// unreachable (observe → allow, enforce → deny).
type toolPolicyResolveRequest struct {
	WorkspaceID     string         `json:"workspace_id"`
	AgentID         string         `json:"agent_id"`
	ToolName        string         `json:"tool_name"`
	ResourcePattern string         `json:"resource_pattern"`
	Args            map[string]any `json:"args"`
	Stage           string         `json:"stage"`
}

// toolPolicyResolveResponse is the daemon's answer to the hook: a single
// definitive allow/deny plus a human-readable reason for the hook's stderr.
type toolPolicyResolveResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// handleToolPolicyResolve is the daemon loopback handler the health server
// mounts at toolPolicyResolvePath. It proxies the tool call to the server and
// returns a definitive allow/deny, applying the staged-mode fail direction on a
// transport error so observe never blocks and enforce never silently allows.
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
// returns the final (allowed, reason). On any transport/server error it folds
// to the spawn-time stage: observe (and off/unknown) fail open so a dry run or a
// disabled feature never stalls work; enforce fails closed so a server blip
// cannot silently bypass a deny.
func (d *Daemon) resolveToolPolicy(ctx context.Context, req toolPolicyResolveRequest) (bool, string) {
	res, err := d.client.ResolveToolPolicy(ctx, req.WorkspaceID, req.AgentID, req.ToolName, req.ResourcePattern, req.Args)
	if err != nil {
		return toolPolicyFailDirection(req.Stage, req.ToolName, err)
	}

	if res.Decision == "pending" && res.ApprovalID != "" {
		allowed, reason, waitErr := d.awaitToolPolicyApproval(ctx, req.WorkspaceID, req.ToolName, res.ApprovalID)
		if waitErr != nil {
			return toolPolicyFailDirection(req.Stage, req.ToolName, waitErr)
		}
		return allowed, reason
	}

	reason := res.Reason
	if !res.Allowed && reason == "" {
		reason = "blocked by tool policy"
	}
	return res.Allowed, reason
}

// toolPolicyFailDirection picks the fail-open/fail-closed answer for a transport
// error, keyed on the spawn-time stage. enforce fails closed; everything else
// (observe, off, unknown) fails open. Logged so an operator can see when the
// gate degraded.
func toolPolicyFailDirection(stage, tool string, err error) (bool, string) {
	if stage == "enforce" {
		slog.Warn("local tool-policy: resolve failed under enforce — failing closed",
			"tool", tool, "error", err)
		return false, "tool policy unavailable (failing closed under enforce)"
	}
	slog.Warn("local tool-policy: resolve failed — failing open (observe/off)",
		"tool", tool, "stage", stage, "error", err)
	return true, ""
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
	Env          map[string]string
}

// prepareToolPolicySpawn wires Claude Code's PreToolUse hook to the local
// tool-policy resolve IPC. Returns nil (no wiring, existing behaviour) when:
//   - the workspace has not opted in (stage off / empty) — the default, so an
//     untouched workspace pays nothing and changes nothing;
//   - the provider is not Claude Code — Codex/Cursor/Gemini land in follow-ups
//     (TECH-2563 priority order); Claude is wired first because it already has a
//     PreToolUse hook to carry the call.
//
// The hook command is this same `multica` binary invoked as the
// cerebro-tool-policy-hook subcommand, so there is no separate binary to build
// or deploy — wherever the daemon runs, the hook runs. The stage is injected as
// CEREBRO_TOOLPOLICY_STAGE only for the hook's own transport-error fail
// direction; workspace_id / agent_id / daemon port come from the MULTICA_* env
// already injected into the spawn.
func (d *Daemon) prepareToolPolicySpawn(provider, stage, workdir string) (*toolPolicySpawn, error) {
	if stage == "" || stage == "off" {
		return nil, nil
	}
	if provider != "claude" {
		return nil, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve multica executable for tool-policy hook: %w", err)
	}

	settingsPath, err := writeToolPolicySettingsJSON(workdir, exe)
	if err != nil {
		return nil, fmt.Errorf("write tool-policy settings.json: %w", err)
	}

	return &toolPolicySpawn{
		SettingsPath: settingsPath,
		Env: map[string]string{
			"CEREBRO_TOOLPOLICY_STAGE": stage,
		},
	}, nil
}

// writeToolPolicySettingsJSON drops a settings file into <workdir>/.claude/ that
// wires the cerebro-tool-policy-hook for every PreToolUse event. Claude Code
// reads it via --settings (the daemon passes the absolute path at spawn). The
// filename is distinct from the persona hook's settings.json so both hooks can
// coexist when both are enabled.
func writeToolPolicySettingsJSON(workdir, exe string) (string, error) {
	dir := filepath.Join(workdir, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Claude runs the hook command through a shell; double-quote the exe path
	// so a path with spaces still resolves. The path never contains a double
	// quote, so this is safe without further escaping.
	command := fmt.Sprintf("%q cerebro-tool-policy-hook", exe)
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "*",
					"hooks": []map[string]any{
						{"type": "command", "command": command},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	path := filepath.Join(dir, "cerebro-tool-policy-settings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
