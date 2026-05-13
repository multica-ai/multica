// cerebro-persona-hook is the small binary Claude Code invokes for every
// PreToolUse event when an agent is spawned with persona enabled. It maps
// the tool name to a persona resource_kind, calls persona's /v1/check on
// behalf of the spawned actor, and exits 0 (allow) or 1 (deny).
//
// Drift-correctness rules (from cerebro-integration-plan.md beslutning #8):
//   - Args come on stdin as JSON (NOT a shell flag — quotes break that)
//   - 500ms hard timeout on the persona call (sync path; pending-approval
//     path uses a longer wall-clock budget — see PERSONA_APPROVAL_TIMEOUT)
//   - Fail-closed: any error, timeout, or non-allow → exit 1
//
// JEH-1078: when persona answers `needs_approval`, the hook polls
// persona for that specific request id until the status flips. The
// timeout is configurable via PERSONA_APPROVAL_TIMEOUT (default 5m).
// On timeout, the hook exits with denial — the operator has the audit
// trail to know nobody acted in time.
//
// The default-allow tools (Read, Grep, Glob, etc.) are short-circuited at
// the very top — they exit 0 without ever calling persona.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hvejsel/firtal-persona/sdk/go"
)

// hookInput is the JSON payload sent on stdin. Claude Code's PreToolUse
// hook spec uses tool_name / tool_input / cwd; the e2e harness can use
// the same field names. The aliases (`tool`, `args`, `workdir`) are
// accepted as a fallback so the hook can be smoke-tested with simpler
// payloads.
type hookInput struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	CWD       string         `json:"cwd"`
	// Aliases for direct-invocation testing.
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Workdir string         `json:"workdir"`
}

func (h hookInput) Name() string {
	if h.ToolName != "" {
		return h.ToolName
	}
	return h.Tool
}

func (h hookInput) InputArgs() map[string]any {
	if len(h.ToolInput) > 0 {
		return h.ToolInput
	}
	return h.Args
}

func (h hookInput) Dir() string {
	if h.CWD != "" {
		return h.CWD
	}
	return h.Workdir
}

// toolToKind maps a Claude Code tool name to the persona resource_kind
// registered in policies/claude-code-tools.yaml. Tools not in this map are
// "default-allow" per the plan — Read, Grep, Glob, TodoWrite, ToolSearch,
// Skill, ScheduleWakeup, Monitor, EnterPlanMode, ExitPlanMode,
// AskUserQuestion. The hook short-circuits to exit 0 for those.
var toolToKind = map[string]string{
	"Bash":         "claude.tool.bash",
	"Write":        "claude.tool.write",
	"Edit":         "claude.tool.edit",
	"NotebookEdit": "claude.tool.notebook_edit",
	"WebFetch":     "claude.tool.web_fetch",
	"WebSearch":    "claude.tool.web_search",
	"Task":         "claude.tool.task",
}

const (
	personaTimeout = 500 * time.Millisecond
	// approvalDefaultTimeout is the wall-clock budget for waiting on a
	// pending approval. Five minutes balances "human can react" against
	// "Claude doesn't sit forever on one tool call". Operators can
	// override via PERSONA_APPROVAL_TIMEOUT (Go duration string).
	approvalDefaultTimeout = 5 * time.Minute
	// approvalPollInterval is how often the hook re-queries persona for
	// the request's current status while waiting.
	approvalPollInterval = 2 * time.Second
)

// Claude Code's hook protocol: exit 2 blocks the tool; any other non-zero
// is logged as an error but does NOT block. We deliberately exit 2 on deny
// so persona's decision is enforced. Exit 1 is reserved for "the hook
// itself failed to function" (config missing, malformed input) — those
// also fail-closed, but use exit 2 as well so denial behaviour is uniform
// from Claude's perspective.
const (
	exitAllow  = 0
	exitDeny   = 2
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitDeny)
	}
}

