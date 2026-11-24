package agent

import (
	"context"
	"fmt"
)

// BuiltinRuntime describes a built-in runtime identity that the daemon probes
// and registers automatically — independent of the custom runtime profile
// protocol_family whitelist. Multiple runtime identities can share one
// protocol family (e.g. both "pi" and "omp" use the "pi" protocol backend).
//
// The descriptor is the single declaration site for a runtime identity:
// agents_probe.go, config.go, daemon.go display-name overrides, execenv
// skill/config paths, local_skills.go, ListModels, and the frontend display
// maps all derive from this. Adding a new compatible fork of an existing
// runtime is a descriptor entry, not a cross-stack change.
type BuiltinRuntime struct {
	// ID is the provider key the daemon registers under (e.g. "omp").
	// It is NOT a protocol_family — it does not appear in SupportedTypes or
	// the runtime_profile.protocol_family CHECK constraint.
	ID string

	// ProtocolFamily is the execution backend this runtime dispatches to.
	// It MUST be in SupportedTypes. NewRuntime builds that family's backend
	// via New(), then applies this descriptor's ID-specific defaults
	// (executable, label) to it.
	ProtocolFamily string

	// DefaultCommand is the bare CLI name the probe looks up on PATH
	// when MULTICA_<ID>_PATH is not set (e.g. "omp").
	DefaultCommand string

	// EnvPrefix is the MULTICA_ prefix for *_PATH and *_MODEL env overrides
	// (e.g. "MULTICA_OMP" → MULTICA_OMP_PATH / MULTICA_OMP_MODEL).
	EnvPrefix string

	// DisplayName is the human-facing runtime name. The daemon and frontend
	// both use this so they never drift apart.
	DisplayName string

	// SkillsDir is the project-level skills directory relative to the
	// workdir (e.g. ".omp/skills").
	SkillsDir string

	// UserSkillsDir is the user-level skills directory relative to $HOME
	// (e.g. ".omp/agent/skills").
	UserSkillsDir string

	// ConfigHomeDir is the runtime's default user-level config directory
	// relative to $HOME (e.g. ".langcli"), where the CLI keys sessions,
	// transcripts, and cost state. Empty means the protocol family's default
	// (".claude" for the claude backend). Runtimes that relocate their config
	// home must declare it, or resume-time usage baselines are read from the
	// family's directory where the fork writes nothing.
	ConfigHomeDir string

	// LaunchHeader is the user-visible launch skeleton shown in the UI
	// (e.g. "omp (json mode)").
	LaunchHeader string

	// DefaultExecutable is the binary name the backend falls back to when
	// cfg.ExecutablePath is empty (passed to piBackend.defaultExecutable).
	DefaultExecutable string

	// ProviderLabel is the label used in log/error messages (passed to
	// piBackend.providerLabel).
	ProviderLabel string

	// ModelDiscovery is the strategy for discovering available models.
	// When set, it replaces the protocol family's discovery entirely — omp
	// uses `omp models --json`, a different command and output shape from
	// pi's `--list-models`. When nil, ListModels returns an empty catalog
	// rather than falling back to the family's command: running a
	// semantically incompatible one (omp exits non-zero on `--list-models`)
	// is worse than degrading to manual entry.
	ModelDiscovery ModelDiscoveryFunc
}

// ModelDiscoveryFunc discovers available models for a runtime identity.
// It receives the context and the resolved command — executable plus the
// runtime's launch prefix — and returns a model catalog. Returning the whole
// Catalog rather than a bare slice lets a fork inherit its family's
// unavailable-entry and thinking-level annotations instead of flattening them.
// Discovery failures propagate to the picker, which shows the reason and
// supports manual entry.
type ModelDiscoveryFunc func(ctx context.Context, runtimeCmd Command) (Catalog, error)

