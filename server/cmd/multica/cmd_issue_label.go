package main

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	ctx, cancel := cli.APIContext(context.Background())
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
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1], "issue")
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	body := map[string]any{"label_id": labelRef.ID}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/labels", body, &result); err != nil {
		return fmt.Errorf("attach label: %w", err)
	}
	labelsRaw, ok := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	// The attachment can succeed even when the server cannot read back the list.
	if !ok {
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"attached": true})
		}
		fmt.Fprintln(os.Stdout, "Label attached.")
		return nil
	}
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
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1], "issue")
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

const issueLabelFlagHelp = "Issue label to attach, by name (case-insensitive), UUID or UUID prefix (repeatable)"

// resolveIssueLabelFlags turns repeated --label values into issue label ids,
// de-duplicated in flag order. The label list is fetched at most once, and
// only when a value is not already a full UUID.
func resolveIssueLabelFlags(ctx context.Context, client *cli.APIClient, inputs []string) ([]string, error) {
	var candidates []idCandidate
	fetched := false
	ids := make([]string, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			return nil, fmt.Errorf("--label cannot be empty")
		}
		id := ""
		if uuidRegexp.MatchString(trimmed) {
			id = strings.ToLower(trimmed)
		} else {
			if !fetched {
				var err error
				if candidates, err = fetchLabelCandidates(ctx, client, "issue"); err != nil {
					return nil, fmt.Errorf("list issue labels: %w", err)
				}
				fetched = true
			}
			resolved, err := matchIssueLabel(candidates, trimmed)
			if err != nil {
				return nil, err
			}
			id = resolved
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// matchIssueLabel prefers an exact (case-insensitive) name over an id prefix,
// so a label named like a hex string still resolves by name.
func matchIssueLabel(candidates []idCandidate, input string) (string, error) {
	var byName []idCandidate
	for _, c := range candidates {
		if strings.EqualFold(c.Display, input) {
			byName = append(byName, c)
		}
	}
	if len(byName) == 1 {
		return byName[0].ID, nil
	}
	if len(byName) > 1 {
		return "", fmt.Errorf("label %q matches %d labels; pass its id instead", input, len(byName))
	}
	prefix, err := normalizeUUIDPrefix(input)
	if err != nil {
		return "", fmt.Errorf("no issue label named %q; run `multica label list` to see labels", input)
	}
	var byID []idCandidate
	for _, c := range candidates {
		if strings.HasPrefix(compactUUID(c.ID), prefix) {
			byID = append(byID, c)
		}
	}
	switch len(byID) {
	case 1:
		return byID[0].ID, nil
	case 0:
		return "", fmt.Errorf("no issue label named or with id prefix %q; run `multica label list` to see labels", input)
	default:
		return "", fmt.Errorf("label id prefix %q matches %d labels; use more characters", input, len(byID))
	}
}
