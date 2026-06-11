// Package claudehook holds the pure, IO-free helpers a Claude Code PreToolUse
// hook needs: parsing the stdin payload Claude sends per tool call, deciding
// whether a tool is gated (vs default-allow read/planning tools that never need
// a permission round-trip), and extracting the operator-meaningful resource
// pattern (shell binary, file path, URL, …) a policy chain can refine on.
//
// It is the shared half of the local-runtime per-tool enforcement seam
// (TECH-2563): the `multica cerebro-tool-policy-hook` subcommand wires these
// helpers to the daemon-local resolve IPC, which proxies the SAME tool-policy
// chain the gateway gate uses (server/internal/cerebro/localtoolpolicy +
// handler.ResolveDaemonToolPolicy). Keeping the Claude-hook parsing pure and in
// one place means the tool→resource mapping cannot drift between callers.
package claudehook

import (
	"encoding/json"
	"io"
	"strings"
)

// maxInputBytes caps the stdin payload Claude Code sends so a malformed or
// hostile hook input cannot exhaust memory. 1 MiB is far above any real
// tool_input.
const maxInputBytes = 1 << 20

// Input is the JSON payload Claude Code sends a PreToolUse hook on stdin. The
// canonical fields are tool_name / tool_input / cwd; the short aliases let the
// hook be smoke-tested with a simpler hand-written payload.
type Input struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	CWD       string         `json:"cwd"`
	// Aliases for direct-invocation testing.
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Workdir string         `json:"workdir"`
}

// Parse reads and decodes a Claude PreToolUse payload from r, enforcing the
// size cap. A decode error is returned so the caller can decide its fail
// direction (the tool-policy hook treats an unparseable payload as "no tool to
// gate" and allows, since a malformed hook input is a CLI/version mismatch, not
// a policy decision).
func Parse(r io.Reader) (Input, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxInputBytes))
	if err != nil {
		return Input{}, err
	}
	var in Input
	if err := json.Unmarshal(raw, &in); err != nil {
		return Input{}, err
	}
	return in, nil
}

// Name returns the tool name, preferring the canonical field over the test alias.
func (in Input) Name() string {
	if in.ToolName != "" {
		return in.ToolName
	}
	return in.Tool
}

// InputArgs returns the tool input map, preferring the canonical field.
func (in Input) InputArgs() map[string]any {
	if len(in.ToolInput) > 0 {
		return in.ToolInput
	}
	return in.Args
}

// Dir returns the working directory, preferring the canonical field.
func (in Input) Dir() string {
	if in.CWD != "" {
		return in.CWD
	}
	return in.Workdir
}

// gatedTools is the set of Claude Code built-in tools that carry a real side
// effect (execute, write, fetch, delegate) and are therefore resolved against
// the tool-policy chain. Everything NOT in this set — Read, Grep, Glob,
// TodoWrite, ToolSearch, Skill, ScheduleWakeup, Monitor, EnterPlanMode,
// ExitPlanMode, AskUserQuestion — is default-allow: the hook short-circuits to
// allow without any permission round-trip, exactly as the persona hook does.
// Keeping the gated set to the impactful tools bounds the per-tool resolve
// traffic to the calls that can actually change or reach outside the workdir.
//
// MCP tools (mcp__<server>__<action>) are always gated via the prefix check in
// Gated, so a per-connection Deny/Ask reaches local runtimes too.
var gatedTools = map[string]bool{
	"Bash":         true,
	"Write":        true,
	"Edit":         true,
	"NotebookEdit": true,
	"WebFetch":     true,
	"WebSearch":    true,
	"Task":         true,
}

// Gated reports whether a tool call must be resolved against the policy chain.
// A non-gated tool is default-allow and never triggers a resolve round-trip.
func Gated(tool string) bool {
	if gatedTools[tool] {
		return true
	}
	return strings.HasPrefix(tool, "mcp__")
}

// ResourcePattern pulls the operator-meaningful identifier out of a Claude
// tool_input object so the tool-policy chain can refine a grant via
// resource_pattern. The mapping is per-tool because Claude's tool_input shape
// differs:
//
//	Bash         → the head shell binary (curl, gh, kubectl, …), not the raw command
//	Edit/Write   → file_path
//	NotebookEdit → notebook_path
//	WebFetch     → url
//	WebSearch    → query
//	Task         → description / subagent_type
//	mcp__*       → first non-empty string arg
//
// Returns "" when no meaningful field is present; the caller then leaves the
// pattern empty so a wildcard ("*") policy row still matches — preserving the
// allow-all behaviour when no refined pattern was authored.
//
// For Bash the head binary (not the whole command) is returned because the
// policy matcher uses path.Match semantics, where "*" does not cross "/", so a
// "curl*" pattern would never match a command that contains a URL. One row per
// binary ("curl", "kubectl") is what a policy author actually writes.
func ResourcePattern(tool string, args map[string]any) string {
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
		if cmd := pick("command"); cmd != "" {
			if bin := ShellBinary(cmd); bin != "" {
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
		for _, v := range args {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	return ""
}

// ShellBinary returns the binary a Bash command would execute, or "" when it
// can't be confidently determined. Used so a policy on "Bash" can match the
// allowlist/denylist on the binary name independent of the full command string.
//
// Parsing rules (deliberately simple — this is policy gating, not a full shell
// parser):
//   - Skip leading env-var assignments (FOO=bar baz → baz)
//   - Strip the path prefix from the head (/usr/bin/curl → curl, ./foo → foo)
//   - Unwrap one level of `sh -c <inner>` / `bash -c <inner>` so the policy sees
//     the real binary, not the shell launcher
//   - Return "" for empty input
//
// Out of scope (a policy author who wants to block these denies the shell
// binaries outright): pipelines, command chaining, subshells, command
// substitution, multi-statement `sh -c`.
func ShellBinary(cmd string) string {
	tokens := splitShellWords(strings.TrimSpace(cmd))
	i := 0
	for i < len(tokens) && isEnvAssignment(tokens[i]) {
		i++
	}
	if i >= len(tokens) {
		return ""
	}
	head := baseName(tokens[i])
	if isShellLauncher(head) && i+2 < len(tokens) && tokens[i+1] == "-c" {
		return ShellBinary(tokens[i+2])
	}
	return head
}

// splitShellWords splits a command into whitespace-separated tokens, treating
// matched single- or double-quoted runs as one token (stripping the quotes).
// Not a full shell parser — backslash escapes, here-docs, and command
// substitution are not handled — but enough to identify the head binary of most
// operator-written Bash commands.
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

// isEnvAssignment reports whether s is a leading NAME=VALUE shell env-var
// assignment (which precedes the actual command). NAME must match
// [A-Za-z_][A-Za-z0-9_]*; VALUE may be anything including empty.
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
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// isShellLauncher reports whether name is a common POSIX shell binary that
// policy authors expect us to unwrap (so `sh -c "curl ..."` gates on "curl").
func isShellLauncher(name string) bool {
	switch name {
	case "sh", "bash", "dash", "zsh":
		return true
	}
	return false
}
