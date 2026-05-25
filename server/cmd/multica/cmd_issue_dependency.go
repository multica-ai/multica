package main

// CEREBRO-PATCH(issue-dependencies): CLI for blocks / blocked-by / related issue
// relations (FIR-823). Talks to the /api/issues/{id}/{blocks,blocked-by,related}
// endpoints added in server/internal/handler/issue_dependency.go.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueBlockCmd = &cobra.Command{
	Use:   "block <id>",
	Short: "Record that an issue is blocked by (or blocks) another issue",
	Long: `Record a blocking relation between two issues.

  multica issue block <id> --by <other>       # <id> is blocked by <other>
  multica issue block <id> --blocks <other>   # <id> blocks <other>

Exactly one of --by / --blocks must be given.`,
	Args: exactArgs(1),
	RunE: runIssueBlock,
}

var issueUnblockCmd = &cobra.Command{
	Use:   "unblock <id>",
	Short: "Remove a blocking relation between two issues",
	Long: `Remove a blocking relation between two issues.

  multica issue unblock <id> --from <other>     # <id> is no longer blocked by <other>
  multica issue unblock <id> --blocks <other>   # <id> no longer blocks <other>

Exactly one of --from / --blocks must be given.`,
	Args: exactArgs(1),
	RunE: runIssueUnblock,
}

var issueBlocksCmd = &cobra.Command{
	Use:   "blocks <id>",
	Short: "Show an issue's blocks / blocked-by / related relations",
	Args:  exactArgs(1),
	RunE:  runIssueBlocks,
}

var issueRelatedCmd = &cobra.Command{
	Use:   "related <id>",
	Short: "Link or unlink an issue as related to another",
	Long: `Manage symmetric "related" links between two issues.

  multica issue related <id> --to <other>     # link <id> and <other> as related
  multica issue related <id> --remove <other> # remove the related link`,
	Args: exactArgs(1),
	RunE: runIssueRelated,
}

func init() {
	for _, c := range []*cobra.Command{issueBlockCmd, issueUnblockCmd, issueBlocksCmd, issueRelatedCmd} {
		c.Flags().String("output", "table", "Output format: table or json")
		c.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	}
	issueBlockCmd.Flags().String("by", "", "The issue that blocks <id>")
	issueBlockCmd.Flags().String("blocks", "", "The issue that <id> blocks")
	issueUnblockCmd.Flags().String("from", "", "Remove the blocker <other> from <id>")
	issueUnblockCmd.Flags().String("blocks", "", "Remove the issue <id> blocks")
	issueRelatedCmd.Flags().String("to", "", "Link <id> to <other> as related")
	issueRelatedCmd.Flags().String("remove", "", "Remove the related link to <other>")

	issueCmd.AddCommand(issueBlockCmd)
	issueCmd.AddCommand(issueUnblockCmd)
	issueCmd.AddCommand(issueBlocksCmd)
	issueCmd.AddCommand(issueRelatedCmd)
}

// resolveOtherIssue resolves a flag value (UUID or identifier) to its UUID.
func resolveOtherIssue(ctx context.Context, client *cli.APIClient, ref string) (string, error) {
	other, err := resolveIssueRef(ctx, client, ref)
	if err != nil {
		return "", fmt.Errorf("resolve target issue %q: %w", ref, err)
	}
	return other.ID, nil
}

func runIssueBlock(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	by, _ := cmd.Flags().GetString("by")
	blocks, _ := cmd.Flags().GetString("blocks")
	if (by == "") == (blocks == "") {
		return fmt.Errorf("provide exactly one of --by or --blocks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var path, target string
	if by != "" {
		path = "/blocked-by"
		target = by
	} else {
		path = "/blocks"
		target = blocks
	}
	otherID, err := resolveOtherIssue(ctx, client, target)
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+path, map[string]any{"issue_id": otherID}, &result); err != nil {
		return fmt.Errorf("add dependency: %w", err)
	}
	return printDependencies(cmd, result)
}

func runIssueUnblock(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	from, _ := cmd.Flags().GetString("from")
	blocks, _ := cmd.Flags().GetString("blocks")
	if (from == "") == (blocks == "") {
		return fmt.Errorf("provide exactly one of --from or --blocks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var path, target string
	if from != "" {
		path = "/blocked-by/"
		target = from
	} else {
		path = "/blocks/"
		target = blocks
	}
	otherID, err := resolveOtherIssue(ctx, client, target)
	if err != nil {
		return err
	}

	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID+path+otherID); err != nil {
		return fmt.Errorf("remove dependency: %w", err)
	}
	return fetchAndPrintDependencies(cmd, client, ctx, issueRef.ID)
}

func runIssueRelated(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	to, _ := cmd.Flags().GetString("to")
	remove, _ := cmd.Flags().GetString("remove")
	if (to == "") == (remove == "") {
		return fmt.Errorf("provide exactly one of --to or --remove")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	if to != "" {
		otherID, err := resolveOtherIssue(ctx, client, to)
		if err != nil {
			return err
		}
		var result map[string]any
		if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/related", map[string]any{"issue_id": otherID}, &result); err != nil {
			return fmt.Errorf("add related: %w", err)
		}
		return printDependencies(cmd, result)
	}

	otherID, err := resolveOtherIssue(ctx, client, remove)
	if err != nil {
		return err
	}
	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID+"/related/"+otherID); err != nil {
		return fmt.Errorf("remove related: %w", err)
	}
	return fetchAndPrintDependencies(cmd, client, ctx, issueRef.ID)
}

func runIssueBlocks(cmd *cobra.Command, args []string) error {
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
	return fetchAndPrintDependencies(cmd, client, ctx, issueRef.ID)
}

func fetchAndPrintDependencies(cmd *cobra.Command, client *cli.APIClient, ctx context.Context, issueID string) error {
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueID+"/dependencies", &result); err != nil {
		return fmt.Errorf("list dependencies: %w", err)
	}
	return printDependencies(cmd, result)
}

func printDependencies(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"RELATION", "ISSUE", "STATUS", "TITLE"}
	rows := make([][]string, 0)
	appendGroup := func(label, key string) {
		group, _ := result[key].([]any)
		for _, raw := range group {
			d, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := strVal(d, "identifier")
			if id == "" {
				id = displayID(strVal(d, "id"), fullID)
			}
			rows = append(rows, []string{label, id, strVal(d, "status"), strVal(d, "title")})
		}
	}
	appendGroup("blocked by", "blocked_by")
	appendGroup("blocks", "blocks")
	appendGroup("related", "related")
	if len(rows) == 0 {
		fmt.Fprintln(os.Stdout, "No dependencies.")
		return nil
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
