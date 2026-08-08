package agent

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
	// It MUST be in SupportedTypes. The New() switch uses this to pick the
	// backend struct, then passes ID-specific defaults (executable, label)
	// via the descriptor.
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

	// LaunchHeader is the user-visible launch skeleton shown in the UI
	// (e.g. "omp (json mode)").
	LaunchHeader string

	// DefaultExecutable is the binary name the backend falls back to when
	// cfg.ExecutablePath is empty (passed to piBackend.defaultExecutable).
	DefaultExecutable string

	// ProviderLabel is the label used in log/error messages (passed to
	// piBackend.providerLabel).
	ProviderLabel string
}

// BuiltinRuntimes is the registry of built-in runtime identities that are
// NOT in SupportedTypes (they are protocol-family derivatives, not families
// themselves). The daemon probes each one independently and dispatches it
// to its protocol family's backend.
//
// A runtime identity appears here when it needs its own probe/display/skills
// but reuses an existing backend's protocol. The first entry is omp (oh-my-pi):
// a separate CLI that speaks the pi JSON event protocol.
var BuiltinRuntimes = []BuiltinRuntime{
	{
		ID:               "omp",
		ProtocolFamily:    "pi",
		DefaultCommand:    "omp",
		EnvPrefix:         "MULTICA_OMP",
		DisplayName:       "Oh-My-Pi",
		SkillsDir:         ".omp/skills",
		UserSkillsDir:     ".omp/agent/skills",
		LaunchHeader:      "omp (json mode)",
		DefaultExecutable: "omp",
		ProviderLabel:     "omp",
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

// BuiltinRuntimeIDs returns the IDs of all registered built-in runtimes.
func BuiltinRuntimeIDs() []string {
	ids := make([]string, len(BuiltinRuntimes))
	for i, r := range BuiltinRuntimes {
		ids[i] = r.ID
	}
	return ids
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

// ResolveBackendType returns the backend type that New() should dispatch to
// for a given runtime identity. For a SupportedTypes entry it returns the
// type itself; for a built-in runtime identity it returns the protocol family.
func ResolveBackendType(runtimeID string) (backendType string, ok bool) {
	if IsSupportedType(runtimeID) {
		return runtimeID, true
	}
	if r, found := BuiltinRuntimeByID(runtimeID); found {
		return r.ProtocolFamily, true
	}
	return "", false
}
