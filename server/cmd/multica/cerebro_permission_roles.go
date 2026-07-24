// CEREBRO-PATCH(cerebro-permission-roles-cli): FIR-3388 Roles are versioned
// permission profiles inside the unified engine. This CLI exposes the same
// Role records and assignments as the workspace Permissions screen.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

type permissionRoleRule struct {
	Setting         string `json:"setting"`
	ResourcePattern string `json:"resource_pattern"`
	Conditions      any    `json:"conditions"`
}

type permissionRole struct {
	ID          string                          `json:"id"`
	Name        string                          `json:"name"`
	Description string                          `json:"description"`
	Version     int                             `json:"version"`
	Permissions map[string][]permissionRoleRule `json:"permissions"`
	ArchivedAt  *string                         `json:"archived_at"`
}

type permissionRoleAssignment struct {
	SubjectType        string  `json:"subject_type"`
	SubjectID          string  `json:"subject_id"`
	SubjectDisplayName *string `json:"subject_display_name"`
	ExpiresAt          *string `json:"expires_at"`
}

var permissionRolesCmd = &cobra.Command{
	Use:   "roles",
	Short: "Manage reusable, versioned permission profiles",
	Long: "List, inspect and manage the Roles that feed the same permission engine used by " +
		"Capabilities, Why Access and call-time enforcement.",
}

func init() {
	list := &cobra.Command{Use: "list", Short: "List permission Roles", Args: cobra.NoArgs, RunE: runPermissionRolesList}
	list.Flags().Bool("include-archived", false, "Include archived Roles")
	list.Flags().String("output", "table", "Output format: table or json")

	show := &cobra.Command{Use: "show <id>", Short: "Show one Role and its assignments", Args: exactArgs(1), RunE: runPermissionRolesShow}
	show.Flags().String("output", "json", "Output format: table or json")

	create := &cobra.Command{Use: "create", Short: "Create a versioned permission Role", Args: cobra.NoArgs, RunE: runPermissionRolesCreate}
	create.Flags().String("name", "", "Role name (required)")
	create.Flags().String("description", "", "Role description")
	create.Flags().String("permissions-file", "", "JSON file containing the Role permissions object")
	_ = create.MarkFlagRequired("name")

	update := &cobra.Command{Use: "update <id>", Short: "Save a new version of a permission Role", Args: exactArgs(1), RunE: runPermissionRolesUpdate}
	update.Flags().String("name", "", "New Role name")
	update.Flags().String("description", "", "New Role description")
	update.Flags().String("permissions-file", "", "JSON file containing the complete Role permissions object")

	archive := &cobra.Command{Use: "archive <id>", Short: "Archive a Role without deleting its history", Args: exactArgs(1), RunE: runPermissionRolesArchive}

	assign := &cobra.Command{Use: "assign <id>", Short: "Assign a Role to an agent or member", Args: exactArgs(1), RunE: runPermissionRolesAssign}
	assign.Flags().String("subject-type", "", "Subject type: agent or member (required)")
	assign.Flags().String("subject-id", "", "Agent UUID or member user UUID (required)")
	assign.Flags().String("expires-at", "", "Optional RFC3339 expiry")
	_ = assign.MarkFlagRequired("subject-type")
	_ = assign.MarkFlagRequired("subject-id")

	unassign := &cobra.Command{Use: "unassign <id>", Short: "Remove a Role assignment", Args: exactArgs(1), RunE: runPermissionRolesUnassign}
	unassign.Flags().String("subject-type", "", "Subject type: agent or member (required)")
	unassign.Flags().String("subject-id", "", "Agent UUID or member user UUID (required)")
	_ = unassign.MarkFlagRequired("subject-type")
	_ = unassign.MarkFlagRequired("subject-id")

	permissionRolesCmd.AddCommand(list, show, create, update, archive, assign, unassign)
	permissionsCmd.AddCommand(permissionRolesCmd)
}

func permissionRolesBasePath(client *cli.APIClient) (string, error) {
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("no workspace selected: set one with `multica config set workspace_id <id>` or MULTICA_WORKSPACE_ID")
	}
	return "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/roles", nil
}

func runPermissionRolesList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	includeArchived, _ := cmd.Flags().GetBool("include-archived")
	output, _ := cmd.Flags().GetString("output")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return listPermissionRoles(ctx, client, includeArchived, output)
}

func listPermissionRoles(ctx context.Context, client *cli.APIClient, includeArchived bool, output string) error {
	path, err := permissionRolesBasePath(client)
	if err != nil {
		return err
	}
	if includeArchived {
		path += "?include_archived=true"
	}
	var roles []permissionRole
	if err := client.GetJSON(ctx, path, &roles); err != nil {
		return fmt.Errorf("list permission Roles: %w", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, roles)
	}
	rows := make([][]string, 0, len(roles))
	for _, role := range roles {
		status := "active"
		if role.ArchivedAt != nil {
			status = "archived"
		}
		rows = append(rows, []string{role.ID, role.Name, fmt.Sprintf("%d", role.Version), fmt.Sprintf("%d", len(role.Permissions)), status})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "VERSION", "PERMISSIONS", "STATUS"}, rows)
	return nil
}

func runPermissionRolesShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return showPermissionRole(ctx, client, args[0], output)
}

func showPermissionRole(ctx context.Context, client *cli.APIClient, roleID, output string) error {
	path := "/api/roles/" + url.PathEscape(roleID)
	var role permissionRole
	if err := client.GetJSON(ctx, path, &role); err != nil {
		return fmt.Errorf("show permission Role: %w", err)
	}
	var assignments []permissionRoleAssignment
	if err := client.GetJSON(ctx, path+"/assignments", &assignments); err != nil {
		return fmt.Errorf("list permission Role assignments: %w", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"role": role, "assignments": assignments})
	}
	status := "active"
	if role.ArchivedAt != nil {
		status = "archived"
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "VERSION", "PERMISSIONS", "ASSIGNMENTS", "STATUS"}, [][]string{{
		role.ID, role.Name, fmt.Sprintf("%d", role.Version), fmt.Sprintf("%d", len(role.Permissions)), fmt.Sprintf("%d", len(assignments)), status,
	}})
	return nil
}

func readPermissionRoleFile(path string) (map[string][]permissionRoleRule, error) {
	if path == "" {
		return map[string][]permissionRoleRule{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read permissions file: %w", err)
	}
	var permissions map[string][]permissionRoleRule
	if err := json.Unmarshal(data, &permissions); err != nil {
		return nil, fmt.Errorf("parse permissions file: %w", err)
	}
	return permissions, nil
}

func runPermissionRolesCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	description, _ := cmd.Flags().GetString("description")
	file, _ := cmd.Flags().GetString("permissions-file")
	permissions, err := readPermissionRoleFile(file)
	if err != nil {
		return err
	}
	path, err := permissionRolesBasePath(client)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var role permissionRole
	if err := client.PostJSON(ctx, path, map[string]any{"name": name, "description": description, "permissions": permissions}, &role); err != nil {
		return fmt.Errorf("create permission Role: %w", err)
	}
	return cli.PrintJSON(os.Stdout, role)
}

func runPermissionRolesUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	body := map[string]any{}
	for _, name := range []string{"name", "description"} {
		if cmd.Flags().Changed(name) {
			value, _ := cmd.Flags().GetString(name)
			body[name] = value
		}
	}
	if cmd.Flags().Changed("permissions-file") {
		file, _ := cmd.Flags().GetString("permissions-file")
		permissions, err := readPermissionRoleFile(file)
		if err != nil {
			return err
		}
		body["permissions"] = permissions
	}
	if len(body) == 0 {
		return fmt.Errorf("set at least one of --name, --description or --permissions-file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var role permissionRole
	if err := client.PatchJSON(ctx, "/api/roles/"+url.PathEscape(args[0]), body, &role); err != nil {
		return fmt.Errorf("update permission Role: %w", err)
	}
	return cli.PrintJSON(os.Stdout, role)
}

func runPermissionRolesArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.DeleteJSON(ctx, "/api/roles/"+url.PathEscape(args[0])); err != nil {
		return fmt.Errorf("archive permission Role: %w", err)
	}
	fmt.Fprintln(os.Stdout, "Role archived")
	return nil
}

func runPermissionRolesAssign(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	subjectType, _ := cmd.Flags().GetString("subject-type")
	subjectID, _ := cmd.Flags().GetString("subject-id")
	expiresAt, _ := cmd.Flags().GetString("expires-at")
	subjectType = strings.ToLower(subjectType)
	if subjectType != "agent" && subjectType != "member" {
		return fmt.Errorf("--subject-type must be agent or member")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var assignment permissionRoleAssignment
	if err := client.PostJSON(ctx, "/api/roles/"+url.PathEscape(args[0])+"/assignments", map[string]any{
		"subject_type": subjectType, "subject_id": subjectID, "expires_at": expiresAt,
	}, &assignment); err != nil {
		return fmt.Errorf("assign permission Role: %w", err)
	}
	return cli.PrintJSON(os.Stdout, assignment)
}

func runPermissionRolesUnassign(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	subjectType, _ := cmd.Flags().GetString("subject-type")
	subjectID, _ := cmd.Flags().GetString("subject-id")
	subjectType = strings.ToLower(subjectType)
	if subjectType != "agent" && subjectType != "member" {
		return fmt.Errorf("--subject-type must be agent or member")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	path := "/api/roles/" + url.PathEscape(args[0]) + "/assignments/" + url.PathEscape(subjectType) + "/" + url.PathEscape(subjectID)
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove permission Role assignment: %w", err)
	}
	fmt.Fprintln(os.Stdout, "Role assignment removed")
	return nil
}
