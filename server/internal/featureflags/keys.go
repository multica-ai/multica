package featureflags

import (
	"context"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const (
	// BillingWorkspaceSubscriptions gates the workspace-scoped entitlement,
	// Stripe Checkout, seat reconcile, and Billing Portal proxy surface. It is
	// deliberately off by default so the main repository can ship before the
	// managed cloud enables its matching billing.subscriptions capability.
	BillingWorkspaceSubscriptions = "billing_workspace_subscriptions"
	// ComposioMCPApps gates the Composio app management UI and — together with
	// the MUL-3963 permission_mode / invocation_targets access model it depends
	// on — the aligned Private / Public-to picker in the agent create flow.
	// The access model exists to gate Composio sharing, so the two ship on the
	// same switch.
	ComposioMCPApps = "composio_mcp_apps"
	// PluginsV1 gates the user-facing Plugin catalog and lifecycle management
	// APIs while the first product slice is dogfooded. It deliberately does not
	// gate pinned Task/Run execution: disabling discovery and management must not
	// mutate an immutable execution manifest that is already in flight.
	PluginsV1 = "plugins_v1"
	// ProjectPlans gates the plan repository plus its read and write services.
	// It is published for the frontend Plan views and defaults OFF — see
	// flagDefaults. The key is deliberately retained so an operator can turn
	// plans on per deployment with FF_PROJECT_PLANS=true or a flag-file rule.
	ProjectPlans = "project_plans"
	// agentBuilderCompat is no longer a release flag. Keep publishing the key
	// as enabled so installed desktop clients that still gate the AI creation
	// entry on this config decision receive the permanently enabled behavior.
	agentBuilderCompat = "agents_agent_builder"
	// agentSkillTogglesCompat is no longer a release flag. Keep publishing the
	// key as enabled so installed v0.4.0 desktop clients, which still gate the
	// switch on this config decision, receive the permanently enabled behavior.
	agentSkillTogglesCompat = "agents_skill_toggles"
	// resourceLabelsCompat is no longer a release flag. Keep publishing the key
	// as enabled for installed desktop clients from v0.4.0 through at least
	// v0.4.15, every release shipped before this change. Unlike the skill-toggle
	// gate above, which was removed client-side in v0.4.1, the resource-label
	// gate remained in every such client and fails closed (default false) if
	// the key stops being published.
	resourceLabelsCompat = "settings_resource_labels"
)

var frontendPublicFlags = []string{
	BillingWorkspaceSubscriptions,
	ComposioMCPApps,
	PluginsV1,
	ProjectPlans,
}

// flagDefaults is the single source of truth for what a flag evaluates to when
// no provider supplies a decision — no FF_<KEY> env override and no entry in
// the MULTICA_FEATURE_FLAGS_FILE rules. An env override still beats a default,
// which is what keeps FF_<KEY>=false working as a kill switch for a flag
// listed here as on.
//
// Keys absent from the map default to false, so registering a flag leaves it
// off until it is listed here deliberately.
//
// Both read paths take their default from here: the *Enabled gates below, and
// the frontend publication in EvaluateFrontendPublicFlags. Neither writes a
// default literal of its own, so the route gate and the /api/config payload
// cannot drift apart — a route can never serve a surface the published config
// reports as off, or vice versa.
var flagDefaults = map[string]bool{
	// Plans ship off: enabling them is a rollout decision for the operator,
	// not a default the code makes. FF_PROJECT_PLANS=true turns them on.
	ProjectPlans: false,
}

// defaultFor reports the configured fallback for a flag key. Unlisted keys are
// off.
func defaultFor(key string) bool {
	return flagDefaults[key]
}

func BillingWorkspaceSubscriptionsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, BillingWorkspaceSubscriptions, defaultFor(BillingWorkspaceSubscriptions))
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, defaultFor(ComposioMCPApps))
}

func PluginsV1Enabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, PluginsV1, defaultFor(PluginsV1))
}

func ProjectPlansEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ProjectPlans, defaultFor(ProjectPlans))
}

func EvaluateFrontendPublicFlags(ctx context.Context, flags *featureflag.Service) map[string]bool {
	out := make(map[string]bool, len(frontendPublicFlags)+3)
	for _, key := range frontendPublicFlags {
		out[key] = flags.IsEnabled(ctx, key, defaultFor(key))
	}
	out[agentBuilderCompat] = true
	out[agentSkillTogglesCompat] = true
	out[resourceLabelsCompat] = true
	return out
}
