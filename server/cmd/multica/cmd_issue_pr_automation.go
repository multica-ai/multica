package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var issuePRAutomationCmd = &cobra.Command{
	Use: "pr-automation <id>", Short: "Inspect PR completion policy or manage an issue's PR links", Args: exactArgs(1), RunE: runIssuePRAutomation,
	Long: "Show the workspace policy, linked PR sources, exclusions and the reason an issue is waiting.\nUse one mutation flag to disable/resume automatic completion, link a PR URL, exclude a PR UUID, or restore automatic linking. A mutation can immediately complete the issue when all remaining PRs are merged.",
}

func init() {
	issueCmd.AddCommand(issuePRAutomationCmd)
	issuePRAutomationCmd.Flags().String("output", "json", "Output format: json or table")
	issuePRAutomationCmd.Flags().Bool("disabled", false, "Disable automatic completion (use --disabled=false to follow workspace)")
	issuePRAutomationCmd.Flags().String("link", "", "Manually link a PR URL from a connected provider")
	issuePRAutomationCmd.Flags().String("exclude", "", "Exclude a linked PR UUID, including from future auto-linking")
	issuePRAutomationCmd.Flags().String("restore", "", "Restore automatic linking for a PR UUID")
	issuePRAutomationCmd.MarkFlagsMutuallyExclusive("disabled", "link", "exclude", "restore")
}
func runIssuePRAutomation(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	ref, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	path := "/api/issues/" + url.PathEscape(ref.ID) + "/pr-automation"
	var body map[string]any
	if cmd.Flags().Changed("disabled") {
		v, _ := cmd.Flags().GetBool("disabled")
		body = map[string]any{"disabled": v}
	}
	for _, flag := range []string{"link", "exclude", "restore"} {
		if !cmd.Flags().Changed(flag) {
			continue
		}
		value, _ := cmd.Flags().GetString(flag)
		if value == "" {
			return fmt.Errorf("--%s requires a value", flag)
		}
		switch flag {
		case "link":
			body = map[string]any{"url": value, "mode": "manual"}
		case "exclude":
			body = map[string]any{"pr_id": value, "mode": "excluded"}
		case "restore":
			body = map[string]any{"pr_id": value, "mode": "automatic"}
		}
	}
	if body != nil {
		var result map[string]any
		if err = client.PutJSON(ctx, path, body, &result); err != nil {
			return err
		}
	}
	var result map[string]any
	if err = client.GetJSON(ctx, path, &result); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q", output)
	}
	policy, _ := result["policy"].(map[string]any)
	issue, _ := result["issue"].(map[string]any)
	decision, _ := issue["decision"].(map[string]any)
	cli.PrintTable(os.Stdout, []string{"MIGRATED", "SOURCE", "AUTO COMPLETE", "ISSUE DISABLED", "REASON"}, [][]string{{fmt.Sprint(result["migrated"]), strVal(policy, "source"), fmt.Sprint(policy["auto_complete"]), fmt.Sprint(issue["disabled"]), strVal(decision, "reason")}})
	var rows [][]string
	for _, field := range []string{"links", "excluded"} {
		values, _ := issue[field].([]any)
		for _, v := range values {
			if link, ok := v.(map[string]any); ok {
				rows = append(rows, []string{strVal(link, "pr_id"), field, strVal(link, "source"), strVal(link, "state"), strVal(link, "url")})
			}
		}
	}
	cli.PrintTable(os.Stdout, []string{"PR ID", "RELATION", "SOURCE", "STATE", "URL"}, rows)
	return nil
}
