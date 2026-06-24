package execenv

// RuntimeConfigPath returns the absolute path to the runtime-brief file
// that InjectRuntimeConfig writes for the given provider, or "" when the
// provider has no file-based config target.
//
// Daemon code uses this to log "where did the brief land" alongside the
// rendered char count, so an operator can `cat` the exact file to confirm
// which template a given task was given. The private runtimeConfigPath is
// the implementation; this is a stable export so the daemon does not have
// to thread the provider→filename table in a second place.
func RuntimeConfigPath(workDir, provider string) string {
	return runtimeConfigPath(workDir, provider)
}

// BriefMode returns the human-readable label of the brief path that
// InjectRuntimeConfig would render right now: "slim" when the
// `runtime_brief_slim` feature flag evaluates to on, "legacy" otherwise.
//
// This is intended for daemon observability only — the brief mode is
// always derivable from the flag service, but a structured label keeps log
// queries cheap and lets dashboards filter by mode without re-implementing
// the toggle logic.
//
// Nil-safe by way of useSlimBrief (which falls through Service.IsEnabled's
// nil-safe path), so a daemon that forgot to wire SetFeatureFlags still
// produces a meaningful label ("legacy") rather than panicking.
func BriefMode() string {
	if useSlimBrief() {
		return "slim"
	}
	return "legacy"
}
