package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func issuePRAutoCompleteFlag(cmd *cobra.Command) (*bool, error) {
	if !cmd.Flags().Changed("pr-auto-complete") {
		return nil, nil
	}
	value, _ := cmd.Flags().GetString("pr-auto-complete")
	if value != "on" && value != "off" {
		return nil, fmt.Errorf("--pr-auto-complete must be on or off")
	}
	disabled := value == "off"
	return &disabled, nil
}

func prAutoCompleteValue(disabled bool) string {
	if disabled {
		return "off"
	}
	return "on"
}

func setIssuePRAutoComplete(ctx context.Context, client *cli.APIClient, issueID string, disabled bool) error {
	var result struct {
		AutoComplete *struct {
			IssueDisabled *bool `json:"issue_disabled"`
		} `json:"auto_complete"`
	}
	if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/pr-auto-complete", map[string]bool{"disabled": disabled}, &result); err != nil {
		return fmt.Errorf("set issue PR auto-complete: %w", err)
	}
	if result.AutoComplete == nil || result.AutoComplete.IssueDisabled == nil || *result.AutoComplete.IssueDisabled != disabled {
		return fmt.Errorf("server did not confirm --pr-auto-complete %s", prAutoCompleteValue(disabled))
	}
	return nil
}

func confirmCreatedIssuePRAutoComplete(ctx context.Context, client *cli.APIClient, issue map[string]any, disabled bool) error {
	confirmed, present := issue["pr_auto_complete_disabled"]
	var err error
	if present {
		if value, ok := confirmed.(bool); !ok || value != disabled {
			err = fmt.Errorf("server did not confirm --pr-auto-complete %s", prAutoCompleteValue(disabled))
		}
	} else {
		// Older servers ignore the create field but may support the existing PUT.
		if id := strVal(issue, "id"); id != "" {
			err = setIssuePRAutoComplete(ctx, client, id, disabled)
		} else {
			err = fmt.Errorf("server returned no created issue ID")
		}
	}
	if err != nil {
		return fmt.Errorf("issue %s (ID: %s) was created, but PR auto-complete was not confirmed; do not repeat issue create; retry with issue update %s --pr-auto-complete %s: %w", issueDisplayKey(issue), strVal(issue, "id"), strVal(issue, "id"), prAutoCompleteValue(disabled), err)
	}
	issue["pr_auto_complete_disabled"] = disabled
	return nil
}

func printIssuePRAutoComplete(autoComplete map[string]any) {
	disabled, ok := autoComplete["issue_disabled"].(bool)
	if !ok {
		return
	}
	workspaceEnabled, ok := autoComplete["workspace_enabled"].(bool)
	if !ok {
		return
	}
	fmt.Fprintf(os.Stdout, "PR auto-complete: %s | Workspace: %s | Target: %s | State: %s\n",
		prAutoCompleteValue(disabled), prAutoCompleteValue(!workspaceEnabled),
		strVal(autoComplete, "target_status"), strVal(autoComplete, "state"))
}
