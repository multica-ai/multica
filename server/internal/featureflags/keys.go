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
	// AgentBuilder controls writes of system builder agents. It stays disabled
	// through the schema-only rollout so an older server cannot expose them.
	AgentBuilder = "agents_agent_builder"
	// ResourceLabels controls the agent- and skill-scoped label namespaces.
	// Issue labels remain available while this release flag is off.
	ResourceLabels = "settings_resource_labels"
	// AgentSkillToggles controls writes of agent_skill.enabled=false. Older
	// servers do not filter that state when preparing an agent task.
	AgentSkillToggles = "agents_skill_toggles"
)

var frontendPublicFlags = []string{
	ComposioMCPApps,
	AgentBuilder,
	ResourceLabels,
	AgentSkillToggles,
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

func AgentBuilderEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, AgentBuilder, false)
}

func ResourceLabelsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ResourceLabels, false)
}

func AgentSkillTogglesEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, AgentSkillToggles, false)
}

func EvaluateFrontendPublicFlags(ctx context.Context, flags *featureflag.Service) map[string]bool {
	out := make(map[string]bool, len(frontendPublicFlags))
	for _, key := range frontendPublicFlags {
		out[key] = flags.IsEnabled(ctx, key, false)
	}
	return out
}
