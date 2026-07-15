package agent

import "sort"

// Per-provider ExecOptions support matrix (FIR-3212, slice 2).
//
// capabilities.Set already answers "what tools does this runtime expose?". It
// does not answer the question that actually breaks runs: "if I set this
// ExecOptions field, will this provider do anything with it?" That knowledge
// existed only as prose in field comments and as scattered `if opts.X != ""`
// branches, so it could not be queried, tested, or shown to an operator.
//
// Two failures make the case for writing it down:
//
//   - opencode.go forwarded a --prompt flag the OpenCode CLI has never had.
//     Every run died. A table nobody checks against the binary cannot catch it;
//     the contract tests in cerebro_exec_options_matrix_test.go check this
//     table against the installed CLI, which is why it is worth having.
//   - DisallowedMCPTools is sent to all 15 backends but honoured by 3. The other
//     12 drop a deny-policy on the floor without a line in the log. That is not
//     a missing feature, it is a protection an operator believes they have.
//     HandlingIgnoredSilent exists to name it.
//
// Sourcing rule: every entry below is traced to the line that consumes — or
// provably does not consume — the field. This table is not allowed to be an
// opinion. If it disagrees with a backend, the test fails; if it disagrees with
// an installed CLI, the contract test fails.
//
// It mirrors the StaticCatalog contract in cerebro_model_catalog.go: ok=false
// means "we have no authoritative answer", never "supports nothing". Absence of
// proof must not be served as proof of absence.

// ExecOptionField names an ExecOptions field whose support varies per provider.
// Fields every backend treats identically (Cwd, Timeout) are deliberately absent
// — a matrix entry that is always the same answer teaches nobody anything.
type ExecOptionField string

const (
	// FieldModel selects the model. ExecOptions.Model.
	FieldModel ExecOptionField = "model"
	// FieldSystemPrompt delivers the system prompt. Modes live in
	// cerebro_prompt_mode.go; this entry records only whether the field lands.
	FieldSystemPrompt ExecOptionField = "system_prompt"
	// FieldMaxTurns caps agent turns. ExecOptions.MaxTurns.
	FieldMaxTurns ExecOptionField = "max_turns"
	// FieldThinkingLevel sets runtime-native reasoning effort.
	FieldThinkingLevel ExecOptionField = "thinking_level"
	// FieldResumeSession resumes a prior session. ExecOptions.ResumeSessionID.
	FieldResumeSession ExecOptionField = "resume_session"
	// FieldMCPConfig injects MCP servers. ExecOptions.McpConfig.
	FieldMCPConfig ExecOptionField = "mcp_config"
	// FieldDisallowedTools carries the workspace tool deny-policy.
	FieldDisallowedTools ExecOptionField = "disallowed_tools"
	// FieldExtraArgs is the daemon-wide argv default. ExecOptions.ExtraArgs.
	FieldExtraArgs ExecOptionField = "extra_args"
	// FieldCustomArgs is the per-agent argv addition. ExecOptions.CustomArgs.
	FieldCustomArgs ExecOptionField = "custom_args"
)

// ExecOptionFields lists every field in the matrix, in a stable order so a
// rendered matrix and a test fixture never churn.
func ExecOptionFields() []ExecOptionField {
	return []ExecOptionField{
		FieldModel,
		FieldSystemPrompt,
		FieldMaxTurns,
		FieldThinkingLevel,
		FieldResumeSession,
		FieldMCPConfig,
		FieldDisallowedTools,
		FieldExtraArgs,
		FieldCustomArgs,
	}
}

// FieldHandling is what a provider actually does with a field that is set.
//
// The split between the two ignore values is the point of this type. Both mean
// "the value has no effect", but only one of them tells the operator. A silent
// ignore of a cosmetic field is a shrug; a silent ignore of a deny-policy is a
// security hole. Collapsing them into a single "unsupported" would hide exactly
// the distinction FIR-3212 exists to surface.
type FieldHandling string

const (
	// HandlingHonoured means the value reaches the model or the CLI.
	HandlingHonoured FieldHandling = "honoured"
	// HandlingIgnoredLogged means the backend drops the value and says so.
	// This is the correct behaviour for an unsupported field — opencode.go:78
	// is the reference implementation.
	HandlingIgnoredLogged FieldHandling = "ignored_logged"
	// HandlingIgnoredSilent means the backend drops the value with no trace.
	// The operator has no way to discover it. Every entry carrying this value
	// is a latent bug report, not a settled design.
	HandlingIgnoredSilent FieldHandling = "ignored_silent"
)

