package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func init() {
	sessions := &cobra.Command{Use: "sessions", Short: "Read your chat sessions in the selected workspace"}
	sessions.AddCommand(
		&cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List your chat sessions", RunE: runChatSessionsList},
		&cobra.Command{Use: "get <session-id>", Args: exactArgs(1), Short: "Get a chat session", RunE: runChatSessionsGet},
		&cobra.Command{Use: "messages <session-id>", Args: exactArgs(1), Short: "Read a chat transcript", RunE: runChatSessionsMessages},
	)
	chatCmd.AddCommand(sessions)
}

func runChatSessionsList(cmd *cobra.Command, _ []string) error {
	return readChatSessionJSON(cmd, "/api/chat/sessions?status=all")
}
func runChatSessionsGet(cmd *cobra.Command, args []string) error {
	return readChatSessionJSON(cmd, "/api/chat/sessions/"+url.PathEscape(args[0]))
}
func runChatSessionsMessages(cmd *cobra.Command, args []string) error {
	return readChatSessionJSON(cmd, "/api/chat/sessions/"+url.PathEscape(args[0])+"/messages")
}
func readChatSessionJSON(cmd *cobra.Command, path string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("read chat sessions: %w", err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}
