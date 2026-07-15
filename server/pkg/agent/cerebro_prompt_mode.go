package agent

// System-prompt delivery modes, resolved per provider (FIR-3212).
//
// Every backend already had an opinion about how a system prompt reaches the
// model, but that opinion was hardcoded at the call site and written down
// nowhere. claude.go pinned --append-system-prompt even though the CLI has
// always accepted --system-prompt too, so an agent could never replace a
// runtime's own prompt. Meanwhile opencode.go forwarded a --prompt flag the
// OpenCode CLI has never had, and every run died before the model was reached.
//
// Both bugs share a root cause: the knowledge of what a runtime accepts lived
// in scattered argv-building code instead of in one place that can be tested
// against the installed binary. This file is that place.
//
// The support table below is deliberately conservative and mirrors the contract
// StaticCatalog established in cerebro_model_catalog.go: ok=false means "we have
// no authoritative answer for this provider", never "this provider supports
// nothing". Callers gate a user-visible choice on it, so an uncertain answer
// must not silently remove a capability that actually works.

// SystemPromptMode selects how a system prompt is delivered to a runtime.
type SystemPromptMode string

const (
	// SystemPromptModeDefault leaves the choice to the backend. It preserves
	// whatever the backend did before FIR-3212, so an unset mode is never a
	// behaviour change.
	SystemPromptModeDefault SystemPromptMode = ""
	// SystemPromptModeAppend adds our prompt to the runtime's own system
	// prompt, keeping the runtime's built-in instructions.
	SystemPromptModeAppend SystemPromptMode = "append"
	// SystemPromptModeReplace substitutes the runtime's own system prompt.
	// Only offered where the runtime has a real channel for it.
	SystemPromptModeReplace SystemPromptMode = "replace"
	// SystemPromptModePrepend splices the prompt into the user message. It is
	// the honest label for runtimes with no system-prompt channel at all —
	// the text arrives, but it carries no system-prompt semantics.
	SystemPromptModePrepend SystemPromptMode = "prepend"
)

// SystemPromptSupport describes how one provider can accept a system prompt.
type SystemPromptSupport struct {
	// Native reports whether the provider has a real system-prompt channel
	// (a CLI flag or an API field) as opposed to text spliced into the user
	// message. The UI must not present a prepend-only runtime as if it
	// honoured system-prompt semantics — that claim is the thing FIR-3212
	// exists to stop.
	Native bool
	// Modes lists every mode this provider actually accepts. Empty means the
	// provider ignores a system prompt entirely.
	Modes []SystemPromptMode
}

// Supports reports whether mode is accepted. The default mode is accepted by any
// provider that accepts anything, because it just means "do what you already do".
func (s SystemPromptSupport) Supports(mode SystemPromptMode) bool {
	if len(s.Modes) == 0 {
		return false
	}
	if mode == SystemPromptModeDefault {
		return true
	}
	for _, m := range s.Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// systemPromptSupport is the per-provider table. Every entry is traced to the
// line that consumes (or discards) ExecOptions.SystemPrompt, so a backend change
// that contradicts this table shows up as a failing test rather than a silently
// dropped instruction.
var systemPromptSupport = map[string]SystemPromptSupport{
	// claude.go:642 --append-system-prompt; the installed CLI also accepts
	// --system-prompt (pinned by TestInstalledClaudeAcceptsBothSystemPromptFlags).
	"claude": {Native: true, Modes: []SystemPromptMode{SystemPromptModeAppend, SystemPromptModeReplace}},
	// pi.go:521 --append-system-prompt. No replace flag observed.
	"pi": {Native: true, Modes: []SystemPromptMode{SystemPromptModeAppend}},
	// codex.go:914,942 "developerInstructions" — a dedicated API field, which
	// sets the instructions rather than appending to them.
	"codex": {Native: true, Modes: []SystemPromptMode{SystemPromptModeReplace}},
	// openai_eu.go:131 system role message (o-series falls back to prepend at :136).
	openaiEUProvider: {Native: true, Modes: []SystemPromptMode{SystemPromptModeReplace}},
	// firtal_gateway.go:154-164 system role, after a fixed Firtal preamble.
	"firtal-gateway": {Native: true, Modes: []SystemPromptMode{SystemPromptModeAppend}},
	// firtal_local.go:917-920 system role, appended after a fixed base prompt.
	firtalLocalProvider: {Native: true, Modes: []SystemPromptMode{SystemPromptModeAppend}},

	// No system-prompt channel: the brief is spliced into the user message.
	// opencode.go:74, kiro.go:272, kimi.go:289, openclaw.go:197.
	"opencode": {Native: false, Modes: []SystemPromptMode{SystemPromptModePrepend}},
	"kiro":     {Native: false, Modes: []SystemPromptMode{SystemPromptModePrepend}},
	"kimi":     {Native: false, Modes: []SystemPromptMode{SystemPromptModePrepend}},
	"openclaw": {Native: false, Modes: []SystemPromptMode{SystemPromptModePrepend}},

	// Ignore a system prompt entirely. hermes.go:72 discards it on purpose and
	// relies on cwd-scoped AGENTS.md; the rest have no reference to the field.
	"hermes":      {Native: false, Modes: nil},
	"copilot":     {Native: false, Modes: nil},
	"gemini":      {Native: false, Modes: nil},
	"cursor":      {Native: false, Modes: nil},
	"antigravity": {Native: false, Modes: nil},
}

// SystemPromptSupportFor returns how providerType accepts a system prompt.
// ok=false means we have no authoritative entry — the caller must treat that as
// "unknown", not as "unsupported".
func SystemPromptSupportFor(providerType string) (support SystemPromptSupport, ok bool) {
	s, ok := systemPromptSupport[providerType]
	return s, ok
}

// ClaudeSystemPromptArgs builds the argv pair carrying prompt to Claude Code in
// the requested mode. An empty prompt yields no args so a caller need not guard.
func ClaudeSystemPromptArgs(mode SystemPromptMode, prompt string) []string {
	if prompt == "" {
		return nil
	}
	flag := "--append-system-prompt"
	if mode == SystemPromptModeReplace {
		flag = "--system-prompt"
	}
	return []string{flag, prompt}
}
