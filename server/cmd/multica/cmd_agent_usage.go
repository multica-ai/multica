// CEREBRO-PATCH(tech-3255-agent-usage): multica agent usage — token/cost per agent.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var agentUsageCmd = &cobra.Command{
	Use:   "usage [agent-id]",
	Short: "Get token and cost usage per agent for the workspace",
	Long: `Get aggregated token and cost usage for all agents (or a single agent).

Uses the workspace dashboard endpoint. Optional agent-id filters to one agent.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentUsage,
}

func init() {
	agentCmd.AddCommand(agentUsageCmd) // CEREBRO-PATCH(tech-3255-agent-usage)

	agentUsageCmd.Flags().String("output", "table", "Output format: table or json")
	agentUsageCmd.Flags().Int("days", 30, "Number of days to include (1–365, default 30)")
}

func runAgentUsage(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	days, _ := cmd.Flags().GetInt("days")
	if days < 1 || days > 365 {
		return fmt.Errorf("--days must be between 1 and 365")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := fmt.Sprintf("/api/dashboard/usage/by-agent?days=%d", days)
	var rows []map[string]any
	if err := client.GetJSON(ctx, path, &rows); err != nil {
		return fmt.Errorf("get agent usage: %w", err)
	}

	// Optional client-side filter by agent-id.
	if len(args) == 1 {
		agentID := args[0]
		filtered := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			if strVal(r, "agent_id") == agentID {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}

	headers := []string{"AGENT_ID", "MODEL", "INPUT_TOKENS", "OUTPUT_TOKENS", "CACHE_READ", "CACHE_WRITE", "COST_CENTS", "TASKS"}
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			displayID(strVal(r, "agent_id"), false),
			strVal(r, "model"),
			fmt.Sprintf("%v", r["input_tokens"]),
			fmt.Sprintf("%v", r["output_tokens"]),
			fmt.Sprintf("%v", r["cache_read_tokens"]),
			fmt.Sprintf("%v", r["cache_write_tokens"]),
			fmt.Sprintf("%v", r["cost_cents"]),
			fmt.Sprintf("%v", r["task_count"]),
		})
	}
	cli.PrintTable(os.Stdout, headers, tableRows)
	return nil
}
