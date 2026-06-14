package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Super-admin operations (requires SUPER_ADMIN_EMAILS)",
	Long:  "System-level management commands available only to users whose email is in the server's SUPER_ADMIN_EMAILS list.",
}

var adminUpdateUserCmd = &cobra.Command{
	Use:   "update-user <user-id>",
	Short: "Update a user's display name (super-admin only)",
	Long: "Update the global display name of any user. The change is reflected " +
		"across all workspaces immediately.\n\n" +
		"Requires the caller's email to be in the server's SUPER_ADMIN_EMAILS list.",
	Args: exactArgs(1),
	RunE: runAdminUpdateUser,
}

func init() {
	adminCmd.AddCommand(adminUpdateUserCmd)

	adminUpdateUserCmd.Flags().String("name", "", "New display name (required)")
	adminUpdateUserCmd.Flags().String("output", "table", "Output format: table or json")
	_ = adminUpdateUserCmd.MarkFlagRequired("name")
}

func runAdminUpdateUser(cmd *cobra.Command, args []string) error {
	userID := strings.TrimSpace(args[0])
	if userID == "" {
		return fmt.Errorf("user-id is required")
	}

	name, _ := cmd.Flags().GetString("name")
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("--name must be non-empty")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"name": name}
	var user map[string]any
	if err := client.PatchJSON(ctx, "/api/admin/users/"+userID, body, &user); err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, user)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintf(w, "ID\t%s\n", strVal(user, "id"))
	fmt.Fprintf(w, "Name\t%s\n", strVal(user, "name"))
	fmt.Fprintf(w, "Email\t%s\n", strVal(user, "email"))
	return nil
}