// Effective reports whether the value changes the run.
func (h FieldHandling) Effective() bool { return h == HandlingHonoured }

// execOptionsSupport is the per-provider matrix. A field absent from a
// provider's map is HandlingIgnoredSilent — see ExecOptionsHandling. Absence is
// spelled out per provider rather than left implicit precisely because the
// silent-ignore cases are the interesting ones.
//
// argv backends (12) build a command line; API backends (firtal-gateway,
// openai-eu, firtal-local) build a request body and have no argv at all, which
// is why every argv-shaped field is silently ignored there. That one is benign:
// there is no flag to pass.
var execOptionsSupport = map[string]map[ExecOptionField]FieldHandling{
	// claude.go — the only backend that honours every field in the matrix.
	"claude": {
		FieldModel:           HandlingHonoured, // claude.go:600 --model
		FieldSystemPrompt:    HandlingHonoured, // claude.go:643 via ClaudeSystemPromptArgs
		FieldMaxTurns:        HandlingHonoured, // claude.go:640 --max-turns
		FieldThinkingLevel:   HandlingHonoured,
		FieldResumeSession:   HandlingHonoured,
		FieldMCPConfig:       HandlingHonoured,
		FieldDisallowedTools: HandlingHonoured, // claude.go:627 --disallowedTools
		FieldExtraArgs:       HandlingHonoured,
		FieldCustomArgs:      HandlingHonoured,
	},

	// codex.go — app-server protocol; deny-policy has no field in it.
	"codex": {
		FieldModel:         HandlingHonoured,
		FieldSystemPrompt:  HandlingHonoured, // codex.go:914 developerInstructions
		FieldThinkingLevel: HandlingHonoured, // codex.go:717 applyCodexReasoningEffort
		FieldResumeSession: HandlingHonoured,
		FieldMCPConfig:     HandlingHonoured,
		FieldExtraArgs:     HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},

	// opencode.go — the only backend that logs what it drops (:78). The brief is
	// prepended to the user message; see cerebro_prompt_mode.go.
	"opencode": {
		FieldModel:         HandlingHonoured,      // opencode.go:66 --model
		FieldSystemPrompt:  HandlingHonoured,      // opencode.go:75 prepend
		FieldMaxTurns:      HandlingIgnoredLogged, // opencode.go:78 warns
		FieldThinkingLevel: HandlingHonoured,      // opencode.go:69 --variant
		FieldResumeSession: HandlingHonoured,      // opencode.go:81 --session
		FieldMCPConfig:     HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},

	// cursor.go — deny-policy lands via the managed project MCP config, not argv
	// (managed_mcp_project_config.go:284).
	"cursor": {
		FieldModel:           HandlingHonoured,
		FieldResumeSession:   HandlingHonoured,
		FieldMCPConfig:       HandlingHonoured, // cursor.go:39
		FieldDisallowedTools: HandlingHonoured, // cursor.go:39
		FieldCustomArgs:      HandlingHonoured,
	},

	// gemini.go — same managed-config path as cursor. Note --allowed-tools is
	// deprecated in the installed CLI (0.44.1) in favour of a Policy Engine; an
	// allowlist dimension must not be added here without re-reading --help.
	"gemini": {
		FieldModel:           HandlingHonoured,
		FieldResumeSession:   HandlingHonoured,
		FieldMCPConfig:       HandlingHonoured, // gemini.go:36
		FieldDisallowedTools: HandlingHonoured, // gemini.go:36
		FieldCustomArgs:      HandlingHonoured,
	},

	// hermes.go:72 discards SystemPrompt on purpose and relies on cwd-scoped
	// AGENTS.md. The daemon sets it anyway (daemon.go:3295) — wasted work plus a
	// misleading inline_system_prompt=true in the log. Tracked separately.
	"hermes": {
		FieldModel:         HandlingHonoured,
		FieldSystemPrompt:  HandlingIgnoredLogged, // hermes.go:72 debug-logs the discard
		FieldResumeSession: HandlingHonoured,
		FieldMCPConfig:     HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},

	// kiro.go / kimi.go / openclaw.go — prepend backends.
	"kiro": {
		FieldModel:         HandlingHonoured,
		FieldSystemPrompt:  HandlingHonoured, // kiro.go:272 prepend
		FieldResumeSession: HandlingHonoured,
		FieldMCPConfig:     HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},
	"kimi": {
		FieldModel:         HandlingHonoured,
		FieldSystemPrompt:  HandlingHonoured, // kimi.go:289 prepend
		FieldResumeSession: HandlingHonoured,
		FieldMCPConfig:     HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},
	"openclaw": {
		FieldModel:         HandlingHonoured,
		FieldSystemPrompt:  HandlingHonoured, // openclaw.go:197 prepend
		FieldResumeSession: HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},

	// pi.go:521 --append-system-prompt.
	"pi": {
		FieldModel:         HandlingHonoured,
		FieldSystemPrompt:  HandlingHonoured,
		FieldResumeSession: HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},

	// copilot.go — runs --allow-all (:461) and blocks the operator from
	// overriding it via blockedArgs, so a deny-policy is doubly ineffective here.
	"copilot": {
		FieldModel:         HandlingHonoured,
		FieldResumeSession: HandlingHonoured,
		FieldCustomArgs:    HandlingHonoured,
	},

	// antigravity.go:263 reads ExtraArgs even though the ExecOptions.ExtraArgs
	// doc comment (agent.go:39) still claims claude and codex only. The code is
	// the truth; the comment is stale.
	"antigravity": {
		FieldModel:         HandlingHonoured,
		FieldResumeSession: HandlingHonoured,
		FieldExtraArgs:     HandlingHonoured, // antigravity.go:263
		FieldCustomArgs:    HandlingHonoured, // antigravity.go:264
	},

	// API backends: a request body, no argv, no CLI session to resume. Every
	// argv-shaped field is silently ignored because there is nothing to ignore it
	// with — the only benign silent-ignore in this table.
	"firtal-gateway": {
		FieldModel:        HandlingHonoured,
		FieldSystemPrompt: HandlingHonoured, // firtal_gateway.go:154 system role
	},
	openaiEUProvider: {
		FieldModel:        HandlingHonoured,
		FieldSystemPrompt: HandlingHonoured, // openai_eu.go:131 system role
	},
	firtalLocalProvider: {
		FieldModel:        HandlingHonoured,
		FieldSystemPrompt: HandlingHonoured, // firtal_local.go:917 system role
	},
}

