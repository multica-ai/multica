package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// supportedMCPClients is the set of clients we know how to register with.
// Each value is also the argument users pass to `--client`.
var supportedMCPClients = []string{"claude-code"}

var mcpInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register the Multica MCP server with a local AI client",
	Long: `Registers 'multica mcp serve' as a stdio MCP server with a local
AI client so the client can use Multica tools without manual config edits.

Currently supports Claude Code (claude CLI). The default scope is 'user', meaning
the server is available across all projects on this machine.

Examples:
  multica mcp install                      # register with Claude Code (user scope)
  multica mcp install --scope project      # only the current project
  multica mcp install --name cerebro       # register under a custom name
  multica mcp install --client claude-code # explicit client (default)`,
	RunE: runMCPInstall,
}

func init() {
	mcpInstallCmd.Flags().String("client", "claude-code", "AI client to register with (claude-code)")
	mcpInstallCmd.Flags().String("name", "multica", "Name to register the MCP server under")
	mcpInstallCmd.Flags().String("scope", "user", "Claude Code scope: user, project, or local")
	mcpCmd.AddCommand(mcpInstallCmd)
}

func runMCPInstall(cmd *cobra.Command, _ []string) error {
	client, _ := cmd.Flags().GetString("client")
	name, _ := cmd.Flags().GetString("name")
	scope, _ := cmd.Flags().GetString("scope")

	if !contains(supportedMCPClients, client) {
		return fmt.Errorf("unsupported client %q. Supported: %s", client, strings.Join(supportedMCPClients, ", "))
	}

	multicaPath, err := exec.LookPath("multica")
	if err != nil {
		return fmt.Errorf("'multica' not found on PATH. The MCP install command must call back into the same binary; install or symlink the CLI before running this")
	}

	switch client {
	case "claude-code":
		return installForClaudeCode(name, scope, multicaPath)
	}
	return fmt.Errorf("client %q is listed as supported but has no installer", client)
}

func installForClaudeCode(name, scope, multicaPath string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("'claude' CLI not found on PATH. Install Claude Code from https://claude.com/claude-code, then re-run this command")
	}

	if scope != "user" && scope != "project" && scope != "local" {
		return fmt.Errorf("invalid scope %q. Must be one of: user, project, local", scope)
	}

	// `claude mcp add <name> --scope <scope> -- <cmd> <args...>` — the `--`
	// separator tells Claude Code that everything after is the server command.
	args := []string{"mcp", "add", name, "--scope", scope, "--", multicaPath, "mcp", "serve"}
	c := exec.Command("claude", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("claude mcp add failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\n✓ Registered %q with Claude Code (scope: %s)\n", name, scope)
	fmt.Fprintf(os.Stderr, "  Restart Claude Code if it's already running, then check '/mcp' to verify.\n")
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
