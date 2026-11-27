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
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List chat sessions", RunE: runChatSessionsList}
	list.Flags().Bool("all-members", false, "Search and list all member chats (requires a workspace superuser grant)")
	list.Flags().String("query", "", "Search chat titles and messages with --all-members")
	list.Flags().Int("offset", 0, "Skip earlier search results (pages contain up to 100 chats)")
	sessions.AddCommand(
		list,
		&cobra.Command{Use: "get <session-id>", Args: exactArgs(1), Short: "Get a chat session", RunE: runChatSessionsGet},
		&cobra.Command{Use: "messages <session-id>", Args: exactArgs(1), Short: "Read a chat transcript", RunE: runChatSessionsMessages},
	)
	chatCmd.AddCommand(sessions)
}

func runChatSessionsList(cmd *cobra.Command, _ []string) error {
	allMembers := false
	query := ""
	offset := 0
	if cmd.Flags().Lookup("all-members") != nil {
		allMembers, _ = cmd.Flags().GetBool("all-members")
	}
	if cmd.Flags().Lookup("query") != nil {
		query, _ = cmd.Flags().GetString("query")
	}
	if cmd.Flags().Lookup("offset") != nil {
		offset, _ = cmd.Flags().GetInt("offset")
	}
	if offset < 0 || offset > 1_000_000 {
		return fmt.Errorf("--offset must be between 0 and 1000000")
	}
	if query != "" && !allMembers {
		return fmt.Errorf("--query requires --all-members")
	}
	if offset != 0 && !allMembers {
		return fmt.Errorf("--offset requires --all-members")
	}
	if allMembers {
		path := "/api/chat/superuser/sessions?q=" + url.QueryEscape(query)
		if offset != 0 {
			path += fmt.Sprintf("&offset=%d", offset)
		}
		return readChatSessionJSON(cmd, path)
	}
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
