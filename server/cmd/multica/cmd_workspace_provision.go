package main

import (
	"context"
	"fmt"
	"net/mail"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var workspaceMemberProvisionCmd = &cobra.Command{
	Use:   "provision <email> [workspace-id|slug|prefix]",
	Short: "Provision a workspace member without email (owner only)",
	Long: "Creates an account and membership without sending email. First login still requires normal verification. " +
		"Requires the default-off settings_bulk_member_provisioning flag and workspace owner access. " +
		"--name applies only to a new account; existing profiles and membership roles never change. " +
		"Returns created, added, or already_member. The optional workspace uses the same UUID/slug/prefix resolution as member invite.",
	Args: cobra.RangeArgs(1, 2),
	RunE: runWorkspaceMemberProvision,
}

func init() {
	workspaceMemberCmd.AddCommand(workspaceMemberProvisionCmd)
	workspaceMemberProvisionCmd.Flags().String("name", "", "Display name for a new account only (omitted: email local part)")
	workspaceMemberProvisionCmd.Flags().String("role", "member", "Member role to grant: member or admin (never changes an existing role)")
	workspaceMemberProvisionCmd.Flags().String("output", "table", "Output format: table or json")
}

type provisionMemberResult struct {
	Email    string `json:"email"`
	Role     string `json:"role,omitempty"`
	Status   string `json:"status"`
	UserID   string `json:"user_id,omitempty"`
	MemberID string `json:"member_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

func runWorkspaceMemberProvision(cmd *cobra.Command, args []string) error {
	email := strings.ToLower(strings.TrimSpace(args[0]))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return fmt.Errorf("invalid email")
	}
	role, _ := cmd.Flags().GetString("role")
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "member" && role != "admin" {
		return fmt.Errorf("invalid --role %q; expected member or admin", role)
	}
	output, _ := cmd.Flags().GetString("output")
	if output != "table" && output != "json" {
		return fmt.Errorf("invalid --output %q; expected table or json", output)
	}
	entry := map[string]any{"email": email, "role": role}
	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("--name cannot be empty")
		}
		entry["name"] = name
	}
	wsID, err := resolveWorkspaceArg(cmd, args[1:])
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	// Positional workspace selection must also override the default request header.
	client.WorkspaceID = wsID
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var response struct {
		Results []provisionMemberResult `json:"results"`
	}
	if err := client.PostJSON(ctx, "/api/workspaces/"+wsID+"/members/provision", map[string]any{"entries": []any{entry}}, &response); err != nil {
		return fmt.Errorf("provision member: %w", err)
	}
	if len(response.Results) != 1 {
		return fmt.Errorf("provision member: malformed response (expected one result)")
	}
	result := response.Results[0]
	switch result.Status {
	case "created", "added", "already_member":
		if result.UserID == "" || result.MemberID == "" || result.Email != email {
			return fmt.Errorf("provision member: malformed success response")
		}
	case "invalid", "failed", "duplicate":
	default:
		return fmt.Errorf("provision member: unknown result status %q", result.Status)
	}
	if output == "json" {
		if err := cli.PrintJSON(os.Stdout, result); err != nil {
			return err
		}
	} else {
		cli.PrintTable(os.Stdout, []string{"EMAIL", "ROLE", "STATUS"}, [][]string{{result.Email, result.Role, result.Status}})
	}
	switch result.Status {
	case "created", "added", "already_member":
		return nil
	default:
		return fmt.Errorf("provision member: %s: %s", result.Status, result.Error)
	}
}
