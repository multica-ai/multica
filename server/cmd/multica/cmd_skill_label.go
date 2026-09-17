package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica skill label {list|add|remove} — manages the labels attached to a
// specific skill. The label itself is managed via `multica label ...`.

var skillLabelCmd = &cobra.Command{
	Use:   "label",
	Short: "Manage labels on a skill",
}

var skillLabelListCmd = &cobra.Command{
	Use:   "list <skill-id>",
	Short: "List labels on a skill",
	Args:  exactArgs(1),
	RunE:  runSkillLabelList,
}

var skillLabelAddCmd = &cobra.Command{
	Use:   "add <skill-id> <label-id>",
	Short: "Attach a label to a skill",
	Args:  exactArgs(2),
	RunE:  runSkillLabelAdd,
}

var skillLabelRemoveCmd = &cobra.Command{
	Use:   "remove <skill-id> <label-id>",
	Short: "Remove a label from a skill",
	Args:  exactArgs(2),
	RunE:  runSkillLabelRemove,
}

func init() {
	skillLabelCmd.AddCommand(skillLabelListCmd)
	skillLabelCmd.AddCommand(skillLabelAddCmd)
	skillLabelCmd.AddCommand(skillLabelRemoveCmd)

	skillLabelListCmd.Flags().String("output", "table", "Output format: table or json")
	skillLabelAddCmd.Flags().String("output", "table", "Output format: table or json")
	skillLabelRemoveCmd.Flags().String("output", "table", "Output format: table or json")
	skillLabelListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	skillLabelAddCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	skillLabelRemoveCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// Register under the top-level `skill` command.
	skillCmd.AddCommand(skillLabelCmd)
}

func runSkillLabelList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/labels", &result); err != nil {
		return fmt.Errorf("list skill labels: %w", err)
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

func runSkillLabelAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	labelRef, err := resolveLabelID(ctx, client, args[1], "skill")
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	body := map[string]any{"label_id": labelRef.ID}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/labels", body, &result); err != nil {
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

func runSkillLabelRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	labelRef, err := resolveLabelID(ctx, client, args[1], "skill")
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/labels/"+labelRef.ID); err != nil {
		return fmt.Errorf("detach label: %w", err)
	}

	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/labels", &result); err != nil {
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
