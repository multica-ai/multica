package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var mentionCmd = &cobra.Command{
	Use:   "mention",
	Short: "Resolve structured mentions",
}

var mentionResolveCmd = &cobra.Command{
	Use:   "resolve <name>",
	Short: "Resolve a member or agent name to canonical mention markup",
	Args:  exactArgs(1),
	RunE:  runMentionResolve,
}

func init() {
	mentionCmd.AddCommand(mentionResolveCmd)

	mentionResolveCmd.Flags().String("type", "", "Mention type: member or agent (required)")
	mentionResolveCmd.Flags().String("output", "table", "Output format: table or json")
}

func runMentionResolve(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	mentionType, _ := cmd.Flags().GetString("type")
	mentionType = strings.TrimSpace(strings.ToLower(mentionType))
	if mentionType != "agent" && mentionType != "member" {
		return fmt.Errorf("--type must be 'agent' or 'member'")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("name", name)
	params.Set("type", mentionType)

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/mention-resolve?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("resolve mention: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	headers := []string{"ID", "LABEL", "TYPE", "MENTION"}
	rows := [][]string{{
		strVal(result, "id"),
		strVal(result, "label"),
		strVal(result, "type"),
		strVal(result, "mention"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
