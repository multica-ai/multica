package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// The `mcp` transport: a hook that points at an MCP server the plugin author
// already runs, whose tools Multica adopts.
//
// The whole difference from an `http` hook is who decides the shape. An http
// hook declares one endpoint in a manifest an administrator read and approved.
// An MCP server decides its own tool list at runtime and may change it whenever
// it likes — so "install this plugin" would otherwise be a standing grant to run
// whatever that server offers next week.
//
// Hence the approval: an administrator sees the discovered tools and pins them
// by name and schema digest. A tool that appears later is not adopted; a tool
// whose schema drifts stops being called. The daemon-side enforcement already
// exists — validatePinnedRemoteMCPTools refuses at broker startup — so this
// wires the plugin's hook into the connection shape that check already reads.

// PluginMCPApproval is one hook's approved tool list.
type PluginMCPApproval struct {
	Tools      []remotemcp.Tool `json:"tools"`
	ApprovedAt string           `json:"approved_at"`
	ApprovedBy string           `json:"approved_by,omitempty"`
}

// PluginMCPApprovals maps hook key to its approval.
type PluginMCPApprovals map[string]PluginMCPApproval

func decodeMCPApprovals(raw []byte) PluginMCPApprovals {
	approvals := PluginMCPApprovals{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &approvals)
	}
	return approvals
}

// DiscoverMCPHookTools asks the hook's MCP server what it offers.
//
// Read-only and admin-driven: nothing is adopted by discovering it. The
// administrator picks from this and approves, which is the step that makes a
// tool callable.
func (s *PluginService) DiscoverMCPHookTools(ctx context.Context, installation db.PluginInstallation, hookKey string) ([]remotemcp.Tool, error) {
	hook, err := FindHook(installation, hookKey)
	if err != nil {
		return nil, err
	}
	if hook.Transport.Type != plugincontract.TransportMCP {
		return nil, pluginErrf(PluginErrorInvalid, "hook %q is not an mcp transport", hookKey)
	}
	// Second layer. Manifest validation already refuses a hook whose transport
	// URL is not covered by a declared net: scope, so this is unreachable for a
	// manifest that installed cleanly — kept because the destination check below
	// takes its allow-list from here, and an empty one must fail closed rather
	// than read as "no restriction".
	domains := plugincontract.NetDomains(decodeScopes(installation.GrantedScopes))
	if len(domains) == 0 {
		return nil, pluginErrf(PluginErrorForbidden, "this Plugin was granted no net: scope, so it cannot reach an MCP server")
	}

	headers, err := s.mcpCredentialHeaders(ctx, installation, hook)
	if err != nil {
		return nil, err
	}
	// Same endpoint guard as every other outbound call: the destination must be
	// inside the consented `net:` set and resolve publicly.
	tools, _, err := remotemcp.Discover(ctx, hook.Transport.URL, domains, nil, headers)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "could not reach the Plugin's MCP server", Err: err}
	}
	return tools, nil
}

// ApproveMCPHookTools pins a tool list for one hook.
//
// Pinned by digest, not just by name: a server that keeps a tool's name and
// changes its arguments has changed what the agent is calling, and the agent
// would have no way to notice.
func (s *PluginService) ApproveMCPHookTools(ctx context.Context, installation db.PluginInstallation, hookKey string, names []string, userID pgtype.UUID) (db.PluginInstallation, error) {
	discovered, err := s.DiscoverMCPHookTools(ctx, installation, hookKey)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	byName := make(map[string]remotemcp.Tool, len(discovered))
	for _, tool := range discovered {
		byName[tool.Name] = tool
	}

	approved := make([]remotemcp.Tool, 0, len(names))
	for _, name := range names {
		tool, ok := byName[name]
		if !ok {
			// Approving something the server does not currently offer would pin
			// a name with no schema behind it, and the broker would refuse the
			// whole connection at startup.
			return db.PluginInstallation{}, pluginErrf(PluginErrorInvalid, "tool %q is not offered by this MCP server", name)
		}
		approved = append(approved, tool)
	}

	approvals := decodeMCPApprovals(installation.McpApprovals)
	if len(approved) == 0 {
		// Approving nothing is how an administrator withdraws a hook, so it
		// removes the entry rather than storing an empty allow-list that reads
		// like "approved, with nothing in it".
		delete(approvals, hookKey)
	} else {
		approvals[hookKey] = PluginMCPApproval{
			Tools:      approved,
			ApprovedAt: time.Now().UTC().Format(time.RFC3339),
			ApprovedBy: uuidString(userID),
		}
	}

	encoded, err := json.Marshal(approvals)
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorInvalid, Message: "encode approvals", Err: err}
	}
	updated, err := s.Queries.SetPluginMCPApprovals(ctx, db.SetPluginMCPApprovalsParams{
		ID:           installation.ID,
		McpApprovals: encoded,
	})
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "store approvals", Err: err}
	}
	return updated, nil
}

// AgentMCPConnections turns approved mcp hooks into broker connections.
//
// Returned in the claim payload beside the http-transport tools, so the daemon
// handles both through the machinery it already has: the existing broker proxies
// to the MCP server and validatePinnedRemoteMCPTools refuses to start if an
// approved tool went missing or its schema drifted.
//
// A hook with no approval yields no connection. That is the point — installing
// the plugin is not the grant, approving the tools is.
func (s *PluginService) AgentMCPConnections(ctx context.Context, workspaceID pgtype.UUID) ([]remotemcp.Connection, error) {
	installations, err := s.Queries.ListWorkspacePluginInstallations(ctx, workspaceID)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin installations", Err: err}
	}

	connections := make([]remotemcp.Connection, 0)
	for _, installation := range installations {
		if !installation.Enabled {
			continue
		}
		connections = append(connections, mcpConnectionsFor(installation)...)
	}
	return connections, nil
}

