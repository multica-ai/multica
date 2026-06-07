// CEREBRO-PATCH(cerebro-wakeup-cli): agent wakeup/heartbeat CLI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var wakeupCmd = &cobra.Command{
	Use:   "wakeup",
	Short: "Schedule agent wakeups (cerebro)",
}

var wakeupCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an agent wakeup",
	RunE:  runWakeupCreate,
}

var wakeupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agent wakeups",
	RunE:  runWakeupList,
}

var wakeupGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get an agent wakeup",
	Args:  exactArgs(1),
	RunE:  runWakeupGet,
}

var wakeupCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a pending agent wakeup",
	Args:  exactArgs(1),
	RunE:  runWakeupCancel,
}

func init() {
	wakeupCmd.AddCommand(wakeupCreateCmd, wakeupListCmd, wakeupGetCmd, wakeupCancelCmd)

	wakeupCreateCmd.Flags().String("agent", "", "Agent ID to wake (defaults to MULTICA_AGENT_ID)")
	wakeupCreateCmd.Flags().String("issue", "", "Issue ID to run the agent on (required)")
	wakeupCreateCmd.Flags().String("prompt", "", "Prompt to give the agent when woken (required)")
	wakeupCreateCmd.Flags().String("at", "", "Wake at RFC3339 timestamp")
	wakeupCreateCmd.Flags().String("on-issue-status", "", "Wake when this issue reaches --watch-status")
	wakeupCreateCmd.Flags().String("watch-status", "", "Status for --on-issue-status")
	wakeupCreateCmd.Flags().String("on-github-ci", "", "Wake when linked GitHub CI updates for this issue")
	wakeupCreateCmd.Flags().String("output", "json", "Output format: table or json")

	wakeupListCmd.Flags().String("agent", "", "Filter by agent ID")
	wakeupListCmd.Flags().String("state", "pending", "Filter by state (empty for all)")
	wakeupListCmd.Flags().Int("limit", 50, "Maximum wakeups to return")
	wakeupListCmd.Flags().String("output", "table", "Output format: table or json")

	wakeupGetCmd.Flags().String("output", "json", "Output format: table or json")
	wakeupCancelCmd.Flags().String("output", "json", "Output format: table or json")
}

func runWakeupCreate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	agentID, _ := cmd.Flags().GetString("agent")
	if strings.TrimSpace(agentID) == "" {
		agentID = os.Getenv("MULTICA_AGENT_ID")
	}
	issueID, _ := cmd.Flags().GetString("issue")
	prompt, _ := cmd.Flags().GetString("prompt")
	at, _ := cmd.Flags().GetString("at")
	onIssueStatus, _ := cmd.Flags().GetString("on-issue-status")
	watchStatus, _ := cmd.Flags().GetString("watch-status")
	onGithubCI, _ := cmd.Flags().GetString("on-github-ci")
	output, _ := cmd.Flags().GetString("output")

	body := map[string]any{
		"agent_id": strings.TrimSpace(agentID),
		"issue_id": strings.TrimSpace(issueID),
		"prompt":   prompt,
	}
	switch {
	case strings.TrimSpace(at) != "":
		body["trigger_type"] = "time"
		body["fire_at"] = strings.TrimSpace(at)
	case strings.TrimSpace(onIssueStatus) != "":
		body["trigger_type"] = "issue_status"
		body["watch_issue_id"] = strings.TrimSpace(onIssueStatus)
		body["watch_status"] = strings.TrimSpace(watchStatus)
	case strings.TrimSpace(onGithubCI) != "":
		body["trigger_type"] = "github_ci"
		body["watch_issue_id"] = strings.TrimSpace(onGithubCI)
	default:
		return fmt.Errorf("one trigger is required: --at, --on-issue-status, or --on-github-ci")
	}
	var result map[string]any
	if err := client.PostJSON(context.Background(), "/api/cerebro/wakeups", body, &result); err != nil {
		return err
	}
	return printWakeupResult(output, result)
}

func runWakeupList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	q := url.Values{}
	if agent, _ := cmd.Flags().GetString("agent"); strings.TrimSpace(agent) != "" {
		q.Set("agent_id", strings.TrimSpace(agent))
	}
	if state, _ := cmd.Flags().GetString("state"); strings.TrimSpace(state) != "" {
		q.Set("state", strings.TrimSpace(state))
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	var result map[string]any
	if err := client.GetJSON(context.Background(), "/api/cerebro/wakeups?"+q.Encode(), &result); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	return printWakeupResult(output, result)
}

func runWakeupGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var result map[string]any
	if err := client.GetJSON(context.Background(), "/api/cerebro/wakeups/"+url.PathEscape(args[0]), &result); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	return printWakeupResult(output, result)
}

func runWakeupCancel(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var result map[string]any
	if err := client.PostJSON(context.Background(), "/api/cerebro/wakeups/"+url.PathEscape(args[0])+"/cancel", map[string]any{}, &result); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	return printWakeupResult(output, result)
}

func printWakeupResult(output string, v any) error {
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
	return nil
}
