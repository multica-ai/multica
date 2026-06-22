package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Issue pin commands.
//
// Pins are a per-user sidebar concept backed by /api/pins (see
// server/internal/handler/pin.go). The endpoint is generic over item_type
// ("issue" | "project"); these commands scope it to issues and resolve the
// usual MUL-XXXX / UUID / short-prefix identifiers via resolveIssueRef so
// callers can say `multica issue pin MUL-3452` instead of pasting a UUID.

var issuePinCmd = &cobra.Command{
	Use:   "pin <id>",
	Short: "Pin an issue to the sidebar for the current user",
	Args:  exactArgs(1),
	RunE:  runIssuePin,
}

var issueUnpinCmd = &cobra.Command{
	Use:   "unpin <id>",
	Short: "Unpin an issue from the sidebar for the current user",
	Args:  exactArgs(1),
	RunE:  runIssueUnpin,
}

var issuePinsCmd = &cobra.Command{
	Use:   "pins",
	Short: "List the issues the current user has pinned",
	RunE:  runIssuePins,
}

func init() {
	issueCmd.AddCommand(issuePinCmd)
	issueCmd.AddCommand(issueUnpinCmd)
	issueCmd.AddCommand(issuePinsCmd)

	issuePinCmd.Flags().String("output", "json", "Output format: table or json")
	issuePinsCmd.Flags().String("output", "table", "Output format: table or json")
	issuePinsCmd.Flags().Bool("full-id", false, "Show the full issue UUID alongside the identifier")
}

// runIssuePin pins an issue for the current user. Pinning is idempotent: a
// 409 (already pinned) is reported as a friendly message rather than an
// error so re-running `issue pin` on an already-pinned issue exits 0.
func runIssuePin(cmd *cobra.Command, args []string) error {
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

	body := map[string]any{"item_type": "issue", "item_id": issueRef.ID}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/pins", body, &result); err != nil {
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict {
			// Idempotent: the backend returns 409 with no pin body when
			// the issue is already pinned, so there is nothing to print
			// but a friendly confirmation.
			fmt.Fprintf(os.Stderr, "Issue %s is already pinned.\n", issueRef.Display)
			return nil
		}
		return fmt.Errorf("pin issue: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Pinned issue %s.\n", issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// runIssueUnpin removes an issue pin. The backend DELETE is idempotent
// (returns 204 whether or not the pin existed), so this needs no 404 handling.
func runIssueUnpin(cmd *cobra.Command, args []string) error {
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

	if err := client.DeleteJSON(ctx, "/api/pins/issue/"+issueRef.ID); err != nil {
		return fmt.Errorf("unpin issue: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Unpinned issue %s.\n", issueRef.Display)
	return nil
}

// runIssuePins lists the current user's pins, filtered to issues. The pin
// response carries only the item UUID (see PinnedItemResponse), so for the
// table view we fetch each pinned issue to render its identifier + title;
// JSON output prints the raw pin rows verbatim. Pin lists are small (a
// sidebar), so the per-issue fetches are bounded and any issue that has
// since been deleted is surfaced by raw UUID rather than failing the list.
func runIssuePins(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var pins []map[string]any
	if err := client.GetJSON(ctx, "/api/pins", &pins); err != nil {
		return fmt.Errorf("list pins: %w", err)
	}

	issuePins := make([]map[string]any, 0, len(pins))
	for _, p := range pins {
		if strVal(p, "item_type") == "issue" {
			issuePins = append(issuePins, p)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, issuePins)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"IDENTIFIER", "TITLE", "STATUS", "PINNED_AT"}
	if fullID {
		headers = []string{"IDENTIFIER", "ID", "TITLE", "STATUS", "PINNED_AT"}
	}
	rows := make([][]string, 0, len(issuePins))
	for _, p := range issuePins {
		itemID := strVal(p, "item_id")
		identifier, title, status := issuePinDisplay(ctx, client, itemID)
		if identifier == "" {
			identifier = itemID
		}
		pinnedAt := strVal(p, "created_at")
		if len(pinnedAt) >= 16 {
			pinnedAt = pinnedAt[:16]
		}
		row := []string{identifier, title, status, pinnedAt}
		if fullID {
			row = []string{identifier, itemID, title, status, pinnedAt}
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// issuePinDisplay fetches a single pinned issue to render its identifier,
// title, and status. A missing/deleted issue returns empty strings so the
// caller can fall back to the raw UUID.
func issuePinDisplay(ctx context.Context, client *cli.APIClient, issueID string) (identifier, title, status string) {
	if issueID == "" {
		return "", "", ""
	}
	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueID, &issue); err != nil {
		return "", "", ""
	}
	return issueDisplayKey(issue), strVal(issue, "title"), strVal(issue, "status")
}
