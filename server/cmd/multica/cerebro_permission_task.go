// CEREBRO-PATCH(cerebro-permission-task-cli): FIR-3388 exposes one task's
// immutable access snapshot through the unified permissions command.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

type permissionTaskAccess struct {
	TaskID       string   `json:"task_id"`
	AgentID      string   `json:"agent_id"`
	AllowedTools []string `json:"allowed_tools"`
	IssuedAt     string   `json:"issued_at"`
	ExpiresAt    string   `json:"expires_at"`
	Status       string   `json:"status"`
}

var permissionTaskCmd = &cobra.Command{
	Use:   "task <id>",
	Short: "Show the immutable permission snapshot for one task",
	Long: "Shows the exact tool allowlist locked when a task started. The running agent's " +
		"tool calls are checked against this same snapshot.",
	Args: exactArgs(1),
	RunE: runPermissionTask,
}

func init() {
	permissionTaskCmd.Flags().String("output", "table", "Output format: table or json")
	permissionsCmd.AddCommand(permissionTaskCmd)
}

func runPermissionTask(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return showPermissionTask(ctx, client, args[0], output)
}

func showPermissionTask(ctx context.Context, client *cli.APIClient, taskID, output string) error {
	var access permissionTaskAccess
	if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(taskID)+"/access", &access); err != nil {
		return fmt.Errorf("show task access: %w", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, access)
	}
	rows := make([][]string, 0, len(access.AllowedTools))
	for _, tool := range access.AllowedTools {
		rows = append(rows, []string{tool, access.Status, access.IssuedAt, access.ExpiresAt})
	}
	cli.PrintTable(os.Stdout, []string{"ALLOWED TOOL", "WINDOW", "ISSUED", "EXPIRES"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "no tools were allowed for this task")
	}
	return nil
}
