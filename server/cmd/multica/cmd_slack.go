package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var slackCmd = &cobra.Command{
	Use:   "slack",
	Short: "Slack Lists helpers for the current agent task",
}

var slackListsCmd = &cobra.Command{
	Use:   "lists",
	Short: "Read and write allowlisted Slack Lists",
}

var slackListsSchemaCmd = &cobra.Command{
	Use:   "schema <list-id>",
	Short: "Read a Slack List schema (column ids, types, select options)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSlackListsSchema,
}

var slackListsCreateCmd = &cobra.Command{
	Use:   "create <list-id>",
	Short: "Create a Slack List item",
	Args:  cobra.ExactArgs(1),
	RunE:  runSlackListsCreate,
}

var slackListsUpdateCmd = &cobra.Command{
	Use:   "update <list-id> <item-id>",
	Short: "Update a Slack List item",
	Args:  cobra.ExactArgs(2),
	RunE:  runSlackListsUpdate,
}

func init() {
	slackListsCreateCmd.Flags().String("json", "", "JSON object of column name/key/id → value")
	slackListsUpdateCmd.Flags().String("json", "", "JSON object of column name/key/id → value")
	_ = slackListsCreateCmd.MarkFlagRequired("json")
	_ = slackListsUpdateCmd.MarkFlagRequired("json")
	slackListsCmd.AddCommand(slackListsSchemaCmd, slackListsCreateCmd, slackListsUpdateCmd)
	slackCmd.AddCommand(slackListsCmd)
}

func runSlackListsSchema(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/slack/lists/"+args[0]+"/schema", &resp); err != nil {
		return fmt.Errorf("read slack list schema: %w", err)
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func runSlackListsCreate(cmd *cobra.Command, args []string) error {
	return runSlackListsWrite(cmd, "/api/slack/lists/"+args[0]+"/items", false)
}

func runSlackListsUpdate(cmd *cobra.Command, args []string) error {
	return runSlackListsWrite(cmd, "/api/slack/lists/"+args[0]+"/items/"+args[1], true)
}

func runSlackListsWrite(cmd *cobra.Command, path string, update bool) error {
	raw, err := cmd.Flags().GetString("json")
	if err != nil {
		return err
	}
	var fields any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return fmt.Errorf("invalid --json: %w", err)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	body := map[string]any{"fields": fields}
	var resp map[string]any
	if update {
		if err := client.PatchJSON(ctx, path, body, &resp); err != nil {
			return fmt.Errorf("update slack list item: %w", err)
		}
	} else if err := client.PostJSON(ctx, path, body, &resp); err != nil {
		return fmt.Errorf("create slack list item: %w", err)
	}
	return cli.PrintJSON(os.Stdout, resp)
}
