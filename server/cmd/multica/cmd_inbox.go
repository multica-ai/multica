package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Read your inbox notifications",
}

var inboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your active notifications without changing read status",
	Long:  "List your active notifications without changing read status. Filters apply locally. Agent tasks may read the runtime owner's inbox only when that owner is the task's verified upstream human originator; cloud-node credentials are refused.",
	Args:  exactArgs(0),
	RunE:  runInboxList,
}

func init() {
	inboxCmd.AddCommand(inboxListCmd)
	registerInboxListFlags(inboxListCmd)
}

func registerInboxListFlags(cmd *cobra.Command) {
	cmd.Flags().String("project", "", "Filter by issue project ID or unique ID prefix")
	cmd.Flags().String("type", "", "Filter by notification type (e.g. mentioned)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

type inboxListItem struct {
	ID             string  `json:"id"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	IssueID        *string `json:"issue_id"`
	CommentID      *string `json:"comment_id,omitempty"`
	IssueProjectID *string `json:"issue_project_id"`
	Read           bool    `json:"read"`
	CreatedAt      string  `json:"created_at"`
}

func runInboxList(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "json" && output != "table" {
		return fmt.Errorf("output must be table or json")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()

	projectID := ""
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		projectID = project.ID
	}

	var response []struct {
		inboxListItem
		Details json.RawMessage `json:"details"`
	}
	if err := client.GetJSON(ctx, "/api/inbox", &response); err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}
	notifType, _ := cmd.Flags().GetString("type")
	items := make([]inboxListItem, 0, len(response))
	for _, row := range response {
		if projectID != "" && (row.IssueProjectID == nil || !strings.EqualFold(*row.IssueProjectID, projectID)) {
			continue
		}
		if notifType != "" && row.Type != notifType {
			continue
		}
		var details struct {
			CommentID *string `json:"comment_id"`
		}
		if json.Unmarshal(row.Details, &details) == nil {
			row.CommentID = details.CommentID
		}
		items = append(items, row.inboxListItem)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, items)
	}
	rows := make([][]string, 0, len(items))
	value := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	for _, item := range items {
		rows = append(rows, []string{item.ID, item.Type, strings.Join(strings.Fields(item.Title), " "), value(item.IssueID), value(item.CommentID), value(item.IssueProjectID), strconv.FormatBool(item.Read), item.CreatedAt})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "TYPE", "TITLE", "ISSUE", "COMMENT", "PROJECT", "READ", "CREATED"}, rows)
	return nil
}
