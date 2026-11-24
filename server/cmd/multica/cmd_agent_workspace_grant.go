package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func init() {
	grants := &cobra.Command{Use: "workspace-grant", Short: "Manage workspaces this agent may access while running"}
	grants.AddCommand(
		&cobra.Command{Use: "list <agent-id>", Short: "List an agent's additional workspaces", Args: exactArgs(1), RunE: runAgentWorkspaceGrantList},
		&cobra.Command{Use: "add <agent-id> <target-workspace-id>", Short: "Grant access to a workspace", Args: exactArgs(2), RunE: runAgentWorkspaceGrantAdd},
		&cobra.Command{Use: "remove <agent-id> <target-workspace-id>", Short: "Revoke access to a workspace", Args: exactArgs(2), RunE: runAgentWorkspaceGrantRemove},
		&cobra.Command{Use: "agent-creation <agent-id> <target-workspace-id> <allow|deny>", Short: "Set the separate agent creation capability", Args: exactArgs(3), RunE: runAgentWorkspaceGrantAgentCreation},
	)
	agentCmd.AddCommand(grants)
}

func runAgentWorkspaceGrantAgentCreation(cmd *cobra.Command, args []string) error {
	if args[2] != "allow" && args[2] != "deny" {
		return fmt.Errorf("expected allow or deny")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.PutJSON(ctx, agentGrantPath(args)+"/agent-creation", map[string]any{"can_create_agents": args[2] == "allow"}, &result); err != nil {
		return fmt.Errorf("set agent creation grant: %w", err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func agentGrantPath(args []string) string {
	path := "/api/agents/" + url.PathEscape(args[0]) + "/workspace-grants"
	if len(args) > 1 {
		path += "/" + url.PathEscape(args[1])
	}
	return path
}

func runAgentWorkspaceGrantList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var grants map[string]any
	if err := client.GetJSON(ctx, agentGrantPath(args), &grants); err != nil {
		return fmt.Errorf("list agent workspace grants: %w", err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), grants)
}

func runAgentWorkspaceGrantAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var grant map[string]any
	if err := client.PutJSON(ctx, agentGrantPath(args), map[string]any{}, &grant); err != nil {
		return fmt.Errorf("grant agent workspace access: %w", err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), grant)
}

func runAgentWorkspaceGrantRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	if err := client.DeleteJSON(ctx, agentGrantPath(args)); err != nil {
		return fmt.Errorf("revoke agent workspace access: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Workspace grant removed.")
	return nil
}
