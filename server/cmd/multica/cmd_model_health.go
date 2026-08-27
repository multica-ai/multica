package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var modelHealthCmd = &cobra.Command{
	Use:   "model-health",
	Short: "Manage model health (global + per-workspace)",
}

var modelHealthGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get model health records",
	RunE:  runModelHealthGet,
}

var modelHealthSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set model health status",
	RunE:  runModelHealthSet,
}

func init() {
	modelHealthCmd.AddCommand(modelHealthGetCmd)
	modelHealthCmd.AddCommand(modelHealthSetCmd)

	modelHealthGetCmd.Flags().Bool("global", false, "Global records (omit workspace filter)")
	modelHealthGetCmd.Flags().String("workspace", "", "Workspace ID (defaults to current if neither --global nor --workspace)")

	modelHealthSetCmd.Flags().String("workspace", "", "Workspace ID (defaults to current)")
	modelHealthSetCmd.Flags().String("model", "", "Concrete model name (required)")
	modelHealthSetCmd.Flags().String("status", "", "Status: healthy|unhealthy (required)")
	modelHealthSetCmd.Flags().String("reason", "", "Optional reason for unhealthy status")
	_ = modelHealthSetCmd.MarkFlagRequired("model")
	_ = modelHealthSetCmd.MarkFlagRequired("status")
}

func runModelHealthGet(cmd *cobra.Command, _ []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}
	client := cli.NewAPIClient(serverURL, "", token)
	isGlobal, _ := cmd.Flags().GetBool("global")
	workspace, _ := cmd.Flags().GetString("workspace")

	path := "/api/model-health"
	wsID := workspace
	if !isGlobal && wsID == "" {
		wsID = resolveWorkspaceID(cmd)
	}
	if wsID != "" {
		path = "/api/model-health?workspace=" + url.QueryEscape(wsID)
	}
	var out any
	if err := client.GetJSON(ctx, path, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runModelHealthSet(cmd *cobra.Command, _ []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}
	client := cli.NewAPIClient(serverURL, "", token)
	workspace, _ := cmd.Flags().GetString("workspace")
	model, _ := cmd.Flags().GetString("model")
	status, _ := cmd.Flags().GetString("status")
	reason, _ := cmd.Flags().GetString("reason")

	wsID := workspace
	if wsID == "" {
		wsID = resolveWorkspaceID(cmd)
	}
	body := map[string]any{
		"workspace_id":   wsID,
		"concrete_model": model,
		"status":         status,
	}
	if reason != "" {
		body["reason"] = reason
	}
	var out any
	if err := client.PutJSON(ctx, "/api/model-health", body, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}