// mcpConnectionsFor turns one installation's approved mcp hooks into broker
// connections. Separated from the workspace query so the three conditions that
// decide whether a hook is offered at all — mcp transport, agent trigger, a
// non-empty approval — are testable without a database.
func mcpConnectionsFor(installation db.PluginInstallation) []remotemcp.Connection {
	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		// One unreadable manifest must not hide every other plugin's tools.
		return nil
	}
	approvals := decodeMCPApprovals(installation.McpApprovals)
	domains := plugincontract.NetDomains(decodeScopes(installation.GrantedScopes))

	connections := make([]remotemcp.Connection, 0)
	for _, hook := range manifest.Contributes.Hooks {
		if hook.Transport.Type != plugincontract.TransportMCP {
			continue
		}
		if !HookAllowsTrigger(hook, plugincontract.TriggerAgent) {
			continue
		}
		approval, ok := approvals[hook.Key]
		if !ok || len(approval.Tools) == 0 {
			continue
		}
		credentialHeader := ""
		if field, found := manifest.Config.Field(hook.Key + "_credential"); found && field.Type == plugincontract.ConfigSecret {
			credentialHeader = "Authorization"
		}
		connections = append(connections, remotemcp.Connection{
			InstallationID:  uuidString(installation.ID),
			ContributionID:  remotemcp.PluginContributionPrefix + uuidString(installation.ID) + ":" + hook.Key,
			ContributionKey: PluginToolName(manifest.Key, hook.Key),
			Endpoint:        hook.Transport.URL,
			Transport:       "http",
			// The same exact-host set the consent screen showed. The broker
			// re-checks it at dial, so a manifest that later repoints its own
			// hook still cannot reach anywhere new.
			EndpointAllowedHosts: domains,
			CredentialHeader:     credentialHeader,
			ApprovedTools:        approval.Tools,
			ToolSchemaDigest:     toolSetDigest(approval.Tools),
			// A plugin's MCP server going down must not fail the task, for the
			// same reason a failing http hook is a tool error: an agent should
			// still be able to work on the issue.
			FailurePolicy: "optional",
		})
	}
	return connections
}

// toolSetDigest pins the whole approved set, not just each tool. A digest error
// yields the empty string: the broker then treats the set as unpinned at the
// set level while still checking every tool individually, which is the
// degradation that fails safe.
func toolSetDigest(tools []remotemcp.Tool) string {
	digest, err := remotemcp.ToolSetDigest(tools)
	if err != nil {
		return ""
	}
	return digest
}

// mcpCredentialHeaders builds the auth header from the installation's secret
// config, if the manifest declared one for this hook.
//
// Reuses the existing secret storage — write-only, secretbox-encrypted, never
// echoed by any read endpoint — rather than introducing a second place where a
// plugin's credentials live.
func (s *PluginService) mcpCredentialHeaders(ctx context.Context, installation db.PluginInstallation, hook plugincontract.Hook) (map[string][]string, error) {
	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		return nil, err
	}
	field, ok := manifest.Config.Field(hook.Key + "_credential")
	if !ok || field.Type != plugincontract.ConfigSecret {
		return nil, nil
	}
	secret, err := s.decryptedSecret(ctx, installation.ID, hook.Key+"_credential")
	if err != nil || secret == "" {
		// A declared-but-unset credential is a configuration gap, not a reason
		// to fail discovery with a confusing transport error.
		return nil, nil
	}
	return map[string][]string{"Authorization": {secret}}, nil
}

// decryptedSecret opens one stored secret.
//
// The only place a plugin secret is ever decrypted for use, and it is used
// here to authenticate to the plugin author's OWN server — never echoed back
// through any read endpoint, which is the rule the separate secret table
// exists to make structural.
func (s *PluginService) decryptedSecret(ctx context.Context, installationID pgtype.UUID, key string) (string, error) {
	if s.Secrets == nil {
		return "", pluginErrf(PluginErrorUnavailable, "plugin secrets are disabled: MULTICA_PLUGIN_SECRET_KEY is not configured")
	}
	row, err := s.Queries.GetPluginSecret(ctx, db.GetPluginSecretParams{InstallationID: installationID, Key: key})
	if err != nil {
		return "", err
	}
	plaintext, err := s.Secrets.Open(row.Ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// ApprovedMCPTools is the pinned set for one hook, by name.
func (s *PluginService) ApprovedMCPTools(installation db.PluginInstallation, hookKey string) map[string]remotemcp.Tool {
	approval, ok := decodeMCPApprovals(installation.McpApprovals)[hookKey]
	if !ok {
		return map[string]remotemcp.Tool{}
	}
	byName := make(map[string]remotemcp.Tool, len(approval.Tools))
	for _, tool := range approval.Tools {
		byName[tool.Name] = tool
	}
	return byName
}

// MCPHookCredential resolves the credential for one mcp hook, for the daemon's
// broker. Returns empty strings when the manifest declared none.
func (s *PluginService) MCPHookCredential(ctx context.Context, installation db.PluginInstallation, hookKey string) (string, string, error) {
	hook, err := FindHook(installation, hookKey)
	if err != nil {
		return "", "", err
	}
	headers, err := s.mcpCredentialHeaders(ctx, installation, hook)
	if err != nil {
		return "", "", err
	}
	for name, values := range headers {
		if len(values) > 0 {
			return name, values[0], nil
		}
	}
	return "", "", nil
}
