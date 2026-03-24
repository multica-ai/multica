package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents in the workspace",
	RunE:  runAgentList,
}

var agentGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get agent details",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentGet,
}

var agentDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentDelete,
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <id>",
	Short: "Stop an agent (set status to offline)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentStop,
}

func init() {
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentGetCmd)
	agentCmd.AddCommand(agentDeleteCmd)
	agentCmd.AddCommand(agentStopCmd)

	agentListCmd.Flags().String("output", "table", "Output format: table or json")
	agentGetCmd.Flags().String("output", "json", "Output format: table or json")
}

func newAPIClient(cmd *cobra.Command) (*cli.APIClient, error) {
	serverURL := resolveServerURL(cmd)
	workspaceID := resolveWorkspaceID(cmd)

	if serverURL == "" {
		return nil, fmt.Errorf("server URL not set: use --server-url flag, MULTICA_SERVER_URL env, or 'multica-cli config set server_url <url>'")
	}

	return cli.NewAPIClient(serverURL, workspaceID), nil
}

func resolveServerURL(cmd *cobra.Command) string {
	val := cli.FlagOrEnv(cmd, "server-url", "MULTICA_SERVER_URL", "")
	if val != "" {
		return val
	}
	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		return "http://localhost:8080"
	}
	if cfg.ServerURL != "" {
		return cfg.ServerURL
	}
	return "http://localhost:8080"
}

func resolveWorkspaceID(cmd *cobra.Command) string {
	val := cli.FlagOrEnv(cmd, "workspace-id", "MULTICA_WORKSPACE_ID", "")
	if val != "" {
		return val
	}
	cfg, _ := cli.LoadCLIConfig()
	if cfg.WorkspaceID != "" {
		return cfg.WorkspaceID
	}
	// Fallback: try daemon.json for workspace_id
	return cli.LoadWorkspaceIDFromDaemonConfig()
}

func runAgentList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var agents []map[string]any
	path := "/api/agents"
	if client.WorkspaceID != "" {
		path += "?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	}
	if err := client.GetJSON(ctx, path, &agents); err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, agents)
	}

	headers := []string{"ID", "NAME", "STATUS", "RUNTIME"}
	rows := make([][]string, 0, len(agents))
	for _, a := range agents {
		rows = append(rows, []string{
			strVal(a, "id"),
			strVal(a, "name"),
			strVal(a, "status"),
			strVal(a, "runtime_mode"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var agent map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0], &agent); err != nil {
		return fmt.Errorf("get agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "RUNTIME", "DESCRIPTION"}
		rows := [][]string{{
			strVal(agent, "id"),
			strVal(agent, "name"),
			strVal(agent, "status"),
			strVal(agent, "runtime_mode"),
			strVal(agent, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, agent)
}

func runAgentDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/agents/"+args[0]); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Agent %s deleted.\n", args[0])
	return nil
}

func runAgentStop(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"status": "offline"}
	if err := client.PutJSON(ctx, "/api/agents/"+args[0], body, nil); err != nil {
		return fmt.Errorf("stop agent: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Agent %s stopped.\n", args[0])
	return nil
}

func strVal(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
