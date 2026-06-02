package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dwickyfp/wallts/server/internal/cli"
)

var concurrencyCmd = &cobra.Command{
	Use:   "concurrency",
	Short: "View concurrency status and capacity utilization",
}

var concurrencyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show real-time concurrency metrics for the workspace",
	RunE:  runConcurrencyStatus,
}

func init() {
	concurrencyCmd.AddCommand(concurrencyStatusCmd)

	concurrencyStatusCmd.Flags().String("output", "table", "Output format: table or json")
	concurrencyStatusCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
}

func runConcurrencyStatus(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp struct {
		WorkspaceID    string `json:"workspace_id"`
		ActiveCount    int    `json:"active_count"`
		QueuedCount    int    `json:"queued_count"`
		CompletedLastH int    `json:"completed_last_hour"`
		FailedLastH    int    `json:"failed_last_hour"`
		AgentDetails   []struct {
			AgentID            string `json:"agent_id"`
			AgentName          string `json:"agent_name"`
			MaxConcurrentTasks int    `json:"max_concurrent_tasks"`
			RunningCount       int    `json:"running_count"`
			QueuedCount        int    `json:"queued_count"`
			AtCapacity         bool   `json:"at_capacity"`
		} `json:"agent_details"`
	}

	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID+"/concurrency", &resp); err != nil {
		return fmt.Errorf("get concurrency stats: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")

	// Workspace summary
	fmt.Printf("Workspace: %s\n", resp.WorkspaceID)
	fmt.Printf("─────────────────────────────────\n")
	fmt.Printf("  Active (running+dispatched): %d\n", resp.ActiveCount)
	fmt.Printf("  Queued:                      %d\n", resp.QueuedCount)
	fmt.Printf("  Completed (last hour):       %d\n", resp.CompletedLastH)
	fmt.Printf("  Failed (last hour):          %d\n", resp.FailedLastH)
	fmt.Printf("─────────────────────────────────\n\n")

	// Per-agent breakdown
	if len(resp.AgentDetails) > 0 {
		headers := []string{"AGENT", "RUNNING", "MAX", "QUEUED", "STATUS"}
		rows := make([][]string, 0, len(resp.AgentDetails))
		for _, a := range resp.AgentDetails {
			status := "OK"
			if a.AtCapacity {
				status = "AT CAPACITY"
			}
			agentID := a.AgentID
			if !fullID && len(agentID) > 8 {
				agentID = agentID[:8]
			}
			rows = append(rows, []string{
				fmt.Sprintf("%s (%s)", a.AgentName, agentID),
				fmt.Sprintf("%d", a.RunningCount),
				fmt.Sprintf("%d", a.MaxConcurrentTasks),
				fmt.Sprintf("%d", a.QueuedCount),
				status,
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
	} else {
		fmt.Println("No agents configured in this workspace.")
	}

	return nil
}
