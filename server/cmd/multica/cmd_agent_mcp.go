package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Assigning a workspace MCP server to an agent, mirroring `agent skills`.

var agentMcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage which workspace MCP servers an agent uses",
	Long: "Assigns MCP servers from the workspace library to an agent. A library " +
		"entry does nothing until it is added to an agent here, and each " +
		"assignment carries its own on/off toggle — the same shape as workspace " +
		"skills.\n\n" +
		"This is separate from the agent's OWN mcp_config ('agent update " +
		"--mcp-config'), which holds servers private to that agent. Both are " +
		"merged at claim time; the agent's own entry wins on a name collision.",
}

var agentMcpListCmd = &cobra.Command{
	Use:   "list <agent-id>",
	Short: "List the workspace MCP servers assigned to an agent",
	Args:  exactArgs(1),
	RunE:  runAgentMcpList,
}

var agentMcpAddCmd = &cobra.Command{
	Use:   "add <agent-id> <server-id>",
	Short: "Give a workspace MCP server to an agent",
	Long: "Assigns a workspace MCP server to this agent, enabled. Take the server " +
		"id from 'multica workspace mcp list'. Adding one twice is a no-op.",
	Args: exactArgs(2),
	RunE: runAgentMcpAdd,
}

var agentMcpEnableCmd = &cobra.Command{
	Use:   "enable <agent-id> <server-id>",
	Short: "Turn an assigned MCP server back on for this agent",
	Args:  exactArgs(2),
	RunE:  runAgentMcpEnable,
}

var agentMcpDisableCmd = &cobra.Command{
	Use:   "disable <agent-id> <server-id>",
	Short: "Turn an assigned MCP server off for this agent",
	Long: "Stops the agent from receiving this server without dropping the " +
		"assignment, so turning it back on later is one command.",
	Args: exactArgs(2),
	RunE: runAgentMcpDisable,
}

var agentMcpRemoveCmd = &cobra.Command{
	Use:   "remove <agent-id> <server-id>",
	Short: "Take a workspace MCP server away from an agent",
	Long: "Removes the assignment. The workspace library entry itself is " +
		"untouched and other agents keep theirs.",
	Args: exactArgs(2),
	RunE: runAgentMcpRemove,
}

func init() {
	agentCmd.AddCommand(agentMcpCmd)
	agentMcpCmd.AddCommand(agentMcpListCmd)
	agentMcpCmd.AddCommand(agentMcpAddCmd)
	agentMcpCmd.AddCommand(agentMcpEnableCmd)
	agentMcpCmd.AddCommand(agentMcpDisableCmd)
	agentMcpCmd.AddCommand(agentMcpRemoveCmd)

	for _, cmd := range []*cobra.Command{
		agentMcpListCmd, agentMcpAddCmd, agentMcpEnableCmd, agentMcpDisableCmd, agentMcpRemoveCmd,
	} {
		cmd.Flags().String("output", "table", "Output format: table or json")
	}
}

func agentMcpPath(agentID string, parts ...string) string {
	path := "/api/agents/" + url.PathEscape(agentID) + "/mcp-servers"
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	return path
}

func runAgentMcpList(cmd *cobra.Command, args []string) error {
	agentID := strings.TrimSpace(args[0])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	if err := client.GetJSON(ctx, agentMcpPath(agentID), &servers); err != nil {
		return fmt.Errorf("list agent mcp servers: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}

func runAgentMcpAdd(cmd *cobra.Command, args []string) error {
	agentID, serverID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	body := map[string]any{"server_id": serverID}
	if err := client.PostJSON(ctx, agentMcpPath(agentID), body, &servers); err != nil {
		return fmt.Errorf("add agent mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}

func runAgentMcpEnable(cmd *cobra.Command, args []string) error {
	return setAgentMcpEnabled(cmd, args, true)
}

func runAgentMcpDisable(cmd *cobra.Command, args []string) error {
	return setAgentMcpEnabled(cmd, args, false)
}

func setAgentMcpEnabled(cmd *cobra.Command, args []string, enabled bool) error {
	agentID, serverID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	body := map[string]any{"enabled": enabled}
	if err := client.PutJSON(ctx, agentMcpPath(agentID, serverID, "enabled"), body, &servers); err != nil {
		return fmt.Errorf("update agent mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}

func runAgentMcpRemove(cmd *cobra.Command, args []string) error {
	agentID, serverID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var servers []workspaceMcpServer
	if err := client.DeleteJSONResponse(ctx, agentMcpPath(agentID, serverID), &servers); err != nil {
		return fmt.Errorf("remove agent mcp server: %w", err)
	}
	return printWorkspaceMcpServers(cmd, servers)
}
