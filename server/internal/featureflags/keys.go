package featureflags

import (
	"context"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const (
	// ComposioMCPApps gates the Composio app management UI and runtime MCP
	// overlay injection.
	ComposioMCPApps = "composio_mcp_apps"
	// AgentAccessPicker gates the MUL-3963 permission_mode + invocation_targets
	// picker in the agent create/duplicate flow. When OFF the create dialog
	// keeps the legacy Workspace/Personal toggle; when ON it switches to the
	// same private / public_to model used on the agent detail page.
	AgentAccessPicker = "agent_access_picker"
)

var frontendPublicFlags = []string{
	ComposioMCPApps,
	AgentAccessPicker,
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

func EvaluateFrontendPublicFlags(ctx context.Context, flags *featureflag.Service) map[string]bool {
	out := make(map[string]bool, len(frontendPublicFlags))
	for _, key := range frontendPublicFlags {
		out[key] = flags.IsEnabled(ctx, key, false)
	}
	return out
}
