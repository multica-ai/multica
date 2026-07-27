package featureflags

import (
	"context"

	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/providerfailover"
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

	// ProviderFailover gates the GPT->Claude usage/rate-limit failover subsystem
	// (td-836aa9). OFF by default: the feature does nothing. When ON, the policy
	// runs in SHADOW mode — it records what it would do without changing any task
	// outcome, agent binding, or dispatch. This is the safe default posture and
	// the surface an operator watches to evaluate a rollout.
	ProviderFailover = "provider_failover"
	// ProviderFailoverActive is the second, independent rollout gate. It only has
	// effect when ProviderFailover is also ON, and it promotes the subsystem from
	// shadow to ACTIVE mode — real Claude handoffs. Deny-by-default: enabling
	// active handoffs is a deliberate two-step so shadow evaluation always
	// precedes action.
	ProviderFailoverActive = "provider_failover_active"
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

// ProviderFailoverMode resolves the operational posture of the failover
// subsystem from the two rollout gates. A nil Service resolves to Off (every
// IsEnabled returns its default). The gates compose deliberately:
//
//	provider_failover off                         -> Off    (feature disabled)
//	provider_failover on, active off (default)     -> Shadow (record only)
//	provider_failover on, active on                -> Active (real handoffs)
//
// Active can never be reached without shadow first being on, so an accidental
// enable of the active gate alone is inert.
func ProviderFailoverMode(ctx context.Context, flags *featureflag.Service) providerfailover.Mode {
	if !flags.IsEnabled(ctx, ProviderFailover, false) {
		return providerfailover.ModeOff
	}
	if flags.IsEnabled(ctx, ProviderFailoverActive, false) {
		return providerfailover.ModeActive
	}
	return providerfailover.ModeShadow
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
