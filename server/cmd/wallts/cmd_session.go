package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/wallts-ai/wallts/server/internal/cli"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage agent sessions for issue-level session persistence",
	Long: `Agent sessions track conversation continuity across runs on the same issue.
Sessions allow consecutive runs to resume from where the last run left off,
reducing token waste from duplicate analysis.`,
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sessions for an issue",
	RunE:  runSessionList,
}

var sessionGetCmd = &cobra.Command{
	Use:   "get <session-id>",
	Short: "Get session details",
	Args:  exactArgs(1),
	RunE:  runSessionGet,
}

var sessionResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset (deactivate) active sessions for an issue+agent",
	RunE:  runSessionReset,
}

func init() {
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionGetCmd)
	sessionCmd.AddCommand(sessionResetCmd)

	// session list
	sessionListCmd.Flags().String("issue", "", "Issue ID (required)")
	_ = sessionListCmd.MarkFlagRequired("issue")
	sessionListCmd.Flags().String("output", "table", "Output format: table or json")

	// session get
	sessionGetCmd.Flags().String("output", "table", "Output format: table or json")

	// session reset
	sessionResetCmd.Flags().String("issue", "", "Issue ID (required)")
	_ = sessionResetCmd.MarkFlagRequired("issue")
	sessionResetCmd.Flags().String("agent", "", "Agent ID (required)")
	_ = sessionResetCmd.MarkFlagRequired("agent")
	sessionResetCmd.Flags().String("output", "json", "Output format: table or json")
}

func runSessionList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	issueID, _ := cmd.Flags().GetString("issue")
	if issueID == "" {
		return fmt.Errorf("--issue is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var sessions []map[string]any
	params := url.Values{}
	params.Set("issue_id", issueID)
	path := "/api/agent-sessions?" + params.Encode()
	if err := client.GetJSON(ctx, path, &sessions); err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, sessions)
	}

	headers := []string{"SESSION ID", "RUN #", "AGENT ID", "STATUS", "BRANCH", "LAST ACTIVE"}
	rows := make([][]string, 0, len(sessions))
	for _, s := range sessions {
		status := "inactive"
		if v, ok := s["is_active"].(bool); ok && v {
			status = "active"
		}
		lastActive := strVal(s, "last_active_at")
		if len(lastActive) > 10 {
			lastActive = lastActive[:10] // truncate to date
		}
		rows = append(rows, []string{
			strVal(s, "id"),
			fmt.Sprintf("%v", s["run_number"]),
			strVal(s, "agent_id"),
			status,
			strVal(s, "branch"),
			lastActive,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSessionGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var session map[string]any
	if err := client.GetJSON(ctx, "/api/agent-sessions/"+args[0], &session); err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, session)
	}

	// Table view: show key fields.
	status := "inactive"
	if v, ok := session["is_active"].(bool); ok && v {
		status = "active"
	}
	headers := []string{"FIELD", "VALUE"}
	rows := [][]string{
		{"ID", strVal(session, "id")},
		{"Issue ID", strVal(session, "issue_id")},
		{"Agent ID", strVal(session, "agent_id")},
		{"Run Number", fmt.Sprintf("%v", session["run_number"])},
		{"Status", status},
		{"Branch", strVal(session, "branch")},
		{"Working Directory", strVal(session, "working_directory")},
		{"Summary", strVal(session, "conversation_summary")},
		{"Version", fmt.Sprintf("%v", session["version"])},
		{"Created At", strVal(session, "created_at")},
		{"Last Active", strVal(session, "last_active_at")},
	}

	// Files modified
	if files, ok := session["files_modified"].([]any); ok && len(files) > 0 {
		for i, f := range files {
			label := fmt.Sprintf("Files Modified[%d]", i)
			rows = append(rows, []string{label, fmt.Sprintf("%v", f)})
		}
	}

	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSessionReset(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	issueID, _ := cmd.Flags().GetString("issue")
	agentID, _ := cmd.Flags().GetString("agent")
	if issueID == "" || agentID == "" {
		return fmt.Errorf("--issue and --agent are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]string{
		"issue_id": issueID,
		"agent_id": agentID,
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agent-sessions/reset", body, &result); err != nil {
		return fmt.Errorf("reset session: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Fprintf(os.Stdout, "Session reset successful for issue=%s agent=%s\n", issueID, agentID)
	return nil
}
