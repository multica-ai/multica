package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Interact with channels assigned to the current issue",
}

var channelAskCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Send a question via the assigned channel and wait for a response",
	Long: `Sends a question to the external channel (e.g. Slack) assigned to the current issue.
Blocks until a response is received or the timeout is reached.
The response is printed to stdout.

Requires MULTICA_ISSUE_ID environment variable (set automatically by the daemon).`,
	Args: cobra.ExactArgs(1),
	RunE: runChannelAsk,
}

var channelHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show channel conversation history for the current issue",
	RunE:  runChannelHistory,
}

func init() {
	channelCmd.AddCommand(channelAskCmd)
	channelCmd.AddCommand(channelHistoryCmd)

	channelAskCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait for a response")
	channelAskCmd.Flags().Duration("poll-interval", 5*time.Second, "How often to poll for a response")
	channelHistoryCmd.Flags().String("output", "json", "Output format: table or json")
}

func runChannelAsk(cmd *cobra.Command, args []string) error {
	issueID := os.Getenv("MULTICA_ISSUE_ID")
	if issueID == "" {
		return fmt.Errorf("MULTICA_ISSUE_ID not set (this command is intended for agent runtime)")
	}
	agentID := os.Getenv("MULTICA_AGENT_ID")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
	question := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Send question.
	var askResp struct {
		MessageID string `json:"message_id"`
	}
	if err := client.PostJSON(ctx, fmt.Sprintf("/api/issues/%s/channel-ask", issueID), map[string]string{
		"question": question,
		"agent_id": agentID,
	}, &askResp); err != nil {
		return fmt.Errorf("send question: %w", err)
	}

	// Poll for response.
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[TIMEOUT] No response received within", timeout)
			return nil
		case <-ticker.C:
			var pollResp struct {
				Found    bool   `json:"found"`
				Response string `json:"response"`
			}
			if err := client.GetJSON(ctx, fmt.Sprintf("/api/channel-messages/%s/response", askResp.MessageID), &pollResp); err != nil {
				continue // retry on transient errors
			}
			if pollResp.Found {
				fmt.Println(pollResp.Response)
				return nil
			}
		}
	}
}

func runChannelHistory(cmd *cobra.Command, args []string) error {
	issueID := os.Getenv("MULTICA_ISSUE_ID")
	if issueID == "" {
		return fmt.Errorf("MULTICA_ISSUE_ID not set")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var messages []map[string]any
	if err := client.GetJSON(ctx, fmt.Sprintf("/api/issues/%s/channel-history", issueID), &messages); err != nil {
		return fmt.Errorf("get history: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, messages)
	}

	for _, m := range messages {
		dir := m["direction"]
		content := m["content"]
		ts := m["created_at"]
		fmt.Printf("[%s] %s: %s\n", ts, dir, content)
	}
	return nil
}
