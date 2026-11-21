package main

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var projectMoveCmd = &cobra.Command{Use: "move <project>", Short: "Move a project to another workspace", Args: cobra.ExactArgs(1), RunE: runProjectMove}

func registerProjectMoveFlags(cmd *cobra.Command) {
	cmd.Flags().String("to", "", "Target workspace slug or ID")
	cmd.Flags().Bool("preview", false, "Print the plan without moving")
	cmd.Flags().Bool("yes", false, "Confirm the preview and move")
	cmd.Flags().String("output", "json", "Output format: json")
}
func init() {
	projectCmd.AddCommand(projectMoveCmd)
	registerProjectMoveFlags(projectMoveCmd)
}

func runProjectMove(cmd *cobra.Command, args []string) error {
	if err := requireHumanLocalCommand("project move"); err != nil {
		return err
	}
	to, _ := cmd.Flags().GetString("to")
	if to == "" {
		return fmt.Errorf("--to is required")
	}
	output, _ := cmd.Flags().GetString("output")
	if output != "json" {
		return fmt.Errorf("--output must be json")
	}
	target, err := resolveWorkspaceArg(cmd, []string{to})
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return err
	}
	path := "/api/projects/" + project.ID + "/move"
	var plan map[string]any
	if err := client.PostJSON(ctx, path+"/preview", map[string]any{"target_workspace_id": target}, &plan); err != nil {
		return fmt.Errorf("preview project move: %w", err)
	}
	preview, _ := cmd.Flags().GetBool("preview")
	yes, _ := cmd.Flags().GetBool("yes")
	if preview || !yes {
		if err := cli.PrintJSON(cmd.OutOrStdout(), plan); err != nil {
			return err
		}
		if preview {
			return nil
		}
		return fmt.Errorf("review the plan and repeat with --yes to move the project")
	}
	hash, ok := plan["plan_hash"].(string)
	if !ok || len(hash) != 64 {
		return fmt.Errorf("invalid move plan: missing plan_hash")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("invalid move plan: invalid plan_hash")
	}
	blockers, ok := plan["blockers"].([]any)
	if !ok {
		return fmt.Errorf("invalid move plan: missing blockers")
	}
	if len(blockers) > 0 {
		if err := cli.PrintJSON(cmd.OutOrStdout(), plan); err != nil {
			return err
		}
		return fmt.Errorf("project move has blockers; resolve them before confirming")
	}
	var moved map[string]any
	if err := client.PostJSON(ctx, path, map[string]any{"target_workspace_id": target, "plan_hash": hash}, &moved); err != nil {
		return fmt.Errorf("move project: %w", err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), moved)
}
