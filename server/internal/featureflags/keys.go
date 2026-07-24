package featureflags

import (
	"context"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const (
	// ComposioMCPApps gates the Composio app management UI and — together with
	// the MUL-3963 permission_mode / invocation_targets access model it depends
	// on — the aligned Private / Public-to picker in the agent create flow.
	// The access model exists to gate Composio sharing, so the two ship on the
	// same switch.
	ComposioMCPApps = "composio_mcp_apps"
	// ResourceLabels controls the agent- and skill-scoped label namespaces.
	// Issue labels remain available while this release flag is off.
	ResourceLabels = "settings_resource_labels"
	// agentBuilderCompat is no longer a release flag. Keep publishing the key
	// as enabled so installed desktop clients that still gate the AI creation
	// entry on this config decision receive the permanently enabled behavior.
	agentBuilderCompat = "agents_agent_builder"
	// agentSkillTogglesCompat is no longer a release flag. Keep publishing the
	// key as enabled so installed v0.4.0 desktop clients, which still gate the
	// switch on this config decision, receive the permanently enabled behavior.
	agentSkillTogglesCompat = "agents_skill_toggles"
	// AutopilotTaskDrivenRuns is the two-phase rollout gate for finalizing
	// create_issue autopilot runs off task outcome (MUL-4809 §4.1). Default OFF:
	// the process finalizes create_issue runs the legacy way (issue status →
	// SyncRunFromIssue) so it stays consistent with old pods whose terminal SQL is
	// unguarded. Phase 1 deploys this binary with the gate off and drains old pods;
	// Phase 2 flips FF_AUTOPILOT_TASK_DRIVEN_RUNS=true so all pods (now on guarded
	// CAS SQL) switch to task-driven finalization together. Server-only; not a
	// frontend public flag. Delete once fully rolled out.
	AutopilotTaskDrivenRuns = "autopilot_task_driven_runs"
)

var frontendPublicFlags = []string{
	ComposioMCPApps,
	ResourceLabels,
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

func ResourceLabelsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ResourceLabels, false)
}

// AutopilotTaskDrivenRunsEnabled reports whether create_issue autopilot runs are
// finalized off task outcome (the new path) rather than issue status (legacy).
// Default OFF for the two-phase rollout (MUL-4809 §4.1). Nil-safe: a nil Service
// returns the default.
func AutopilotTaskDrivenRunsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, AutopilotTaskDrivenRuns, false)
}

func EvaluateFrontendPublicFlags(ctx context.Context, flags *featureflag.Service) map[string]bool {
	out := make(map[string]bool, len(frontendPublicFlags)+2)
	for _, key := range frontendPublicFlags {
		out[key] = flags.IsEnabled(ctx, key, false)
	}
	out[agentBuilderCompat] = true
	out[agentSkillTogglesCompat] = true
	return out
}
