package main

// CEREBRO-PATCH(cerebro-tool-policy-hook-cmd): TECH-2563 — Claude Code PreToolUse
// hook for local-runtime per-tool enforcement.
//
// This subcommand is what the daemon wires as Claude Code's PreToolUse hook
// command (see server/internal/daemon/toolpolicy.go). It is part of the multica
// binary itself, so there is no separate hook binary to build or ship: wherever
// the daemon runs, the hook runs.
//
// Per tool call Claude Code invokes it with the tool payload on stdin. The hook:
//   1. Short-circuits default-allow tools (Read/Grep/Glob/planning) with no IO.
//   2. POSTs gated tools (Bash/Write/Edit/WebFetch/Task/mcp__*) to the daemon's
//      loopback resolve IPC, which proxies the unified tool-policy chain and
//      long-polls a pending approval.
//   3. Exits 0 to allow or 2 to block (Claude Code's hook protocol: exit 2
//      blocks the tool and surfaces stderr to the model).
//
// Fail direction on a transport error mirrors the daemon: under enforce it fails
// closed (exit 2); under observe / off / unknown it fails open (exit 0) so a dry
// run or a disabled feature never stalls work. Its own input errors (an
// unparseable payload, a missing daemon port) fail open — a malformed hook
// payload is a CLI/version mismatch, not a policy decision.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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

var cerebroToolPolicyHookCmd = &cobra.Command{
	Use:    "cerebro-tool-policy-hook",
	Short:  "Internal: Claude Code PreToolUse hook for local-runtime tool-policy enforcement",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		runToolPolicyHook(cmd)
		return nil
	},
}

// runToolPolicyHook reads the PreToolUse payload, resolves the verdict, and
// exits with the Claude hook code. It never returns on a deny (os.Exit(2)); on
// allow it returns so the process exits 0.
func runToolPolicyHook(cmd *cobra.Command) {
	stage := os.Getenv("CEREBRO_TOOLPOLICY_STAGE")

	in, err := claudehook.Parse(cmd.InOrStdin())
	if err != nil {
		// Unparseable payload — not a policy decision. Allow.
		return
	}
	tool := in.Name()
	if tool == "" || !claudehook.Gated(tool) {
		// Default-allow tool (or no tool) — no resolve round-trip.
		return
	}

	port := os.Getenv("MULTICA_DAEMON_PORT")
	if port == "" {
		// No daemon to ask — fail per stage.
		failHook(stage, tool, fmt.Errorf("MULTICA_DAEMON_PORT not set"))
		return
	}

	body, _ := json.Marshal(map[string]any{
		"workspace_id":     os.Getenv("MULTICA_WORKSPACE_ID"),
		"agent_id":         os.Getenv("MULTICA_AGENT_ID"),
		"tool_name":        tool,
		"resource_pattern": claudehook.ResourcePattern(tool, in.InputArgs()),
		"args":             in.InputArgs(),
		"stage":            stage,
	})

	ctx, cancel := context.WithTimeout(context.Background(), toolPolicyHookClientTimeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%s/tool-policy/resolve", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		failHook(stage, tool, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: toolPolicyHookClientTimeout}).Do(req)
	if err != nil {
		failHook(stage, tool, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		failHook(stage, tool, fmt.Errorf("daemon returned status %d", resp.StatusCode))
		return
	}

	var out struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		failHook(stage, tool, err)
		return
	}
	if out.Allowed {
		return
	}
	reason := out.Reason
	if reason == "" {
		reason = "blocked by tool policy"
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Blocked by Multica tool policy: %s (%s)\n", tool, reason)
	osExit(hookExitDeny)
}

// failHook applies the fail direction for a transport/IO error: deny under
// enforce, allow otherwise.
func failHook(stage, tool string, err error) {
	if stage == "enforce" {
		fmt.Fprintf(os.Stderr, "Blocked by Multica tool policy: %s (enforcement unreachable: %v)\n", tool, err)
		osExit(hookExitDeny)
		return
	}
	// observe / off / unknown — allow, but leave a breadcrumb on stderr.
	fmt.Fprintf(os.Stderr, "Multica tool policy: allow (resolve unavailable under %q stage: %v)\n", stage, err)
}

// osExit is a package var so tests can intercept the exit code instead of
// terminating the test binary.
var osExit = os.Exit