// BuiltinRuntimes is the registry of built-in runtime identities that are
// NOT in SupportedTypes (they are protocol-family derivatives, not families
// themselves). The daemon probes each one independently and dispatches it
// to its protocol family's backend.
//
// A runtime identity appears here when it needs its own probe/display/skills
// but reuses an existing backend's protocol. omp (oh-my-pi) is a separate CLI
// that speaks the pi JSON event protocol. langcli is a Claude Code fork that
// keeps Claude Code's stream-json CLI surface (`-p`, `--output-format
// stream-json`, `--resume`, `--permission-mode`) and its `.claude/skills` +
// CLAUDE.md discovery conventions, so it dispatches to the claude backend.
var BuiltinRuntimes = []BuiltinRuntime{
	{
		ID:                "omp",
		ProtocolFamily:    "pi",
		DefaultCommand:    "omp",
		EnvPrefix:         "MULTICA_OMP",
		DisplayName:       "Oh-My-Pi",
		SkillsDir:         ".omp/skills",
		UserSkillsDir:     ".omp/agent/skills",
		LaunchHeader:      "omp (json mode)",
		DefaultExecutable: "omp",
		ProviderLabel:     "omp",
		ModelDiscovery:    discoverOmpCatalog,
	},
	{
		ID:             "langcli",
		ProtocolFamily: "claude",
		DefaultCommand: "langcli",
		EnvPrefix:      "MULTICA_LANGCLI",
		DisplayName:    "Langcli",
		// Project-level skills stay in Claude Code's native location. Langcli's
		// own config home is ~/.langcli (CLAUDE_CONFIG_DIR override), so its
		// user-level skill root is not ~/.claude/skills, and its session
		// transcripts/cost state live under ~/.langcli/projects.
		SkillsDir:         ".claude/skills",
		UserSkillsDir:     ".langcli/skills",
		ConfigHomeDir:     ".langcli",
		LaunchHeader:      "langcli (stream-json)",
		DefaultExecutable: "langcli",
		ProviderLabel:     "langcli",
		ModelDiscovery:    discoverLangcliCatalog,
	},
}

// BuiltinRuntimeByID returns the descriptor for the given runtime identity,
// or false if no such built-in runtime exists.
func BuiltinRuntimeByID(id string) (BuiltinRuntime, bool) {
	for _, r := range BuiltinRuntimes {
		if r.ID == id {
			return r, true
		}
	}
	return BuiltinRuntime{}, false
}

// IsBuiltinRuntime reports whether id is a registered built-in runtime
// identity (as opposed to a protocol family in SupportedTypes).
func IsBuiltinRuntime(id string) bool {
	_, ok := BuiltinRuntimeByID(id)
	return ok
}

// BuiltinRuntimeCommands returns the default CLI command names for all
// built-in runtimes, for the daemon's defaultAgentCommandNames list.
func BuiltinRuntimeCommands() []string {
	cmds := make([]string, len(BuiltinRuntimes))
	for i, r := range BuiltinRuntimes {
		cmds[i] = r.DefaultCommand
	}
	return cmds
}

// backendOverrideApplicator is the interface that lets a descriptor apply
// per-runtime overrides (executable, label) to the backend its protocol family
// returned. A family whose backend does not implement it cannot host a runtime
// identity at all: NewRuntime fails closed rather than handing back a backend
// with the overrides silently dropped.
type backendOverrideApplicator interface {
	applyBuiltinRuntimeOverrides(desc BuiltinRuntime)
}

// piBackend already implements this via its defaultExecutable and
// providerLabel fields. The method is defined here (not in pi.go) to keep
// the descriptor→backend contract in one file.
func (b *piBackend) applyBuiltinRuntimeOverrides(desc BuiltinRuntime) {
	b.defaultExecutable = desc.DefaultExecutable
	b.providerLabel = desc.ProviderLabel
}

// claudeBackend carries the same overrides so a Claude Code fork registered as
// its own runtime identity launches its own binary, names itself in logs and
// task errors, and resolves session transcripts from its own config home.
func (b *claudeBackend) applyBuiltinRuntimeOverrides(desc BuiltinRuntime) {
	b.defaultExecutable = desc.DefaultExecutable
	b.providerLabel = desc.ProviderLabel
	b.configHomeDir = desc.ConfigHomeDir
}

// ProtocolFamilyFor resolves a provider key to the protocol family that backs
// it. Protocol families are their own family; built-in runtime identities
// (e.g. "langcli") resolve to the family in their descriptor. Callers that
// branch on a family's CLI behaviour should compare against the result rather
// than against a literal family name, so a fork inherits that behaviour.
func ProtocolFamilyFor(provider string) string {
	if desc, ok := BuiltinRuntimeByID(provider); ok {
		return desc.ProtocolFamily
	}
	return provider
}

