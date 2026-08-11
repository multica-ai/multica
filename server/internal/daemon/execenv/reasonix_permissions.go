package execenv

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
)

// reasonixProjectConfigFile is Reasonix's project-scoped config. Reasonix
// resolves configuration as flag > project ./reasonix.toml > user config.toml >
// defaults, rooted at the session cwd — which for a task is the workdir — so a
// file written here binds to this task only and leaves the runtime owner's
// interactive sessions alone.
const reasonixProjectConfigFile = "reasonix.toml"

// reasonixProjectConfig turns off Reasonix's `ask` tool for the task.
//
// `ask` puts a multiple-choice question to a human mid-turn. An unattended task
// has no human: Reasonix wires an asker for every ACP session, so the runtime's
// own "no interactive user — decide for yourself" fallback is unreachable, the
// daemon can only refuse the question, and Reasonix reads a refusal as a
// dismissal and cancels the whole turn. The task fails over a decision the model
// could have made itself.
//
// A deny rule is the one lever that covers the whole turn. Reasonix checks deny
// rules before the read-only allow fallthrough, so it applies to `ask` despite
// `ask` being read-only, and the executor, the planner, and sub-agents all
// decide against the same policy — where a tool-call hook would only ever reach
// the executor. The model gets "denied by permission policy" back as an ordinary
// tool result and keeps working.
//
// Deny is a replacing list, not an additive one: for the duration of a task in
// this workdir this is the effective deny list, so a deny rule in the runtime
// owner's global config does not apply here. That is a deliberate, accepted
// widening of what the agent may run in a task workdir — not something the
// daemon's other constraints (--workspace-only, the ACP permission policy) make
// equivalent. Extending the global list instead would mean parsing the runtime
// owner's config to restate it, which is its own failure mode.
const reasonixProjectConfig = `# Managed by Multica. Written per task, removed when the task env is cleaned up.
# Edits are not preserved.

[permissions]
# No human can answer a question in an unattended task, and an unanswered
# question cancels the Reasonix turn. Denying the tool instead lets the model
# take its own decision and keep going.
deny = ["ask"]
`

// writeReasonixProjectConfig lays down the per-task reasonix.toml in workDir.
//
// A reasonix.toml that came with the repository is left untouched: sidecar
// cleanup is a pure deletion of paths the daemon created, so it must never
// overwrite user content. That case is a warning rather than a failure — the
// task still runs, `ask` stays available, and a question that does arrive hits
// the existing fail-closed permission handling in the reasonix backend.
func writeReasonixProjectConfig(workDir string, manifest *sidecarManifest, logger *slog.Logger) error {
	if workDir == "" {
		return nil
	}
	path := filepath.Join(workDir, reasonixProjectConfigFile)
	err := recordWriteFile(path, []byte(reasonixProjectConfig), 0o644, manifest)
	if errors.Is(err, errPathPreExists) {
		if logger != nil {
			logger.Warn("execenv: project reasonix.toml already exists; leaving it untouched — the reasonix ask tool stays enabled for this task",
				"path", path,
			)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", reasonixProjectConfigFile, err)
	}
	return nil
}
