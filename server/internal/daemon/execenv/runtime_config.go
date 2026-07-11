package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// runtimeMarkerBegin and runtimeMarkerEnd delimit the Multica-managed brief
// inside the runtime config file (CLAUDE.md / AGENTS.md). The
// markers exist so writeRuntimeConfigFile can:
//
//   - preserve user-authored content in the same file (the user's repo may
//     already ship a CLAUDE.md / AGENTS.md when the agent is pointed at a
//     local_directory project resource),
//   - replace the brief idempotently on subsequent runs in the same workdir
//     instead of appending duplicate copies, and
//   - leave a precise excision target for a future cleanup pass.
//
// HTML comments are used so the markers are inert in every Markdown renderer
// and harmless when fed to the agent as instructions. Changing the marker
// text is a breaking change for any file that already carries the previous
// markers — bump deliberately.
const (
	runtimeMarkerBegin = "<!-- BEGIN MULTICA-RUNTIME (auto-managed; do not edit) -->"
	runtimeMarkerEnd   = "<!-- END MULTICA-RUNTIME -->"

	// runtimeManagedSeparator is the fixed separator inserted between any
	// pre-existing user content and the marker block whenever Inject
	// appends to a file that already exists. The separator is considered
	// part of the managed region: Cleanup strips it together with the
	// block, so the file rolls back to its exact pre-injection bytes
	// regardless of whether the user file ended with no newline, one
	// newline, or multiple trailing newlines. Without a fixed-width
	// separator the cleanup path would have to renormalise the user's
	// trailing bytes and would leave a subtle but real diff every run
	// (see MUL-2753 review on PR #3438).
	//
	// Cleanup distinguishes "file we created" (no managed separator
	// precedes the block — write a missing file from scratch) from "file
	// that pre-existed" (managed separator precedes the block) so the
	// file's existence is preserved exactly across the inject→cleanup
	// cycle, including empty / whitespace-only pre-existing files.
	runtimeManagedSeparator = "\n\n"
)

// runtimeGOOS is the host-platform string used by buildMetaSkillContent and
// BuildCommentReplyInstructions to emit Windows-specific guidance. Defaults
// to runtime.GOOS; tests override it to exercise the cross-platform branches
// deterministically without having to run on every target OS.
var runtimeGOOS = runtime.GOOS

