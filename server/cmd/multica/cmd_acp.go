package main

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/acpbridge"
	"github.com/spf13/cobra"
)

var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Join Multica issue workers from an ACP editor over stdio",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		if client.Token == "" || strings.HasPrefix(client.Token, "mat_") {
			return fmt.Errorf("ACP requires a signed-in human CLI profile, not an agent task token")
		}
		if client.WorkspaceID == "" {
			return fmt.Errorf("select a workspace with --workspace-id or your CLI profile before starting ACP")
		}
		return acpbridge.New(client, client.WorkspaceID).Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

func init() { rootCmd.AddCommand(acpCmd) }
