package service

import (
	"encoding/json"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// The approval is the grant, not the install.
//
// An http hook declares one endpoint in a manifest an administrator read. An
// MCP server decides its own tool list at runtime, so without a pinned approval
// "install this plugin" would be standing permission to run whatever that
// server offers next week.

const mcpHookManifest = `{
	"manifest_version": 1,
	"key": "com.example.mcp",
	"name": "MCP Plugin",
	"description": "d",
	"version": "1.0.0",
	"author": {"name": "example"},
	"scopes": ["issues:read", "net:tools.example.com"],
	"contributes": {"hooks": [{
		"key": "toolbox",
		"name": "Toolbox",
		"description": "Tools from an external MCP server.",
		"triggers": ["agent"],
		"transport": {"type": "mcp", "url": "https://tools.example.com/mcp"}
	}]}
}`

func mcpInstallation(t *testing.T, approvals string) db.PluginInstallation {
	t.Helper()
	if approvals == "" {
		approvals = "{}"
	}
	return db.PluginInstallation{
		ID:            testInstallationID(t),
		WorkspaceID:   testInstallationID(t),
		Enabled:       true,
		Manifest:      []byte(mcpHookManifest),
		GrantedScopes: []byte(`["issues:read","net:tools.example.com"]`),
		McpApprovals:  []byte(approvals),
	}
}

// No approval, no connection. This is the whole safety property: an installed
// mcp hook is inert until an administrator pins its tools, so the plugin being
// installed and enabled is not by itself permission for an agent to call
// anything that server offers.
func TestUnapprovedMCPHookYieldsNoConnection(t *testing.T) {
	installation := mcpInstallation(t, "{}")

	if tools := (&PluginService{}).ApprovedMCPTools(installation, "toolbox"); len(tools) != 0 {
		t.Fatalf("an unapproved hook reported approved tools: %v", tools)
	}
	if connections := mcpConnectionsFor(installation); len(connections) != 0 {
		t.Fatalf("an unapproved hook produced %d connections, want none", len(connections))
	}

	// An approval that exists but is empty is a withdrawal, not an allow-all.
	withdrawn := mcpInstallation(t, `{"toolbox":{"tools":[]}}`)
	if connections := mcpConnectionsFor(withdrawn); len(connections) != 0 {
		t.Fatalf("a withdrawn hook produced %d connections, want none", len(connections))
	}
}

// An approved hook reaches the daemon as an ordinary broker connection, pinned
// to its approved tools and to the exact hosts the consent screen showed.
func TestApprovedMCPHookBecomesABrokerConnection(t *testing.T) {
	approvals, err := json.Marshal(PluginMCPApprovals{
		"toolbox": {Tools: []remotemcp.Tool{{Name: "search", SchemaDigest: "sha256:aaa"}}},
	})
	if err != nil {
		t.Fatalf("marshal approvals: %v", err)
	}
	connections := mcpConnectionsFor(mcpInstallation(t, string(approvals)))
	if len(connections) != 1 {
		t.Fatalf("got %d connections, want exactly one", len(connections))
	}

	connection := connections[0]
	if len(connection.ApprovedTools) != 1 || connection.ApprovedTools[0].Name != "search" {
		t.Fatalf("approved tools = %v, want the pinned set", connection.ApprovedTools)
	}
	if len(connection.EndpointAllowedHosts) != 1 || connection.EndpointAllowedHosts[0] != "tools.example.com" {
		t.Fatalf("allowed hosts = %v, want the granted net: scope", connection.EndpointAllowedHosts)
	}
	// A plugin's MCP server going down must not fail somebody's task.
	if connection.FailurePolicy != "optional" {
		t.Fatalf("failure policy = %q, want optional", connection.FailurePolicy)
	}
	// The daemon asks for the credential at dial time using this id, so it has
	// to name both halves or the server cannot tell which hook it means.
	want := remotemcp.PluginContributionPrefix + uuidString(mcpInstallation(t, "{}").ID) + ":toolbox"
	if connection.ContributionID != want {
		t.Fatalf("contribution id = %q, want %q", connection.ContributionID, want)
	}
}

// An approved hook carries its pinned set, and the digest travels with it —
// the broker's validatePinnedRemoteMCPTools compares against exactly this.
func TestApprovedMCPToolsArePinnedByDigest(t *testing.T) {
	approvals, err := json.Marshal(PluginMCPApprovals{
		"toolbox": {Tools: []remotemcp.Tool{
			{Name: "search", SchemaDigest: "sha256:aaa"},
			{Name: "fetch", SchemaDigest: "sha256:bbb"},
		}},
	})
	if err != nil {
		t.Fatalf("marshal approvals: %v", err)
	}
	installation := mcpInstallation(t, string(approvals))

	pinned := (&PluginService{}).ApprovedMCPTools(installation, "toolbox")
	if len(pinned) != 2 {
		t.Fatalf("pinned = %v, want two tools", pinned)
	}
	if pinned["search"].SchemaDigest != "sha256:aaa" {
		t.Fatalf("the digest must survive storage, got %q", pinned["search"].SchemaDigest)
	}

	// A name-only pin would let a server keep a tool's name and change its
	// arguments without the agent noticing, which is what the digest prevents.
	drifted := []remotemcp.Tool{{Name: "search", SchemaDigest: "sha256:CHANGED"}}
	if err := validatePinnedAgainst(pinned, drifted); err == nil {
		t.Fatal("a drifted schema must not pass the pin")
	}
}

// validatePinnedAgainst mirrors the daemon's broker check closely enough to
// assert the property here; the broker's own copy is what runs in production.
func validatePinnedAgainst(pinned map[string]remotemcp.Tool, discovered []remotemcp.Tool) error {
	available := map[string]remotemcp.Tool{}
	for _, tool := range discovered {
		available[tool.Name] = tool
	}
	for name, tool := range pinned {
		current, ok := available[name]
		if !ok {
			continue
		}
		if current.SchemaDigest != tool.SchemaDigest {
			return errSchemaDrift
		}
	}
	return nil
}

var errSchemaDrift = &PluginError{Kind: PluginErrorConflict, Message: "schema drifted"}

// A hook that did not declare the agent trigger is not adopted even once its
// tools are approved: approval says which tools, the trigger says whether an
// agent may reach them at all.
func TestMCPConnectionRequiresTheAgentTrigger(t *testing.T) {
	manifest := `{
		"manifest_version": 1,
		"key": "com.example.mcp2",
		"name": "MCP Plugin",
		"description": "d",
		"version": "1.0.0",
		"author": {"name": "example"},
		"scopes": ["net:tools.example.com"],
		"contributes": {"hooks": [{
			"key": "toolbox", "name": "Toolbox", "description": "Not for agents.",
			"triggers": ["manual"],
			"transport": {"type": "mcp", "url": "https://tools.example.com/mcp"}
		}]}
	}`
	installation := db.PluginInstallation{
		ID: testInstallationID(t), Enabled: true,
		Manifest:      []byte(manifest),
		GrantedScopes: []byte(`["net:tools.example.com"]`),
	}
	parsed, err := ParseInstallationManifest(installation)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if HookAllowsTrigger(parsed.Contributes.Hooks[0], "agent") {
		t.Fatal("a manual-only hook must not be reachable by an agent")
	}

	// Even with its tools approved, it is not offered to an agent.
	installation.McpApprovals = []byte(`{"toolbox":{"tools":[{"name":"search"}]}}`)
	if connections := mcpConnectionsFor(installation); len(connections) != 0 {
		t.Fatalf("a manual-only hook produced %d connections, want none", len(connections))
	}
}