// sanitizeNameForBriefMarkdown turns a possibly-multiline display name into a
// single-line, plain-text token that is safe to embed inside markdown inline
// constructs (e.g. `**%s**`) in the agent brief. The brief is loaded as
// trusted instructions, so user-controlled name fields must not be able to
// introduce headings, lists, or close the surrounding bold span.
//
// CR/LF and other whitespace control bytes collapse to a single space; other
// C0 controls and DEL are dropped; markdown structural characters that have
// meaning in inline context (`*`, `_`, “ ` “, `\`, `[`, `]`, `<`) are
// backslash-escaped. Trailing whitespace is trimmed.
func sanitizeNameForBriefMarkdown(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	prevSpace := false
	for _, r := range name {
		switch {
		case r == '\r' || r == '\n' || r == '\t' || r == '\v' || r == '\f':
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
		case r < 0x20 || r == 0x7f:
			continue
		case r == '*' || r == '_' || r == '`' || r == '\\' || r == '[' || r == ']' || r == '<':
			b.WriteByte('\\')
			b.WriteRune(r)
			prevSpace = false
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

// sanitizeEmailForBrief returns the email verbatim when it is safe to embed
// inline in the brief, or "" when it carries a character a real address never
// has (whitespace, control chars, or a markdown-break risk). Unlike
// sanitizeNameForBriefMarkdown it does NOT backslash-escape markdown specials:
// an agent may want to match the initiator's address exactly, and escaping
// `_`/`+` would corrupt it, while a valid email can't contain a newline to
// inject a heading anyway. Emails are validated at signup, so this is
// defense-in-depth, not the primary guard. See MUL-2645.
func sanitizeEmailForBrief(email string) string {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return ""
	}
	for _, r := range email {
		if r < 0x20 || r == 0x7f || r == ' ' || r == '\\' || r == '`' || r == '*' || r == '<' || r == '>' || r == '[' || r == ']' {
			return ""
		}
	}
	return email
}

// formatProjectResource renders a single resource as a human-readable bullet.
// Unknown resource types fall back to a JSON-encoded ref so the agent can
// still read what the user attached. New resource types should add a case
// here AND in the API validator (handler/project_resource.go).
func formatProjectResource(r ProjectResourceForEnv) string {
	label := r.Label
	switch r.ResourceType {
	case "github_repo":
		var payload struct {
			URL               string `json:"url"`
			DefaultBranchHint string `json:"default_branch_hint,omitempty"`
			Ref               string `json:"ref,omitempty"`
		}
		_ = json.Unmarshal(r.ResourceRef, &payload)
		out := fmt.Sprintf("**GitHub repo**: %s", payload.URL)
		details := make([]string, 0, 2)
		if payload.Ref != "" {
			details = append(details, fmt.Sprintf("checkout ref: `%s`", payload.Ref))
		}
		if payload.DefaultBranchHint != "" {
			details = append(details, fmt.Sprintf("default branch hint: `%s`", payload.DefaultBranchHint))
		}
		if len(details) > 0 {
			out += " (" + strings.Join(details, ", ") + ")"
		}
		if label != "" {
			out += " — " + label
		}
		return out
	default:
		ref := string(r.ResourceRef)
		if ref == "" {
			ref = "{}"
		}
		out := fmt.Sprintf("**%s**: `%s`", r.ResourceType, ref)
		if label != "" {
			out += " — " + label
		}
		return out
	}
}

// InjectRuntimeConfig writes the meta skill content into the runtime-specific
// config file so the agent discovers its environment through its native mechanism.
//
// For Claude:   writes {workDir}/CLAUDE.md  (skills discovered natively from .claude/skills/)
// For Codex:    writes {workDir}/AGENTS.md  (skills discovered natively via CODEX_HOME)
// For Copilot:  writes {workDir}/AGENTS.md  (skills discovered natively from .github/skills/)
// For OpenCode: writes {workDir}/AGENTS.md  (skills discovered natively from .opencode/skills/)
// For DevEco Code: writes {workDir}/AGENTS.md  (skills discovered natively from .deveco/skills/)
// For OpenClaw: writes {workDir}/AGENTS.md  (skills discovered natively from {workDir}/skills/ via per-task openclaw-config.json that pins agents.defaults.workspace)
// For Hermes:   writes {workDir}/AGENTS.md  (skills discovered natively from a per-task HERMES_HOME/skills seeded by the daemon; see hermes_home.go)
// For Pi:       writes {workDir}/AGENTS.md  (skills discovered natively from .pi/skills/)
// For Cursor:   writes {workDir}/AGENTS.md  (skills discovered natively from .cursor/skills/)
// For Kimi:        writes {workDir}/AGENTS.md  (Kimi Code CLI reads AGENTS.md natively; skills auto-discovered from project skills dirs)
// For Kiro:        writes {workDir}/AGENTS.md  (Kiro CLI reads AGENTS.md natively; skills auto-discovered from project skills dirs)
// For Qoder:       writes {workDir}/AGENTS.md  (skills discovered from .qoder/skills/, user-level ~/.qoder/skills is unaffected)
// For Antigravity: writes {workDir}/AGENTS.md  (agy CLI reads AGENTS.md natively; skills discovered natively from .agents/skills/ — see https://antigravity.google/docs/gcli-migration)
// For Traecli:     writes {workDir}/AGENTS.md  (traecli reads .trae/rules/ not AGENTS.md, so the brief is delivered inline via providerNeedsInlineSystemPrompt; the file is written for parity/visibility only)
// For Grok:        writes {workDir}/AGENTS.md  (Grok Build CLI reads AGENTS.md natively from the workdir)
func InjectRuntimeConfig(workDir, provider string, ctx TaskContextForEnv) (string, error) {
	content := buildMetaSkillContent(provider, ctx)
	path := runtimeConfigPath(workDir, provider)
	if path == "" {
		// Unknown provider — skip config injection, prompt-only mode.
		return content, nil
	}
	return content, writeRuntimeConfigFile(path, content)
}

// runtimeConfigPath returns the absolute path to the runtime config file that
// InjectRuntimeConfig writes for the given provider, or "" when the provider
// has no file-based config target. Centralising the mapping keeps Inject /
// Cleanup in lockstep — both paths consult the same table so a new provider
// added to one side cannot drift past the other.
func runtimeConfigPath(workDir, provider string) string {
	switch provider {
	case "claude", "codebuddy":
		return filepath.Join(workDir, "CLAUDE.md")
	case "codex", "copilot", "opencode", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "traecli", "grok":
		return filepath.Join(workDir, "AGENTS.md")
	default:
		return ""
	}
}

// writeRuntimeConfigFile writes the Multica runtime brief to path without
// clobbering any user-authored content already present. Behaviour by file
// state:
//
//   - file missing → create the file containing only the marker block, no
//     leading separator. Cleanup detects the absence of the separator and
//     restores the missing-file state by removing the file outright.
//   - file present (any content, including empty), no marker block →
//     append `<runtimeManagedSeparator>` + the marker block. The
//     separator's bytes are part of the managed region so Cleanup can
//     restore the user's pre-injection bytes exactly (no trailing-newline
//     normalisation, no surprises for files that ended without a newline
//     or with extra trailing newlines).
//   - file present, marker block already there → replace the body between
//     the markers in place so repeated runs in the same workdir don't grow
//     the file unboundedly. The pre-block content (including any managed
//     separator established by the first inject) is preserved verbatim.
//
// The previous implementation called os.WriteFile unconditionally, which
// silently truncated a repository's CLAUDE.md / AGENTS.md the
// first time the agent was pointed at the user's own directory via the
// local_directory project resource flow. See MUL-2753.
func writeRuntimeConfigFile(path, brief string) error {
	block := runtimeMarkerBegin + "\n" + strings.TrimRight(brief, "\n") + "\n" + runtimeMarkerEnd + "\n"

	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(path, []byte(block), 0o644)
	}
	if err != nil {
		return fmt.Errorf("read existing runtime config %s: %w", path, err)
	}

	existingStr := string(existing)
	if start, end, ok := locateMarkerBlock(existingStr); ok {
		// Replace the existing block in place. locateMarkerBlock already
		// consumes the trailing newline that closed the previous block, so
		// successive runs don't accumulate blank lines around the block.
		// The managed separator (if any) lives in existingStr[:start] and
		// is preserved untouched.
		newContent := existingStr[:start] + block + existingStr[end:]
		return os.WriteFile(path, []byte(newContent), 0o644)
	}

	// No marker block present. Append the fixed managed separator followed
	// by the block. The separator is unconditional — including for files
	// that already end in two or more newlines — so the byte boundary
	// between user content and the managed region is deterministic, which
	// is what lets Cleanup roll back to the user's exact original bytes.
	return os.WriteFile(path, []byte(existingStr+runtimeManagedSeparator+block), 0o644)
}

// locateMarkerBlock finds the [start, end) byte range of the Multica marker
// block inside content. The returned `end` is one past the block's trailing
// newline (if any) so callers can splice the block out without leaving an
// orphan blank line behind.
//
// The end marker is searched for strictly after the begin marker. This
// matters for two malformed cases that the previous naive `strings.Index`
// pair would mishandle:
//
//   - User content carries a stray `<!-- END MULTICA-RUNTIME -->` (e.g. a
//     documentation snippet showing what the wire format looks like) before
//     any begin marker. The naive parser would find that end and reject the
//     block (`endIdx > startIdx` false), then append a fresh block — and
//     since the stray end stays in place, every subsequent run would append
//     yet another block, growing the file unboundedly.
//   - A previous run crashed between writing begin and end and left the file
//     with a half-block. The naive parser would not find an end, fall
//     through to the append branch, and stack a new block after the
//     half-block. Treating "begin found, no end after" as "the block ends
//     at EOF" makes the next write replace the half-block in place.
func locateMarkerBlock(content string) (start, end int, found bool) {
	start = strings.Index(content, runtimeMarkerBegin)
	if start < 0 {
		return 0, 0, false
	}
	afterBegin := start + len(runtimeMarkerBegin)
	endRel := strings.Index(content[afterBegin:], runtimeMarkerEnd)
	if endRel < 0 {
		// Malformed — no end marker after begin. Treat the rest of the file
		// as the block so the next write replaces it cleanly instead of
		// stacking another block beneath the half-block.
		return start, len(content), true
	}
	end = afterBegin + endRel + len(runtimeMarkerEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return start, end, true
}

// CleanupRuntimeConfig excises the Multica marker block from the runtime
// config file for the given provider and restores the file to its exact
// pre-injection state, byte for byte. The cleanup is the second half of
// the contract `writeRuntimeConfigFile` establishes: together they must
// round-trip a user's local repository config across an arbitrary number
// of Multica runs without ever touching a single non-managed byte.
//
// Behaviour, mirroring the three Inject states:
//
//   - file has no marker block → no-op (nothing was ever injected here);
//   - block is at the start of the file with no preceding managed
//     separator → the file was created by Inject from a missing-file
//     state. Remove the file outright so the post-cleanup directory
//     listing is byte-identical to the pre-Inject one.
//   - block is preceded by the fixed managed separator → strip the
//     separator together with the block; whatever remains (which may be
//     an empty pre-existing file, a whitespace-only file, or arbitrary
//     user content) is the user's original file, written back verbatim
//     with NO trailing-newline normalisation and NO TrimSpace-based file
//     removal heuristic. Both of those were sources of subtle diff in
//     PR #3438 review feedback.
//
// Required for the local_directory flow (WorkDir is the user's own repo):
// without this pass, a manual `claude` / `codex` run started by
// the user inside the same directory after a Multica task would pick up
// the stale brief and act on the previous task's issue id, trigger
// comment id, and reply rules. Cloud workspace runs never trigger this
// pollution because their workdir is daemon scratch that the GC loop
// deletes wholesale; the daemon skips this Cleanup on those workdirs.
//
// Missing files, unknown providers, and files without a marker block are
// no-ops — Cleanup is safe to call defensively.
func CleanupRuntimeConfig(workDir, provider string) error {
	path := runtimeConfigPath(workDir, provider)
	if path == "" {
		return nil
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read runtime config %s: %w", path, err)
	}
	existingStr := string(existing)
	start, end, ok := locateMarkerBlock(existingStr)
	if !ok {
		return nil
	}
	pre := existingStr[:start]
	post := existingStr[end:]

	// Detect — and strip — the fixed managed separator that Inject puts
	// immediately before the block whenever it appended to a file that
	// pre-existed. The absence of the separator is the marker that says
	// "Inject created this file from scratch", which is the only case
	// where Cleanup is allowed to delete the file.
	hadManagedSeparator := strings.HasSuffix(pre, runtimeManagedSeparator)
	if hadManagedSeparator {
		pre = pre[:len(pre)-len(runtimeManagedSeparator)]
	}
	remainder := pre + post

	if !hadManagedSeparator && remainder == "" {
		// Inject created the file (no managed separator → block was the
		// only content). Restore the missing-file state.
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove runtime config %s: %w", path, err)
		}
		return nil
	}
	// File pre-existed (possibly empty, possibly whitespace-only,
	// possibly with user content) — write the remainder back exactly,
	// without any normalisation. An empty `remainder` here means the
	// user's original file was empty; we still write it (zero-byte file)
	// so the file's existence is preserved.
	return os.WriteFile(path, []byte(remainder), 0o644)
}

// buildMetaSkillContent generates the meta skill markdown that teaches the
// agent about the Multica runtime environment and available CLI tools.
//
// The brief is assembled by buildMetaSkillContentSlim (runtime_config_sections.go),
// which applies kind-driven section gating + per-section prose compression.
// This used to be gated behind the `runtime_brief_slim` feature flag against a
// legacy verbose brief; the flag has been retired (MUL-4297) and the slim brief
// is now the only path.
func buildMetaSkillContent(provider string, ctx TaskContextForEnv) string {
	return buildMetaSkillContentSlim(provider, ctx)
}
