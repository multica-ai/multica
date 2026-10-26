package main

import (
	"context"
	"fmt"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
	"os"
)

func init() {
	command := &cobra.Command{
		Use:   "bind-aido-owner <agent-id>",
		Short: "Bind a dedicated AIDO agent/runtime to minimum task-token API capabilities",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeID, _ := cmd.Flags().GetString("runtime-id")
			if runtimeID == "" {
				return fmt.Errorf("--runtime-id is required")
			}
			client, err := newAPIClient(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := cli.APIContext(context.Background())
			defer cancel()
			var result map[string]any
			if err := client.PostJSON(ctx, "/api/agents/"+args[0]+"/aido-owner-binding", map[string]string{"runtime_id": runtimeID}, &result); err != nil {
				return err
			}
			return cli.PrintJSON(os.Stdout, result)
		},
	}
	command.Flags().String("runtime-id", "", "Dedicated runtime UUID (agent must already be assigned to it)")
	agentCmd.AddCommand(command)
}
