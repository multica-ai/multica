// CEREBRO-PATCH(cerebro-groups-cli): JEH-1172 cerebro-only file — group CLI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// groupCmd is the cerebro `multica group ...` command tree. The server
// surface lives in server/internal/cerebro/groups (CRUD + membership) and
// server/internal/cerebro/grouppermissions (capability / runtime / agent /
// project grants). Each subcommand routes to the matching API endpoint
// declared in server/cmd/server/router.go.
var groupCmd = &cobra.Command{
	Use:   "group",
	Short: "Work with workspace groups (cerebro)",
}

var (
	groupListCmd = &cobra.Command{
		Use:   "list",
		Short: "List groups in the workspace",
		RunE:  runGroupList,
	}
	groupGetCmd = &cobra.Command{
		Use:   "get <id>",
		Short: "Get group details",
		Args:  exactArgs(1),
		RunE:  runGroupGet,
	}
	groupCreateCmd = &cobra.Command{
		Use:   "create",
		Short: "Create a new group",
		RunE:  runGroupCreate,
	}
	groupUpdateCmd = &cobra.Command{
		Use:   "update <id>",
		Short: "Update a group",
		Args:  exactArgs(1),
		RunE:  runGroupUpdate,
	}
	groupDeleteCmd = &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a group",
		Args:  exactArgs(1),
		RunE:  runGroupDelete,
	}

	groupMemberCmd = &cobra.Command{
		Use:   "member",
		Short: "Manage group members",
	}
	groupMemberListCmd = &cobra.Command{
		Use:   "list <group-id>",
		Short: "List members of a group",
		Args:  exactArgs(1),
		RunE:  runGroupMemberList,
	}
	groupMemberAddCmd = &cobra.Command{
		Use:   "add <group-id>",
		Short: "Add a member to a group (--user <name|id>)",
		Args:  exactArgs(1),
		RunE:  runGroupMemberAdd,
	}
	groupMemberRemoveCmd = &cobra.Command{
		Use:   "remove <group-id> <user>",
		Short: "Remove a member from a group",
		Args:  exactArgs(2),
		RunE:  runGroupMemberRemove,
	}

	groupCapabilityCmd = &cobra.Command{
		Use:   "capability",
		Short: "Manage group capabilities (admin only)",
	}
	groupCapabilityListCmd = &cobra.Command{
		Use:   "list <group-id>",
		Short: "List capabilities granted to a group",
		Args:  exactArgs(1),
		RunE:  runGroupCapabilityList,
	}
	groupCapabilitySetCmd = &cobra.Command{
		Use:   "set <group-id> <capability>",
		Short: "Grant a capability to a group (e.g. create_runtime, create_agent)",
		Args:  exactArgs(2),
		RunE:  runGroupCapabilitySet,
	}
	groupCapabilityRemoveCmd = &cobra.Command{
		Use:   "remove <group-id> <capability>",
		Short: "Revoke a capability from a group",
		Args:  exactArgs(2),
		RunE:  runGroupCapabilityRemove,
	}

	groupRuntimeCmd = &cobra.Command{
		Use:   "runtime",
		Short: "Manage the runtime allowlist for a group (admin only)",
	}
	groupRuntimeListCmd = &cobra.Command{
		Use:   "list <group-id>",
		Short: "List runtimes allowed for a group",
		Args:  exactArgs(1),
		RunE:  runGroupRuntimeList,
	}
	groupRuntimeAddCmd = &cobra.Command{
		Use:   "add <group-id> <runtime-id>",
		Short: "Allow a runtime for a group",
		Args:  exactArgs(2),
		RunE:  runGroupRuntimeAdd,
	}
	groupRuntimeRemoveCmd = &cobra.Command{
		Use:   "remove <group-id> <runtime-id>",
		Short: "Revoke a runtime from a group",
		Args:  exactArgs(2),
		RunE:  runGroupRuntimeRemove,
	}

	groupAgentCmd = &cobra.Command{
		Use:   "agent",
		Short: "Manage the agent allowlist for a group (admin only)",
	}
	groupAgentListCmd = &cobra.Command{
		Use:   "list <group-id>",
		Short: "List agents allowed for a group",
		Args:  exactArgs(1),
		RunE:  runGroupAgentList,
	}
	groupAgentAddCmd = &cobra.Command{
		Use:   "add <group-id> <agent>",
		Short: "Allow an agent for a group (name or UUID)",
		Args:  exactArgs(2),
		RunE:  runGroupAgentAdd,
	}
	groupAgentRemoveCmd = &cobra.Command{
		Use:   "remove <group-id> <agent>",
		Short: "Revoke an agent from a group",
		Args:  exactArgs(2),
		RunE:  runGroupAgentRemove,
	}

	groupProjectCmd = &cobra.Command{
		Use:   "project",
		Short: "Manage group access on projects (admin only)",
	}
	groupProjectListCmd = &cobra.Command{
		Use:   "list <project-id>",
		Short: "List groups with access to a project",
		Args:  exactArgs(1),
		RunE:  runGroupProjectList,
	}
	groupProjectAddCmd = &cobra.Command{
		Use:   "add <project-id> <group>",
		Short: "Grant a group access to a project",
		Args:  exactArgs(2),
		RunE:  runGroupProjectAdd,
	}
	groupProjectRemoveCmd = &cobra.Command{
		Use:   "remove <project-id> <group>",
		Short: "Revoke a group's access to a project",
		Args:  exactArgs(2),
		RunE:  runGroupProjectRemove,
	}
)

