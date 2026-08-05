package main

// CEREBRO-PATCH(cerebro-tool-policy-hook-cmd): TECH-2563 — Claude Code PreToolUse
// hook for local-runtime per-tool enforcement.
//
// This subcommand is what the daemon wires as Claude Code's PreToolUse hook
// command (see server/internal/daemon/toolpolicy.go). It is part of the multica
// binary itself, so there is no separate hook binary to build or ship: wherever
// the daemon runs, the hook runs.
//
// Per tool call the provider invokes it with the tool payload on stdin. The hook:
//   1. Parses the provider's before-tool payload.
//   2. POSTs every named tool to the daemon's
//      loopback resolve IPC, which proxies the unified tool-policy chain and
//      long-polls a pending approval.
//   3. Answers with the provider's before-tool protocol:
//        - Claude / Codex / Gemini: exit 0 allow, exit 2 deny (stderr reason).
//        - Cursor: exit 0 with JSON stdout `{"permission":"allow|deny",...}`.
//          Cursor requires JSON on stdout; empty stdout is treated as a hook
//          failure, and with failClosed:true that blocks every tool (FIR-4526).
//
// Transport, input and protocol errors fail closed.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
)

// toolPolicyHookClientTimeout sits just above the daemon's approval wait budget
// (4m) so an enforce-stage Ask that a human approves late still returns a clean
// allow/deny instead of a client-side timeout.
const toolPolicyHookClientTimeout = 5 * time.Minute

const (
	hookExitAllow = 0
	hookExitDeny  = 2
)

// hookProtocolFlag selects the before-tool response protocol. Set by the
// daemon when it writes provider-native hook config (Cursor gets "cursor") so
// detection does not depend on env inheritance or payload field names.
var hookProtocolFlag string

func init() {
	// CEREBRO-PATCH(cursor-tool-policy-hook-json): FIR-4526 — protocol flag.
	cerebroToolPolicyHookCmd.Flags().StringVar(&hookProtocolFlag, "protocol", "", "before-tool response protocol: cursor|claude (empty = auto-detect)")
}

var cerebroToolPolicyHookCmd = &cobra.Command{
	Use:    "cerebro-tool-policy-hook",
	Short:  "Internal: local-runtime before-tool hook for Multica tool-policy enforcement",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		runToolPolicyHook(cmd)
		return nil
	},
}

// runToolPolicyHook reads the before-tool payload, resolves the verdict, and
// answers with the active provider protocol. Deny/fail paths call os.Exit;
// allow returns so the process exits 0.
func runToolPolicyHook(cmd *cobra.Command) {
	raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
	if err != nil {
		// Prefer cursor protocol on early IO failure when the daemon asked for
		// it via --protocol; otherwise fail closed with Claude exit codes.
		failHook(cmd, "unknown", err, usesCursorHookProtocol(raw))
		return
	}
	cursorProto := usesCursorHookProtocol(raw)

	in, err := claudehook.Parse(bytes.NewReader(raw))
	if err != nil {
		failHook(cmd, "unknown", err, cursorProto)
		return
	}
	tool := in.Name()
	if tool == "" || !claudehook.Gated(tool) {
		failHook(cmd, "unknown", fmt.Errorf("tool_name missing from hook payload"), cursorProto)
		return
	}

	port := os.Getenv("MULTICA_DAEMON_PORT")
	if port == "" {
		// No daemon to ask — fail closed.
		failHook(cmd, tool, fmt.Errorf("MULTICA_DAEMON_PORT not set"), cursorProto)
		return
	}

	claimGeneration, _ := strconv.ParseInt(strings.TrimSpace(os.Getenv("MULTICA_TASK_MANDATE_GENERATION")), 10, 64)
	body, _ := json.Marshal(map[string]any{
		"workspace_id":     os.Getenv("MULTICA_WORKSPACE_ID"),
		"agent_id":         os.Getenv("MULTICA_AGENT_ID"),
		"task_id":          os.Getenv("MULTICA_TASK_ID"),
		"claim_generation": claimGeneration,
		"tool_name":        tool,
		"resource_pattern": claudehook.ResourcePattern(tool, in.InputArgs()),
		"args":             in.InputArgs(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), toolPolicyHookClientTimeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%s/tool-policy/resolve", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		failHook(cmd, tool, err, cursorProto)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: toolPolicyHookClientTimeout}).Do(req)
	if err != nil {
		failHook(cmd, tool, err, cursorProto)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		failHook(cmd, tool, fmt.Errorf("daemon returned status %d", resp.StatusCode), cursorProto)
		return
	}

	var out struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		failHook(cmd, tool, err, cursorProto)
		return
	}
	if out.Allowed {
		allowHook(cmd, cursorProto)
		return
	}
	reason := out.Reason
	if reason == "" {
		reason = "blocked by tool policy"
	}
	denyHook(cmd, tool, reason, cursorProto)
}

// usesCursorHookProtocol reports whether the caller expects Cursor's JSON
// stdout permission protocol.
//
// Order of preference (FIR-4526):
//  1. --protocol cursor (baked into .cursor/hooks.json by the daemon — does not
//     depend on env inheritance into the hook child process)
//  2. MULTICA_AGENT_PROVIDER=cursor (daemon-injected agent env)
//  3. Cursor payload markers (cursor_version, hook_event_name=preToolUse)
//
// tool_use_id is intentionally NOT used alone: Claude Code also sends it, so
// it would flip Claude onto Cursor JSON when the flag/env are missing.
func usesCursorHookProtocol(raw []byte) bool {
	switch strings.ToLower(strings.TrimSpace(hookProtocolFlag)) {
	case "cursor":
		return true
	case "claude", "codex", "gemini":
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MULTICA_AGENT_PROVIDER")), "cursor") {
		return true
	}
	var probe struct {
		CursorVersion string `json:"cursor_version"`
		HookEventName string `json:"hook_event_name"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if strings.TrimSpace(probe.CursorVersion) != "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(probe.HookEventName), "preToolUse")
}

func allowHook(cmd *cobra.Command, cursorProto bool) {
	if cursorProto {
		// Cursor: exit 0 + JSON permission. Empty stdout is a hook failure.
		fmt.Fprintln(cmd.OutOrStdout(), `{"permission":"allow"}`)
	}
}

func denyHook(cmd *cobra.Command, tool, reason string, cursorProto bool) {
	msg := fmt.Sprintf("Blocked by Multica tool policy: %s (%s)", tool, reason)
	if cursorProto {
		// Cursor failClosed treats non-JSON / non-zero exits as hook failure.
		// Emit an explicit deny decision on stdout so the agent sees the reason.
		_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
			"permission":    "deny",
			"agent_message": msg,
			"user_message":  msg,
		})
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), msg)
	osExit(hookExitDeny)
}

// failHook applies fail-closed deny for transport/IO errors.
func failHook(cmd *cobra.Command, tool string, err error, cursorProto bool) {
	denyHook(cmd, tool, fmt.Sprintf("enforcement unreachable: %v", err), cursorProto)
	if cursorProto {
		// Cursor already got a JSON deny above; exit 0 so failClosed does not
		// rewrite it as "hook returned no output".
		return
	}
	// Claude path: denyHook already exited.
}

// osExit is a package var so tests can intercept the exit code instead of
// terminating the test binary.
var osExit = os.Exit