func run() error {
	personaURL := os.Getenv("PERSONA_URL")
	personaToken := os.Getenv("PERSONA_SERVICE_TOKEN")
	actorID := os.Getenv("PERSONA_ACTOR_ID")
	spawnID := os.Getenv("PERSONA_SPAWN_ID")
	if personaURL == "" || personaToken == "" || actorID == "" {
		return fmt.Errorf("missing PERSONA_URL/PERSONA_SERVICE_TOKEN/PERSONA_ACTOR_ID env (refusing to fail open)")
	}
	if spawnID == "" {
		spawnID = "unknown-spawn"
	}

	rawInput, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20)) // 1 MiB cap
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	var input hookInput
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return fmt.Errorf("parse stdin JSON: %w", err)
	}
	tool := input.Name()
	if tool == "" {
		return fmt.Errorf("missing tool_name (or tool) in stdin payload")
	}

	// MCP tools share one resource kind. Anything beginning with mcp__ → mcp.
	kind, gated := toolToKind[tool]
	if !gated && strings.HasPrefix(tool, "mcp__") {
		kind = "claude.tool.mcp"
		gated = true
	}
	if !gated {
		// Default-allow — read, grep, glob, planning tools etc. Exit before
		// the network call so persona never sees them.
		return nil
	}

	client, err := persona.New(persona.Config{Endpoint: personaURL, Token: personaToken})
	if err != nil {
		return fmt.Errorf("init persona client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), personaTimeout)
	defer cancel()

	attrs := map[string]any{
		"tool":    tool,
		"args":    input.InputArgs(),
		"workdir": input.Dir(),
	}
	// JEH-1080: forward the spawning user + group memberships the daemon
	// resolved at claim time as facts. Persona's resolver matches group
	// grants against args.group_ids[] without ever fetching membership
	// itself — keeps persona stateless w.r.t. cerebro's group entity.
	if userID := os.Getenv("MULTICA_SPAWNER_USER_ID"); userID != "" {
		attrs["spawner_user_id"] = userID
	}
	if raw := os.Getenv("MULTICA_SPAWNER_GROUP_IDS"); raw != "" {
		ids := strings.Split(raw, ",")
		clean := make([]string, 0, len(ids))
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				clean = append(clean, id)
			}
		}
		if len(clean) > 0 {
			attrs["group_ids"] = clean
		}
	}

	res, err := client.CheckResource(ctx, actorID, "call-tool", kind, spawnID, attrs)
	if err != nil {
		return fmt.Errorf("persona check failed (deny by default): %w", err)
	}
	// JEH-1078: third state. Wait for the approver before allowing or
	// denying. The CheckResource call itself returned promptly; the
	// wait below uses a fresh, longer-budget context.
	if res.NeedsApproval {
		if res.Pending == nil {
			return fmt.Errorf("Denied by persona: needs_approval without request payload")
		}
		fmt.Fprintf(os.Stderr, "persona: tool %q requires approval (request=%s, expires=%s)\n",
			tool, res.Pending.RequestID, res.Pending.ExpiresAt.Format(time.RFC3339))
		final, perr := waitForApproval(client, res.Pending)
		if perr != nil {
			return fmt.Errorf("Denied by persona: %s", perr.Error())
		}
		if final.Status != persona.ApprovalApproved {
			return fmt.Errorf("Denied by persona: approval %s — %s", final.Status, final.DecisionReason)
		}
		return nil
	}
	if !res.Allowed {
		reason := res.Reason
		if reason == "" {
			reason = "policy denied"
		}
		return fmt.Errorf("Denied by persona: %s", reason)
	}
	return nil
}

// waitForApproval polls persona for the pending request until status
// flips, the configured timeout expires, or the server-side expires_at
// passes. The hook stays sync from Claude Code's perspective — a long
// wall-clock wait is acceptable because the operator is choosing
// "block until human reviews" as part of opting into requires_approval.
func waitForApproval(client *persona.Client, pending *persona.PendingApproval) (*persona.ApprovalRequest, error) {
	timeout := approvalDefaultTimeout
	if v := os.Getenv("PERSONA_APPROVAL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}
	// Cap the wall-clock wait at the request's own expires_at so we
	// don't outlast the server-side window. The expiry worker will
	// flip the row to expired regardless; we exit early to free Claude.
	deadline := time.Now().Add(timeout)
	if !pending.ExpiresAt.IsZero() && pending.ExpiresAt.Before(deadline) {
		deadline = pending.ExpiresAt
	}

	t := time.NewTicker(approvalPollInterval)
	defer t.Stop()

	for {
		// Each individual GET keeps the 500ms persona timeout so the
		// hook is resilient to a flapping persona.
		ctx, cancel := context.WithTimeout(context.Background(), personaTimeout)
		ar, err := client.GetApprovalRequest(ctx, pending.RequestID)
		cancel()
		if err != nil {
			// One transient error isn't fatal — try again on the next
			// tick. Final deadline check below catches sustained
			// failure.
			fmt.Fprintf(os.Stderr, "persona: poll error (will retry): %v\n", err)
		} else if ar != nil && ar.Status != persona.ApprovalPending {
			return ar, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("approval timed out after %s (request %s)", timeout, pending.RequestID)
		}
		<-t.C
	}
}
