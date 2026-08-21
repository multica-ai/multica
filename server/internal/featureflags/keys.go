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
	// CustomIssueStatuses gates CREATING a custom issue status (MUL-6243). It is
	// a rollout gate, not a behavior switch, and it is deliberately one-way.
	//
	// The readers ship unconditionally and are safe to: issuestatus.Effective is
	// the identity function on the 7 built-in keys, so a pod running this code
	// behaves exactly as before until a custom status exists. The hazard is the
	// other direction — the first custom status written by a new pod is a value
	// an OLD pod cannot interpret, and old pods would fall back to literal
	// comparisons and mishandle the issue's events and tasks. Gating creation
	// means that value cannot come into existence until the whole fleet can read
	// it, which removes the need for a coordinated restart.
	//
	// Enable only after every pod is running this version or later. Once a
	// workspace has custom statuses, turning it back off stops new ones being
	// created but does NOT make the existing ones safe for an older binary; a
	// true rollback requires migrating those issues back to built-in statuses
	// first (migration 337's down direction refuses precisely because of this).
	CustomIssueStatuses = "custom_issue_statuses"
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
	// The settings UI needs this to decide whether to offer status creation at
	// all. Without it the tab would show a "New status" button that 403s.
	CustomIssueStatuses,
}

func BillingWorkspaceSubscriptionsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, BillingWorkspaceSubscriptions, false)
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

func PluginsV1Enabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, PluginsV1, false)
}

// CustomIssueStatusesEnabled reports whether creating custom issue statuses is
// allowed. Default false: a fleet mid-rollout must not be able to mint a status
// value its older pods cannot interpret.
func CustomIssueStatusesEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, CustomIssueStatuses, false)
}

func EvaluateFrontendPublicFlags(ctx context.Context, flags *featureflag.Service) map[string]bool {
	out := make(map[string]bool, len(frontendPublicFlags)+3)
	for _, key := range frontendPublicFlags {
		out[key] = flags.IsEnabled(ctx, key, false)
	}
	out[agentBuilderCompat] = true
	out[agentSkillTogglesCompat] = true
	out[resourceLabelsCompat] = true
	return out
}
