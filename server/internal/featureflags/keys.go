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
	// DesktopHangStackCapture gates reading a JS call stack out of a hung
	// desktop renderer (MUL-5345). Capture holds a debugger channel open on
	// every renderer, so the desktop client is fail-closed: it stays off unless
	// this key arrives as an explicit true. That makes publishing the key here
	// mandatory — a key the client never receives can never be turned on.
	DesktopHangStackCapture = "desktop_hang_stack_capture"
	// agentBuilderCompat is no longer a release flag. Keep publishing the key
	// as enabled so installed desktop clients that still gate the AI creation
	// entry on this config decision receive the permanently enabled behavior.
	agentBuilderCompat = "agents_agent_builder"
	// agentSkillTogglesCompat is no longer a release flag. Keep publishing the
	// key as enabled so installed v0.4.0 desktop clients, which still gate the
	// switch on this config decision, receive the permanently enabled behavior.
	agentSkillTogglesCompat = "agents_skill_toggles"
)

var frontendPublicFlags = []string{
	ComposioMCPApps,
	ResourceLabels,
	DesktopHangStackCapture,
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

func ResourceLabelsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ResourceLabels, false)
}

func DesktopHangStackCaptureEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, DesktopHangStackCapture, false)
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
