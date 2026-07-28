package localtoolpolicy

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
)

type providerLookupDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const unavailableProviderToolKey = "tools:__provider_lookup_failed__"

// ProviderPolicyToolKey maps a provider-native hook name to the capability key
// stored in the runtime inventory and immutable task mandate.
func ProviderPolicyToolKey(provider, toolName string) string {
	canonicalName := capabilitycatalog.CanonicalRuntimeToolName(provider, toolName)
	return claudehook.PolicyToolKey(canonicalName)
}

// ProviderMandateToolKey maps the provider-native policy identity to the exact
// callable identity stored for server-dispatched workspace Connection endpoints.
// Local MCP hooks wrap those endpoint names as mcp__multica__<connection>__<op>,
// while the shared Gateway registry and task mandate use <connection>__<op>.
func ProviderMandateToolKey(policyToolKey string) string {
	const workspaceMCPPrefix = "mcp__multica__"
	name, ok := strings.CutPrefix(strings.TrimSpace(policyToolKey), workspaceMCPPrefix)
	if !ok || !strings.Contains(name, "__") {
		return policyToolKey
	}
	return name
}

// ProviderResourcePattern extracts the concrete resource from provider-native
// arguments. An already-supplied pattern wins for protocol compatibility.
func ProviderResourcePattern(provider, toolName, resourcePattern string, args map[string]any) string {
	if resourcePattern != "" {
		return resourcePattern
	}
	resourceTool := toolName
	if provider == "cursor" {
		switch toolName {
		case "Shell":
			resourceTool = "Bash"
		case "StrReplace", "Delete":
			resourceTool = "Edit"
		}
	}
	return claudehook.ResourcePattern(resourceTool, args)
}

// ProviderToolCallForAgent resolves the provider server-side so an old
// daemon does not need to send new identity fields before the fix takes effect.
// Lookup failure returns a key that cannot be present in the task mandate, so
// the existing mandate check fails closed before policy resolution.
func ProviderToolCallForAgent(ctx context.Context, db providerLookupDB, workspaceID, agentID pgtype.UUID, toolName, resourcePattern string, args map[string]any) (string, string) {
	if db == nil || !workspaceID.Valid || !agentID.Valid {
		return unavailableProviderToolKey, resourcePattern
	}
	var provider string
	if err := db.QueryRow(ctx, `
		SELECT runtime.provider
		FROM agent
		JOIN agent_runtime AS runtime ON runtime.id = agent.runtime_id
		WHERE agent.workspace_id = $1 AND agent.id = $2
	`, workspaceID, agentID).Scan(&provider); err != nil {
		return unavailableProviderToolKey, resourcePattern
	}
	return ProviderPolicyToolKey(provider, toolName), ProviderResourcePattern(provider, toolName, resourcePattern, args)
}
