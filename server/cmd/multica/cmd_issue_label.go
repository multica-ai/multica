package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue label {list|add|remove} — manages the labels attached to a
// specific issue. The label itself is managed via `multica label ...`.

var issueLabelCmd = &cobra.Command{
	Use:   "label",
	Short: "Manage labels on an issue",
}

var issueLabelListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List labels on an issue",
	Args:  exactArgs(1),
	RunE:  runIssueLabelList,
}

var issueLabelAddCmd = &cobra.Command{
	Use:   "add <issue-id> <label-id>",
	Short: "Attach a label to an issue",
	Args:  exactArgs(2),
	RunE:  runIssueLabelAdd,
}

var issueLabelRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id> <label-id>",
	Short: "Remove a label from an issue",
	Args:  exactArgs(2),
	RunE:  runIssueLabelRemove,
}

func init() {
	issueLabelCmd.AddCommand(issueLabelListCmd)
	issueLabelCmd.AddCommand(issueLabelAddCmd)
	issueLabelCmd.AddCommand(issueLabelRemoveCmd)

	issueLabelListCmd.Flags().String("output", "table", "Output format: table or json")
	issueLabelAddCmd.Flags().String("output", "table", "Output format: table or json")
	issueLabelRemoveCmd.Flags().String("output", "table", "Output format: table or json")

	// Register under the top-level `issue` command.
	issueCmd.AddCommand(issueLabelCmd)
}

func runIssueLabelList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+args[0]+"/labels", &result); err != nil {
		return fmt.Errorf("list issue labels: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	printLabelTable(labelsRaw)
	return nil
}

func runIssueLabelAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"label_id": args[1]}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+args[0]+"/labels", body, &result); err != nil {
		return fmt.Errorf("attach label: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	printLabelTable(labelsRaw)
	return nil
}

func runIssueLabelRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/issues/"+args[0]+"/labels/"+args[1]); err != nil {
		return fmt.Errorf("detach label: %w", err)
	}

	// Follow up with the current label list so the user sees the result.
	// If the refresh fails, still print a clear success message — the
	// detach itself already succeeded.
	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/issues/"+args[0]+"/labels", &result); err != nil {
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"detached": true})
		}
		fmt.Fprintln(os.Stdout, "Label detached.")
		return nil
	}
	labelsRaw, _ := result["labels"].([]any)
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	printLabelTable(labelsRaw)
	return nil
}

func printLabelTable(labels []any) {
	headers := []string{"ID", "NAME", "COLOR"}
	rows := make([][]string, 0, len(labels))
	for _, raw := range labels {
		l, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			truncateID(strVal(l, "id")),
			strVal(l, "name"),
			strVal(l, "color"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}
