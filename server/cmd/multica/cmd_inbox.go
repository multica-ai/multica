package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Inbox commands — read-only access to the caller's notifications.

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Work with your inbox",
}

var inboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your inbox notifications",
	Long: "List the active (non-archived) inbox notifications for the current user in the workspace.\n\n" +
		"Read-only: nothing is marked read or archived. The server returns the whole inbox in one " +
		"response, so --project and --type narrow the output in the CLI, not the request.\n\n" +
		"JSON output passes through the server's fields, including issue_id and, for comment " +
		"notifications, details.comment_id.",
	Example: "  $ multica inbox list\n" +
		"  $ multica inbox list --project <project-id> --type mentioned --output json",
	RunE: runInboxList,
}

func init() {
	inboxCmd.AddCommand(inboxListCmd)

	inboxListCmd.Flags().String("output", "table", "Output format: table or json")
	inboxListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	inboxListCmd.Flags().String("project", "", "Only notifications about issues in this project (ID or unique ID prefix)")
	inboxListCmd.Flags().String("type", "", "Only notifications of this type (e.g. mentioned, new_comment, issue_assigned)")
}

func runInboxList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectID := ""
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		projectID = project.ID
	}
	itemType, _ := cmd.Flags().GetString("type")

	var all []map[string]any
	if err := client.GetJSON(ctx, "/api/inbox", &all); err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}

	items := make([]map[string]any, 0, len(all))
	for _, item := range all {
		if projectID != "" && !strings.EqualFold(strVal(item, "issue_project_id"), projectID) {
			continue
		}
		if itemType != "" && strVal(item, "type") != itemType {
			continue
		}
		items = append(items, item)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, items)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TYPE", "READ", "ISSUE ID", "TITLE", "CREATED"}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		created := strVal(item, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		read := "no"
		if r, _ := item["read"].(bool); r {
			read = "yes"
		}
		rows = append(rows, []string{
			displayID(strVal(item, "id"), fullID),
			strVal(item, "type"),
			read,
			displayID(strVal(item, "issue_id"), fullID),
			strVal(item, "title"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