// ExecOptionsSupportFor returns the field→handling map for providerType.
//
// ok=false means providerType is not in the matrix — "unknown", not
// "unsupported". A caller must not disable a control on the strength of a
// missing entry; the drift test exists so this stays a theoretical case.
func ExecOptionsSupportFor(providerType string) (support map[ExecOptionField]FieldHandling, ok bool) {
	src, ok := execOptionsSupport[providerType]
	if !ok {
		return nil, false
	}
	out := make(map[ExecOptionField]FieldHandling, len(src))
	for field, handling := range src {
		out[field] = handling
	}
	return out, true
}

// ExecOptionsHandling reports what providerType does with field.
//
// A provider we know, with no entry for field, ignores it silently — that is the
// default precisely because it is the dangerous case, and defaulting to it means
// a newly added field shows up as "nobody handles this" rather than quietly
// inheriting a claim of support.
//
// ok=false means the provider itself is unknown, so the answer is unknown too.
// Note the asymmetry with the returned handling: callers must branch on ok
// before reading handling, or they will read a silent-ignore they were never told.
func ExecOptionsHandling(providerType string, field ExecOptionField) (handling FieldHandling, ok bool) {
	support, ok := execOptionsSupport[providerType]
	if !ok {
		return HandlingIgnoredSilent, false
	}
	h, present := support[field]
	if !present {
		return HandlingIgnoredSilent, true
	}
	return h, true
}

// ExecOptionsHonours reports whether providerType acts on field.
//
// Like StaticCatalogSupports, an unknown provider answers true: this gates
// user-visible controls, and hiding a control that actually works is the worse
// failure. Callers that need to distinguish "yes" from "we cannot say" must use
// ExecOptionsHandling instead.
func ExecOptionsHonours(providerType string, field ExecOptionField) bool {
	handling, ok := ExecOptionsHandling(providerType, field)
	if !ok {
		return true
	}
	return handling.Effective()
}

// ProvidersSilentlyIgnoring lists every known provider that drops field without
// a word, sorted. It is the query behind the drift alarm: run it over
// FieldDisallowedTools and it returns the 12 runtimes where a deny-policy is
// decoration.
func ProvidersSilentlyIgnoring(field ExecOptionField) []string {
	var out []string
	for provider := range execOptionsSupport {
		if h, ok := ExecOptionsHandling(provider, field); ok && h == HandlingIgnoredSilent {
			out = append(out, provider)
		}
	}
	sort.Strings(out)
	return out
}

// ExecOptionsMatrixProviders lists every provider in the matrix, sorted.
func ExecOptionsMatrixProviders() []string {
	out := make([]string, 0, len(execOptionsSupport))
	for provider := range execOptionsSupport {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}
