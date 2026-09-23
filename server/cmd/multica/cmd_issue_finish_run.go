package main

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var issueFinishRunCmd = &cobra.Command{
	Use:   "finish-run",
	Short: "Request completion of your current interactive run after its final reply",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(client.Token, "mat_") {
			return fmt.Errorf("finish-run requires the current agent's task-scoped token")
		}
		if _, err := uuid.Parse(client.TaskID); err != nil {
			return fmt.Errorf("finish-run requires MULTICA_TASK_ID from the current agent run")
		}
		ctx, cancel := cli.APIContext(cmd.Context())
		defer cancel()
		var response struct {
			Requested bool `json:"requested"`
		}
		if err := client.PostJSON(ctx, "/api/tasks/"+client.TaskID+"/complete", map[string]any{}, &response); err != nil {
			return err
		}
		if !response.Requested {
			return fmt.Errorf("run completion was not confirmed")
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Completion requested. Deliver your final reply and yield normally. New human input or interruption cancels this request. This does not change the issue status.")
		return nil
	},
}

func init() { issueCmd.AddCommand(issueFinishRunCmd) }
