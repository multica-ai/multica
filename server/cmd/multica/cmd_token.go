package main

import (
	"context"
	"fmt"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{Use: "token", Short: "Manage your personal access tokens"}
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
	tokenCmd.AddCommand(tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}