// discoverLangcliCatalog uses the claude family's discovery: the fork speaks
// the same control protocol. It deliberately does NOT substitute the Claude
// static catalog when discovery fails: langcli runs its own model set
// (DeepSeek, GLM, GPT, Claude, ...), so Anthropic IDs would advertise models
// the runtime may not accept. An old or failing binary reports no models
// instead and the picker falls back to manual entry.
//
// The installed binary's own `--effort` vocabulary is attached in both the
// success and fallback paths. Langcli builds its model rows from a shared
// EFFORT_LEVELS superset, so a build can advertise xhigh in list_models while
// its flag parser still rejects `--effort xhigh`; without the help-derived
// vocabulary the daemon would inject the level and the CLI would refuse to
// start. A nil result (help unreadable) keeps the validation pass-through.
//
// ListModels resolves an empty Command.Path from the descriptor's
// DefaultCommand before calling this, so an unset path never enumerates the
// family's binary instead of the fork's.
func discoverLangcliCatalog(ctx context.Context, runtimeCmd Command) (Catalog, error) {
	catalog := discoverClaudeCatalog(ctx, runtimeCmd)
	if catalog.Fallback {
		// The static fallback already probed --help and carries its vocabulary.
		return Catalog{Models: []Model{}, CLIThinkingLevels: catalog.CLIThinkingLevels}, nil
	}
	// A live catalog does not, and its rows over-advertise the fork's shared
	// effort superset — attach the CLI's own flag vocabulary here.
	catalog.CLIThinkingLevels = annotateClaudeThinking(ctx, nil, runtimeCmd)
	return catalog, nil
}

// discoverOmpCatalog adapts discoverOmpModels to the catalog-shaped descriptor
// contract.
func discoverOmpCatalog(ctx context.Context, runtimeCmd Command) (Catalog, error) {
	return discovered(discoverOmpModels(ctx, runtimeCmd))
}

// ResolveBackend is the single production entry point the daemon uses to
// construct a backend from a provider key. It routes built-in runtime
// identities (e.g. "omp") through NewRuntime, and protocol families (e.g.
// "pi", "claude") through New. This keeps both factories meaning exactly one
// thing: New() is family-only, NewRuntime() is runtime-identity-only, and
// the daemon never has to know which is which.
func ResolveBackend(provider string, cfg Config) (Backend, error) {
	if IsBuiltinRuntime(provider) {
		return NewRuntime(provider, cfg)
	}
	return New(provider, cfg)
}

// NewRuntime creates a Backend for a built-in runtime identity (e.g. "omp").
// It builds the descriptor's protocol family backend via New(), then applies
// the per-runtime overrides through the backendOverrideApplicator interface.
// This is the production entry point for runtime identities; New() is
// family-only and rejects runtime ids.
//
// Fails closed: if the protocol family backend does not implement
// backendOverrideApplicator, the descriptor's overrides cannot be applied and
// the runtime would launch under the wrong executable and label — so
// NewRuntime returns an error rather than returning an unmodified backend.
func NewRuntime(runtimeID string, cfg Config) (Backend, error) {
	desc, ok := BuiltinRuntimeByID(runtimeID)
	if !ok {
		return nil, fmt.Errorf("unknown runtime identity: %q", runtimeID)
	}
	cfg.provider = runtimeID
	backend, err := New(desc.ProtocolFamily, cfg)
	if err != nil {
		return nil, fmt.Errorf("runtime %q (family %q): %w", runtimeID, desc.ProtocolFamily, err)
	}
	applicator, ok := backend.(backendOverrideApplicator)
	if !ok {
		return nil, fmt.Errorf("runtime %q: protocol family %q backend %T does not support runtime overrides", runtimeID, desc.ProtocolFamily, backend)
	}
	applicator.applyBuiltinRuntimeOverrides(desc)
	return backend, nil
}

// ProfileRuntimeType preserves profiles authored before runtime identity was stored.
func ProfileRuntimeType(runtimeType, protocolFamily string) string {
	if runtimeType != "" {
		return runtimeType
	}
	return protocolFamily
}

// RuntimeProtocolFamily resolves the compatibility target using the same registry
// as backend construction and model discovery.
func RuntimeProtocolFamily(runtimeType string) (string, bool) {
	if desc, ok := BuiltinRuntimeByID(runtimeType); ok {
		return desc.ProtocolFamily, true
	}
	return runtimeType, IsSupportedType(runtimeType)
}
