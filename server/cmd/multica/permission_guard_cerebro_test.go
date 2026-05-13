// CEREBRO-PATCH(permguard-mcp-cli-test): JEH-1171 regression guard. Walks
// the cobra command tree and the registered MCP tool list and diffs both
// against server/internal/cerebro/permguard/inventory.json. See the
// permguard README for the exemption mechanism.
package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cerebro/permguard"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestPermissionGuard_MCPToolsInventoried(t *testing.T) {
	srv := mcp.NewServer("permguard-test", "0")
	// registerTools wires every tool the real `multica mcp serve` exposes.
	// The arguments are placeholders — the test never invokes a handler,
	// only enumerates registered tool names.
	client := cli.NewAPIClient("http://invalid", "", "")
	var state mcpSessionState
	registerTools(srv, client, &state, "", "", "")

	var ids []string
	for _, tool := range srv.Tools() {
		ids = append(ids, tool.Name)
	}
	sort.Strings(ids)

	inv, err := permguard.Load()
	if err != nil {
		t.Fatalf("permguard.Load: %v", err)
	}
	res := inv.Diff(permguard.SurfaceMCP, ids)
	if res.Clean() {
		return
	}
	path, _ := permguard.InventoryPath()
	t.Fatalf("\n%s\nInventory: %s", res.Report(permguard.SurfaceMCP), path)
}

func TestPermissionGuard_CLICommandsInventoried(t *testing.T) {
	var ids []string
	collectLeafCommands(rootCmd, "", &ids)
	sort.Strings(ids)

	inv, err := permguard.Load()
	if err != nil {
		t.Fatalf("permguard.Load: %v", err)
	}
	res := inv.Diff(permguard.SurfaceCLI, ids)
	if res.Clean() {
		return
	}
	path, _ := permguard.InventoryPath()
	t.Fatalf("\n%s\nInventory: %s", res.Report(permguard.SurfaceCLI), path)
}

// collectLeafCommands walks the cobra tree depth-first and emits a stable
// identifier for every leaf command (no further sub-commands). The
// identifier is the space-separated path from the root: "issue comment add",
// "agent skills set", etc. Hidden and deprecated commands are skipped — they
// are not part of the supported surface.
func collectLeafCommands(c *cobra.Command, prefix string, out *[]string) {
	if c.Hidden || c.Deprecated != "" {
		return
	}
	name := c.Name()
	path := name
	if prefix != "" {
		path = prefix + " " + name
	}
	subs := c.Commands()
	hasLeaf := false
	for _, sub := range subs {
		if sub.Hidden || sub.Deprecated != "" {
			continue
		}
		// Skip cobra's built-ins ("help", "completion") — they are not part
		// of our surface and have no permission semantics.
		if sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		hasLeaf = true
		collectLeafCommands(sub, path, out)
	}
	if !hasLeaf && c != rootCmd {
		// Trim the "multica " prefix so identifiers match the way users type
		// commands: "multica issue create" -> "issue create".
		id := strings.TrimPrefix(path, rootCmd.Name()+" ")
		*out = append(*out, id)
	}
}
