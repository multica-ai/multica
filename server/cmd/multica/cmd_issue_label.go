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
	issueLabelListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	issueLabelAddCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	issueLabelRemoveCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

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

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/labels", &result); err != nil {
		return fmt.Errorf("list issue labels: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func runIssueLabelAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	body := map[string]any{"label_id": labelRef.ID}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/labels", body, &result); err != nil {
		return fmt.Errorf("attach label: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func runIssueLabelRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID+"/labels/"+labelRef.ID); err != nil {
		return fmt.Errorf("detach label: %w", err)
	}

	// Follow up with the current label list so the user sees the result.
	// If the refresh fails, still print a clear success message — the
	// detach itself already succeeded.
	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/labels", &result); err != nil {
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
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func printLabelTable(labels []any, fullID bool) {
	headers := []string{"ID", "NAME", "COLOR"}
	rows := make([][]string, 0, len(labels))
	for _, raw := range labels {
		l, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			displayID(strVal(l, "id"), fullID),
			strVal(l, "name"),
			strVal(l, "color"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}
