package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// mcp-stdio is the in-binary MCP server. Replaces the standalone Node
// @multica/mcp bundle. Auth + workspace + server URL come from the same
// CLI config the rest of the binary uses (--server-url, --workspace-id,
// --token flags or MULTICA_* env vars or `multica config` defaults), so a
// user who can already run `multica issue list` can also run
// `multica mcp-stdio` with no extra setup.
//
// Why a single subcommand and not a daemon: MCP clients (Claude Code,
// Claude Desktop, Cursor, Windsurf) launch the server as a child process
// over stdio. The lifecycle is per-conversation. Running it as a long-
// lived daemon would just add a forwarding layer and complicate auth
// rotation.

var mcpCmd = &cobra.Command{
	Use:   "mcp-stdio",
	Short: "Run the Multica MCP server over stdio",
	Long: `Run an MCP (Model Context Protocol) server that exposes the active
workspace's resources to MCP-aware AI clients.

The server speaks the standard MCP stdio transport and is meant to be
launched by an MCP client (Claude Code, Claude Desktop, Cursor, Windsurf,
etc.) rather than invoked directly. Configure your client to run:

  multica mcp-stdio

with MULTICA_SERVER_URL / MULTICA_WORKSPACE_ID / MULTICA_TOKEN env vars
set, or with the equivalent ` + "`multica config`" + ` defaults in place.

Authentication and workspace selection follow the same precedence as
every other ` + "`multica`" + ` command: --flags > env vars > config file.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runMCPStdio,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCPStdio(cmd *cobra.Command, _ []string) error {
	apiClient, err := newAPIClient(cmd)
	if err != nil {
		return fmt.Errorf("mcp: build api client: %w", err)
	}

	// Capabilities: tools only for v1. Resources, prompts, and sampling
	// are intentionally out of scope until the tool surface settles —
	// shipping with capabilities you don't fully implement is worse than
	// not advertising them, because clients will probe and silently fail.
	srv := server.NewMCPServer(
		"multica",
		ClientVersion(),
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	// Tool registry lives in cmd_mcp_tools.go. Keep order stable across
	// releases so the picker shows tools in the same place every
	// conversation; new tools should be added at the end of their
	// resource group, not in the middle.
	RegisterAllMCPTools(srv, apiClient)

	// stdio transport — the MCP client owns the process lifecycle. Errors
	// from ServeStdio are returned so the parent (Claude Code etc.) can
	// surface them in its UI.
	if err := server.ServeStdio(srv); err != nil {
		return fmt.Errorf("mcp: serve stdio: %w", err)
	}
	return nil
}

// ClientVersion exposes the build-time CLI version through the same getter
// the rest of the package uses, so the MCP server reports whatever
// `multica --version` reports — no separate version drift.
func ClientVersion() string {
	if cli.ClientVersion != "" {
		return cli.ClientVersion
	}
	return "dev"
}

// ---------------------------------------------------------------------------
// Tool handler adapter
// ---------------------------------------------------------------------------

// toolHandler wraps a typical "call REST, return JSON" handler in the
// shape mcp-go expects. Tools return their raw JSON payload as the only
// content block — the model gets to parse it. For tools that need to
// return formatted text instead of JSON, wrap the result with mcp.NewToolResultText
// at the call site.
func toolHandler(
	fn func(ctx context.Context, req mcp.CallToolRequest) (any, error),
) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := fn(ctx, req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// json.RawMessage round-trips cleanly; everything else gets
		// re-marshalled. This keeps the wire shape consistent across
		// tools that pass through API responses verbatim and tools
		// that build their own structured output.
		var payload []byte
		switch v := result.(type) {
		case json.RawMessage:
			payload = v
		case []byte:
			payload = v
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
			}
			payload = b
		}
		return mcp.NewToolResultText(string(payload)), nil
	}
}
