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
	// agentBuilderCompat is no longer a release flag. Keep publishing the key
	// as enabled so installed desktop clients that still gate the AI creation
	// entry on this config decision receive the permanently enabled behavior.
	agentBuilderCompat = "agents_agent_builder"
	// agentSkillTogglesCompat is no longer a release flag. Keep publishing the
	// key as enabled so installed v0.4.0 desktop clients, which still gate the
	// switch on this config decision, receive the permanently enabled behavior.
	agentSkillTogglesCompat = "agents_skill_toggles"
	// EventHooks gates the Event Hooks engine (MUL-4332). It does not gate the
	// transactional-outbox event layer, which is always-on so no trigger is ever
	// lost; it gates the policy API and the matcher, i.e. everything that decides
	// anything. Server-only, default off, evaluated PER WORKSPACE.
	EventHooks = "automation_event_hooks"
	// EventHookExecution gates ACTION EXECUTION specifically, separately from
	// EventHooks. The two are deliberately distinct rollout stages: EventHooks
	// alone opens the policy API, dry-run/explain and the matcher, which only
	// record queued/skipped decisions, so a workspace can run the engine in shadow
	// and inspect what it *would* do. Only this second switch lets the executor
	// claim those queued executions and perform real side effects, so turning the
	// engine on for shadow evaluation can never start mutating data.
	// Server-only, default off, and required IN ADDITION to EventHooks.
	EventHookExecution = "automation_event_hook_execution"
	// resourceLabelsCompat is no longer a release flag. Keep publishing the key
	// as enabled for installed desktop clients from v0.4.0 through at least
	// v0.4.15, every release shipped before this change. Unlike the skill-toggle
	// gate above, which was removed client-side in v0.4.1, the resource-label
	// gate remained in every such client and fails closed (default false) if
	// the key stops being published.
	resourceLabelsCompat = "settings_resource_labels"
)

var frontendPublicFlags = []string{
	ComposioMCPApps,
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

// EventHooksEnabled reports whether the Event Hooks engine may run reactions.
// The outbox writer never consults it — events are recorded unconditionally so no
// trigger is lost — and it gates the API surface plus the matcher.
//
// Callers MUST supply an EvalContext carrying the workspace this decision is about
// (HookService.hooksEnabledFor / Handler.hookFlagContext do). Asking with a bare
// process context answers for "no workspace", which no workspace-targeted rule can
// match — that is what made allow_by: workspace_id silently a no-op.
func EventHooksEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, EventHooks, false)
}

// EventHookExecutionEnabled reports whether the executor may run real actions. It
// requires BOTH switches: the engine as a whole, and execution specifically. A
// workspace that has only enabled EventHooks stays in shadow mode, where matches are
// recorded in a terminal status the executor can never claim — so a later flip of
// this switch cannot retroactively fire the observation period.
//
// Same EvalContext requirement as EventHooksEnabled.
func EventHookExecutionEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return EventHooksEnabled(ctx, flags) && flags.IsEnabled(ctx, EventHookExecution, false)
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
