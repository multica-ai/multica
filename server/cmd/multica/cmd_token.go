package main

import (
	"context"
	"fmt"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{Use: "token", Short: "Manage your personal access tokens"}
var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List safe metadata for your personal access tokens",
	Args:  cobra.NoArgs,
	RunE:  runTokenList,
}
var tokenRevokeCmd = &cobra.Command{
	Use: "revoke <token-id>", Short: "Revoke one of your personal access tokens",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		if err := client.DeleteJSON(ctx, "/api/tokens/"+args[0]); err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, "Personal access token revoked.")
		return err
	},
}

func init() {
	tokenListCmd.Flags().String("output", "table", "Output format: table or json")
	tokenCmd.AddCommand(tokenListCmd)
	tokenCmd.AddCommand(tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}

type tokenListAPIItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
}

type tokenListOutputItem struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	LastUsed  *string `json:"last_used_at"`
	Expires   *string `json:"expires_at"`
}

func runTokenList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var tokens []tokenListAPIItem
	if err := client.GetJSON(ctx, "/api/tokens", &tokens); err != nil {
		return fmt.Errorf("list personal access tokens: %w", err)
	}
	items := make([]tokenListOutputItem, 0, len(tokens))
	for _, token := range tokens {
		id := token.ID
		if len(id) > 8 {
			id = id[:8]
		}
		items = append(items, tokenListOutputItem{
			ID: id, Name: token.Name, CreatedAt: token.CreatedAt,
			LastUsed: token.LastUsedAt, Expires: token.ExpiresAt,
		})
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, items)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q; use table or json", output)
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		lastUsed, expires := "", ""
		if item.LastUsed != nil {
			lastUsed = *item.LastUsed
		}
		if item.Expires != nil {
			expires = *item.Expires
		}
		rows = append(rows, []string{item.ID, item.Name, item.CreatedAt, lastUsed, expires})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "CREATED", "LAST USED", "EXPIRES"}, rows)
	return nil
}