func init() {
	groupCmd.AddCommand(groupListCmd)
	groupCmd.AddCommand(groupGetCmd)
	groupCmd.AddCommand(groupCreateCmd)
	groupCmd.AddCommand(groupUpdateCmd)
	groupCmd.AddCommand(groupDeleteCmd)
	groupCmd.AddCommand(groupMemberCmd)
	groupCmd.AddCommand(groupCapabilityCmd)
	groupCmd.AddCommand(groupRuntimeCmd)
	groupCmd.AddCommand(groupAgentCmd)
	groupCmd.AddCommand(groupProjectCmd)

	groupMemberCmd.AddCommand(groupMemberListCmd)
	groupMemberCmd.AddCommand(groupMemberAddCmd)
	groupMemberCmd.AddCommand(groupMemberRemoveCmd)

	groupCapabilityCmd.AddCommand(groupCapabilityListCmd)
	groupCapabilityCmd.AddCommand(groupCapabilitySetCmd)
	groupCapabilityCmd.AddCommand(groupCapabilityRemoveCmd)

	groupRuntimeCmd.AddCommand(groupRuntimeListCmd)
	groupRuntimeCmd.AddCommand(groupRuntimeAddCmd)
	groupRuntimeCmd.AddCommand(groupRuntimeRemoveCmd)

	groupAgentCmd.AddCommand(groupAgentListCmd)
	groupAgentCmd.AddCommand(groupAgentAddCmd)
	groupAgentCmd.AddCommand(groupAgentRemoveCmd)

	groupProjectCmd.AddCommand(groupProjectListCmd)
	groupProjectCmd.AddCommand(groupProjectAddCmd)
	groupProjectCmd.AddCommand(groupProjectRemoveCmd)

	// group list / get / create / update / delete
	groupListCmd.Flags().String("output", "table", "Output format: table or json")
	groupListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	groupGetCmd.Flags().String("output", "json", "Output format: table or json")

	groupCreateCmd.Flags().String("name", "", "Group name (required)")
	groupCreateCmd.Flags().String("description", "", "Optional description")
	groupCreateCmd.Flags().String("output", "json", "Output format: table or json")

	groupUpdateCmd.Flags().String("name", "", "New name")
	groupUpdateCmd.Flags().String("description", "", "New description (use empty string to clear)")
	groupUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	groupDeleteCmd.Flags().String("output", "table", "Output format: table or json")

	// member list / add / remove
	groupMemberListCmd.Flags().String("output", "table", "Output format: table or json")
	groupMemberListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	groupMemberAddCmd.Flags().String("user", "", "Member name or user UUID (required)")
	groupMemberAddCmd.Flags().String("output", "json", "Output format: table or json")
	groupMemberRemoveCmd.Flags().String("output", "table", "Output format: table or json")

	// capability list / set / remove
	groupCapabilityListCmd.Flags().String("output", "table", "Output format: table or json")
	groupCapabilitySetCmd.Flags().String("output", "json", "Output format: table or json")
	groupCapabilityRemoveCmd.Flags().String("output", "table", "Output format: table or json")

	// runtime list / add / remove
	groupRuntimeListCmd.Flags().String("output", "table", "Output format: table or json")
	groupRuntimeListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	groupRuntimeAddCmd.Flags().String("output", "json", "Output format: table or json")
	groupRuntimeRemoveCmd.Flags().String("output", "table", "Output format: table or json")

	// agent list / add / remove
	groupAgentListCmd.Flags().String("output", "table", "Output format: table or json")
	groupAgentListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	groupAgentAddCmd.Flags().String("output", "json", "Output format: table or json")
	groupAgentRemoveCmd.Flags().String("output", "table", "Output format: table or json")

	// project list / add / remove
	groupProjectListCmd.Flags().String("output", "table", "Output format: table or json")
	groupProjectListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	groupProjectAddCmd.Flags().String("output", "json", "Output format: table or json")
	groupProjectRemoveCmd.Flags().String("output", "table", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func runGroupList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace ID is required: use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groups, err := fetchGroupList(ctx, client)
	if err != nil {
		return fmt.Errorf("list groups: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, groups)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "DESCRIPTION", "CREATED"}
	rows := make([][]string, 0, len(groups))
	for _, g := range groups {
		desc := strVal(g, "description")
		created := strVal(g, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		rows = append(rows, []string{
			displayID(strVal(g, "id"), fullID),
			strVal(g, "name"),
			desc,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runGroupGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	var group map[string]any
	if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(groupRef.ID), &group); err != nil {
		return fmt.Errorf("get group: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "DESCRIPTION", "CREATED"}
		rows := [][]string{{
			strVal(group, "id"),
			strVal(group, "name"),
			strVal(group, "description"),
			strVal(group, "created_at"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, group)
}

func runGroupCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace ID is required: use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"name": name}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}

	var result map[string]any
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/groups"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("create group: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "DESCRIPTION"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "name"),
			strVal(result, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runGroupUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name or --description")
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/groups/"+url.PathEscape(groupRef.ID), body, &result); err != nil {
		return fmt.Errorf("update group: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "DESCRIPTION"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "name"),
			strVal(result, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runGroupDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/groups/"+url.PathEscape(groupRef.ID)); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Group %s deleted.\n", groupRef.Display)
	return nil
}

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

func runGroupMemberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(groupRef.ID)+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"USER ID", "NAME", "EMAIL", "ADDED"}
	rows := make([][]string, 0, len(members))
	for _, m := range members {
		added := strVal(m, "added_at")
		if len(added) >= 10 {
			added = added[:10]
		}
		rows = append(rows, []string{
			displayID(strVal(m, "user_id"), fullID),
			strVal(m, "user_name"),
			strVal(m, "user_email"),
			added,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runGroupMemberAdd(cmd *cobra.Command, args []string) error {
	user, _ := cmd.Flags().GetString("user")
	if strings.TrimSpace(user) == "" {
		return fmt.Errorf("--user is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	userID, err := resolveWorkspaceMemberID(ctx, client, user)
	if err != nil {
		return fmt.Errorf("resolve user: %w", err)
	}

	body := map[string]any{"user_id": userID}
	var result map[string]any
	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/members"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"USER ID", "NAME", "EMAIL"}
		rows := [][]string{{
			strVal(result, "user_id"),
			strVal(result, "user_name"),
			strVal(result, "user_email"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runGroupMemberRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	userID, err := resolveWorkspaceMemberID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve user: %w", err)
	}

	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/members/" + url.PathEscape(userID)
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	fmt.Fprintf(os.Stderr, "User %s removed from group %s.\n", userID, groupRef.Display)
	return nil
}

// ---------------------------------------------------------------------------
// Capabilities
// ---------------------------------------------------------------------------

func runGroupCapabilityList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	var rows []map[string]any
	if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(groupRef.ID)+"/capabilities", &rows); err != nil {
		return fmt.Errorf("list capabilities: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	headers := []string{"CAPABILITY", "GRANTED BY", "GRANTED AT"}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		granted := strVal(r, "granted_at")
		if len(granted) >= 10 {
			granted = granted[:10]
		}
		out = append(out, []string{
			strVal(r, "capability"),
			strVal(r, "granted_by"),
			granted,
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
	return nil
}

func runGroupCapabilitySet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	capability := strings.TrimSpace(args[1])
	if capability == "" {
		return fmt.Errorf("capability is required")
	}

	body := map[string]any{"capability": capability}
	var result map[string]any
	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/capabilities"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("set capability: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout,
			[]string{"CAPABILITY", "GRANTED BY"},
			[][]string{{strVal(result, "capability"), strVal(result, "granted_by")}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runGroupCapabilityRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	capability := strings.TrimSpace(args[1])
	if capability == "" {
		return fmt.Errorf("capability is required")
	}

	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/capabilities/" + url.PathEscape(capability)
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove capability: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Capability %s revoked from group %s.\n", capability, groupRef.Display)
	return nil
}

// ---------------------------------------------------------------------------
// Runtimes
// ---------------------------------------------------------------------------

func runGroupRuntimeList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	var rows []map[string]any
	if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(groupRef.ID)+"/runtimes", &rows); err != nil {
		return fmt.Errorf("list runtimes: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"RUNTIME ID", "GRANTED BY", "GRANTED AT"}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		granted := strVal(r, "granted_at")
		if len(granted) >= 10 {
			granted = granted[:10]
		}
		out = append(out, []string{
			displayID(strVal(r, "runtime_id"), fullID),
			strVal(r, "granted_by"),
			granted,
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
	return nil
}

func runGroupRuntimeAdd(cmd *cobra.Command, args []string) error {
	if !uuidRegexp.MatchString(strings.TrimSpace(args[1])) {
		return fmt.Errorf("runtime id must be a UUID; run `multica runtime list` to find it")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	body := map[string]any{"runtime_id": strings.TrimSpace(args[1])}
	var result map[string]any
	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/runtimes"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("add runtime: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout,
			[]string{"RUNTIME ID", "GRANTED BY"},
			[][]string{{strVal(result, "runtime_id"), strVal(result, "granted_by")}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runGroupRuntimeRemove(cmd *cobra.Command, args []string) error {
	if !uuidRegexp.MatchString(strings.TrimSpace(args[1])) {
		return fmt.Errorf("runtime id must be a UUID")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	rtID := strings.TrimSpace(args[1])
	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/runtimes/" + url.PathEscape(rtID)
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove runtime: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Runtime %s removed from group %s.\n", rtID, groupRef.Display)
	return nil
}

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

func runGroupAgentList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	var rows []map[string]any
	if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(groupRef.ID)+"/agents", &rows); err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"AGENT ID", "GRANTED BY", "GRANTED AT"}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		granted := strVal(r, "granted_at")
		if len(granted) >= 10 {
			granted = granted[:10]
		}
		out = append(out, []string{
			displayID(strVal(r, "agent_id"), fullID),
			strVal(r, "granted_by"),
			granted,
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
	return nil
}

func runGroupAgentAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	agentID, err := resolveAgent(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}

	body := map[string]any{"agent_id": agentID}
	var result map[string]any
	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/agents"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("add agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout,
			[]string{"AGENT ID", "GRANTED BY"},
			[][]string{{strVal(result, "agent_id"), strVal(result, "granted_by")}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runGroupAgentRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupRef, err := resolveGroupID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	agentID, err := resolveAgent(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}

	path := "/api/groups/" + url.PathEscape(groupRef.ID) + "/agents/" + url.PathEscape(agentID)
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove agent: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Agent %s removed from group %s.\n", agentID, groupRef.Display)
	return nil
}

// ---------------------------------------------------------------------------
// Project group-access
// ---------------------------------------------------------------------------

func runGroupProjectList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var rows []map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(projectRef.ID)+"/group-access", &rows); err != nil {
		return fmt.Errorf("list project groups: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"GROUP ID", "ADDED BY", "ADDED AT"}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		added := strVal(r, "added_at")
		if len(added) >= 10 {
			added = added[:10]
		}
		out = append(out, []string{
			displayID(strVal(r, "group_id"), fullID),
			strVal(r, "added_by"),
			added,
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
	return nil
}

func runGroupProjectAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	groupRef, err := resolveGroupID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	body := map[string]any{"group_id": groupRef.ID}
	var result map[string]any
	path := "/api/projects/" + url.PathEscape(projectRef.ID) + "/group-access"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("grant project access: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout,
			[]string{"PROJECT ID", "GROUP ID", "ADDED BY"},
			[][]string{{strVal(result, "project_id"), strVal(result, "group_id"), strVal(result, "added_by")}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runGroupProjectRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	groupRef, err := resolveGroupID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	path := "/api/projects/" + url.PathEscape(projectRef.ID) + "/group-access/" + url.PathEscape(groupRef.ID)
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("revoke project access: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Group %s removed from project %s.\n", groupRef.Display, projectRef.Display)
	return nil
}

// ---------------------------------------------------------------------------
// Resolvers
// ---------------------------------------------------------------------------

// resolveGroupID accepts a UUID, UUID prefix, or group name. Falls through to
// the shared prefix resolver for partial UUIDs and matches by name otherwise.
func resolveGroupID(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return resolvedID{}, fmt.Errorf("group id or name is required")
	}
	if uuidRegexp.MatchString(trimmed) {
		return resolvedID{ID: trimmed, Display: trimmed}, nil
	}

	groups, err := fetchGroupList(ctx, client)
	if err != nil {
		return resolvedID{}, fmt.Errorf("list groups: %w", err)
	}

	// Try exact / case-insensitive name match first so users can pass plain
	// names like "engineering" without colliding with the UUID-prefix logic.
	var nameMatches []idCandidate
	inputLower := strings.ToLower(trimmed)
	for _, g := range groups {
		name := strVal(g, "name")
		if strings.EqualFold(name, trimmed) {
			nameMatches = append(nameMatches, idCandidate{
				ID:      strVal(g, "id"),
				Display: name,
			})
		}
	}
	switch len(nameMatches) {
	case 1:
		return resolvedID{ID: nameMatches[0].ID, Display: nameMatches[0].Display}, nil
	case 0:
		// fall through to prefix
	default:
		return resolvedID{}, ambiguousIDPrefixError("group", input, nameMatches)
	}

	// Substring match — same UX as resolveAgent.
	var substring []idCandidate
	for _, g := range groups {
		name := strVal(g, "name")
		if strings.Contains(strings.ToLower(name), inputLower) {
			substring = append(substring, idCandidate{
				ID:      strVal(g, "id"),
				Display: name,
			})
		}
	}
	switch len(substring) {
	case 1:
		return resolvedID{ID: substring[0].ID, Display: substring[0].Display}, nil
	case 0:
		// fall through to prefix
	default:
		return resolvedID{}, ambiguousIDPrefixError("group", input, substring)
	}

	return resolveIDByPrefix(ctx, client, "group", input, fetchGroupCandidates)
}

func fetchGroupList(ctx context.Context, client *cli.APIClient) ([]map[string]any, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace ID is required: use --workspace-id or set MULTICA_WORKSPACE_ID")
	}
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/groups"
	var raw json.RawMessage
	if err := client.GetJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	// Server returns a bare JSON array today; the autopilot scope path uses
	// a {"groups": [...]} wrapper. Accept both for forward-compat.
	var groups []map[string]any
	if err := json.Unmarshal(raw, &groups); err == nil {
		return groups, nil
	}
	var wrapped struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("decode groups: %w", err)
	}
	return wrapped.Groups, nil
}

func fetchGroupCandidates(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
	groups, err := fetchGroupList(ctx, client)
	if err != nil {
		return nil, err
	}
	candidates := make([]idCandidate, 0, len(groups))
	for _, g := range groups {
		candidates = append(candidates, idCandidate{
			ID:      strVal(g, "id"),
			Display: strVal(g, "name"),
		})
	}
	return candidates, nil
}

// resolveWorkspaceMemberID accepts a UUID or a member name and returns the
// canonical user_id. Mirrors resolveAssignee but restricted to members — group
// membership is by user, not by agent.
func resolveWorkspaceMemberID(ctx context.Context, client *cli.APIClient, input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", fmt.Errorf("user is required")
	}
	if uuidRegexp.MatchString(trimmed) {
		return trimmed, nil
	}
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("workspace ID is required to resolve members; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+url.PathEscape(client.WorkspaceID)+"/members", &members); err != nil {
		return "", fmt.Errorf("fetch members: %w", err)
	}

	inputLower := strings.ToLower(trimmed)
	type match struct{ ID, Name string }
	var exact, substring []match
	for _, m := range members {
		name := strVal(m, "name")
		email := strVal(m, "email")
		id := strVal(m, "user_id")
		if id == "" {
			continue
		}
		if strings.EqualFold(name, trimmed) || strings.EqualFold(email, trimmed) {
			exact = append(exact, match{ID: id, Name: name})
			continue
		}
		if strings.Contains(strings.ToLower(name), inputLower) || strings.Contains(strings.ToLower(email), inputLower) {
			substring = append(substring, match{ID: id, Name: name})
		}
	}
	for _, bucket := range [][]match{exact, substring} {
		switch len(bucket) {
		case 0:
			continue
		case 1:
			return bucket[0].ID, nil
		default:
			var parts []string
			for _, m := range bucket {
				parts = append(parts, fmt.Sprintf("  %q (%s)", m.Name, truncateID(m.ID)))
			}
			return "", fmt.Errorf("ambiguous member %q; matches:\n%s", input, strings.Join(parts, "\n"))
		}
	}
	return "", fmt.Errorf("no member found matching %q", input)
}
