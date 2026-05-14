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

// resourceIDFromInput pulls the operator-meaningful identifier out of a
// CEREBRO-PATCH(main): persona integration changes.
// Claude tool_input object so sandbox grants can refine via
// resource_pattern (W4.7). The mapping is per-tool because Claude's
// tool_input shape differs:
//
//	Bash         → command
//	Edit/Write   → file_path
//	Read         (default-allow today; included for forward-compat)
//	NotebookEdit → notebook_path
//	WebFetch     → url
//	WebSearch    → query
//	Task         → description
//	mcp__*       → first string-valued arg, or "" (caller falls back)
//
// Returns "" when the field is missing or non-string; the caller then
// falls back to spawnID so pattern="*" still matches and the existing
// allow-all behaviour is preserved when no refined pattern exists.
func resourceIDFromInput(tool string, args map[string]any) string {
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch {
	case tool == "Bash":
		// CEREBRO-PATCH(main-bash-resourceid): JEH-1182 — for Bash we
		// pass the extracted binary as resourceID instead of the raw
		// command. Persona's grant matcher uses path.Match, where `*`
		// does not cross `/` — so pattern "curl*" never matches a
		// command containing a URL like "curl https://example.com".
		// Sending the bare binary lets sandbox-policy grants be written
		// as one entry per binary (pattern="curl", pattern="kubectl")
		// without juggling glob escapes. The full command stays
		// available to policies via attrs.args.command and
		// attrs.shell_binary; only the matched resource_id changes.
		if cmd := pick("command"); cmd != "" {
			if bin := shellBinaryFromCommand(cmd); bin != "" {
				return bin
			}
			return cmd
		}
		return ""
	case tool == "Edit", tool == "Write", tool == "Read":
		return pick("file_path", "path")
	case tool == "NotebookEdit":
		return pick("notebook_path", "file_path")
	case tool == "WebFetch":
		return pick("url")
	case tool == "WebSearch":
		return pick("query")
	case tool == "Task":
		return pick("description", "subagent_type")
	case strings.HasPrefix(tool, "mcp__"):
		// MCP tools have varying shapes; pick the first non-empty string
		// arg so a per-server grant pattern can still narrow.
		for _, v := range args {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	return ""
}

// CEREBRO-PATCH(main-shell-binary): JEH-1182 — shell_binary attr for sandbox-policy.
//
// shellBinaryFromCommand returns the binary that a Bash command would
// execute, or "" when we can't confidently determine it. The result is
// sent to persona as attrs.shell_binary so sandbox-policy grants on
// claude.tool.bash can match the allowlist/denylist independent of the
// full command string.
//
// Parsing rules (kept deliberately simple — this is policy gating, not
// a full shell parser):
//   - Skip leading env-var assignments (FOO=bar baz → baz)
//   - Strip path prefix from the head (/usr/bin/curl → curl, ./foo → foo)
//   - Unwrap one level of `sh -c <inner>` / `bash -c <inner>` wrapper
//     so the policy sees the real binary, not the shell launcher
//   - Return "" for empty input — caller decides the default action
//
// Out of scope (policy should treat the wrapper binary as the head):
//   - Pipelines (`a | b`), command-chaining (`a && b`, `a; b`)
//   - Subshells / command substitution (`$(curl ...)`)
//   - Multi-statement `sh -c "cd /tmp && curl ..."`
//
// A policy author who wants to block these constructs entirely can
// deny the shell binaries (sh, bash, dash, zsh) outright; the head
// binary is exposed precisely so the simpler rule works.
func shellBinaryFromCommand(cmd string) string {
	tokens := splitShellWords(strings.TrimSpace(cmd))
	i := 0
	for i < len(tokens) && isEnvAssignment(tokens[i]) {
		i++
	}
	if i >= len(tokens) {
		return ""
	}
	head := baseName(tokens[i])
	// Unwrap a single level of `sh -c <inner>`. We recurse once; the
	// recursion already strips a second wrapper, but pathological
	// quadruple-wrapping is out of scope.
	if isShellLauncher(head) && i+2 < len(tokens) && tokens[i+1] == "-c" {
		return shellBinaryFromCommand(tokens[i+2])
	}
	return head
}

// splitShellWords splits a shell command into whitespace-separated
// tokens, treating matched single- or double-quoted runs as a single
// token (stripping the quote characters). This is not a full shell
// parser — backslash escapes, here-documents, and command substitution
// are not handled — but it is enough to identify the head binary of
// most operator-written Bash commands.
func splitShellWords(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// isEnvAssignment reports whether s looks like a leading NAME=VALUE
// shell env-var assignment (which precedes the actual command, e.g.
// `FOO=bar curl ...`). NAME must match [A-Za-z_][A-Za-z0-9_]*; the
// VALUE half may be anything including empty.
func isEnvAssignment(s string) bool {
	eq := strings.IndexByte(s, '=')
	if eq <= 0 {
		return false
	}
	name := s[:eq]
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			// ok
		case i > 0 && r >= '0' && r <= '9':
			// ok
		default:
			return false
		}
	}
	return true
}

// baseName returns the trailing path component of p (POSIX-style).
// Used to normalise "/usr/bin/curl" and "./curl" both to "curl" before
// matching against the policy allowlist.
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// isShellLauncher reports whether name is one of the common POSIX-shell
// binaries that policy authors expect us to unwrap (so `sh -c "curl ..."`
// gates on "curl", not on "sh"). zsh/dash are included because daemons
// occasionally invoke them as login shells on macOS / Alpine.
func isShellLauncher(name string) bool {
	switch name {
	case "sh", "bash", "dash", "zsh":
		return true
	}
	return false
}

// mcpResourceKind reverses Claude's mcp__<server>__<tool> naming into a
// per-server persona kind ("claude.tool.mcp.<server>"). When the input
// is malformed (no server segment), returns the catch-all
// "claude.tool.mcp" so the gating still applies — better to deny on a
// missing grant than to slip through.
func mcpResourceKind(tool string) string {
	rest := strings.TrimPrefix(tool, "mcp__")
	server, _, ok := strings.Cut(rest, "__")
	if !ok || server == "" {
		return "claude.tool.mcp"
	}
	return "claude.tool.mcp." + server
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

	// MCP tools are gated per-server, not globally. Claude Code names MCP
	// tools "mcp__<server>__<tool>" (e.g. mcp__supabase__execute_sql), so
	// the resource_kind becomes "claude.tool.mcp.<server>". This lets an
	// operator grant access to one MCP server in a sandbox without
	// implicitly granting every other server too.
	//
	// When the server segment is missing (malformed mcp__* without a
	// double-underscore separator), fall back to the global
	// "claude.tool.mcp" kind so the call still gets gated rather than
	// slipping through default-allow.
	kind, gated := toolToKind[tool]
	if !gated && strings.HasPrefix(tool, "mcp__") {
		kind = mcpResourceKind(tool)
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

	// W4.7: pull a meaningful resource_id out of the tool input so
	// sandbox grants can refine via resource_pattern. For Bash that's
	// the command; for Read/Edit/Write it's the file_path; for WebFetch
	// it's the URL. When we can't derive one, fall back to spawnID so
	// pattern="*" grants still match (the existing wildcard-allow path).
	resourceID := resourceIDFromInput(tool, input.InputArgs())
	if resourceID == "" {
		resourceID = spawnID
	}

	attrs := map[string]any{
		"tool":    tool,
		"args":    input.InputArgs(),
		"workdir": input.Dir(),
	}
	// CEREBRO-PATCH(main-shell-binary-attr): JEH-1182 — sandbox-policy grants
	// on claude.tool.bash match on the binary name (curl, gh, kubectl, …),
	// not the full command. Extracting once here keeps the policy author
	// free of regex; persona sees attrs.shell_binary as a clean string.
	if tool == "Bash" {
		if cmd, ok := input.InputArgs()["command"].(string); ok && cmd != "" {
			if bin := shellBinaryFromCommand(cmd); bin != "" {
				attrs["shell_binary"] = bin
			}
		}
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

	res, err := client.CheckResource(ctx, actorID, "call-tool", kind, resourceID, attrs)
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
