package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Work with workspaces",
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces you belong to",
	RunE:  runWorkspaceList,
}

var workspaceGetCmd = &cobra.Command{
	Use:   "get [workspace-id]",
	Short: "Get workspace details",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceGet,
}

var workspaceMembersCmd = &cobra.Command{
	Use:   "members [workspace-id]",
	Short: "List workspace members",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceMembers,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMembersCmd)

	workspaceGetCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceMembersCmd.Flags().String("output", "table", "Output format: table or json")
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "No workspaces found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME")
	for _, ws := range workspaces {
		fmt.Fprintf(w, "%s\t%s\n", ws.ID, ws.Name)
	}
	return w.Flush()
}

func workspaceIDFromArgs(cmd *cobra.Command, args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return resolveWorkspaceID(cmd)
}

func runWorkspaceGet(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		desc := strVal(ws, "description")
		if utf8.RuneCountInString(desc) > 60 {
			runes := []rune(desc)
			desc = string(runes[:57]) + "..."
		}
		wsContext := strVal(ws, "context")
		if utf8.RuneCountInString(wsContext) > 60 {
			runes := []rune(wsContext)
			wsContext = string(runes[:57]) + "..."
		}
		headers := []string{"ID", "NAME", "SLUG", "DESCRIPTION", "CONTEXT"}
		rows := [][]string{{
			strVal(ws, "id"),
			strVal(ws, "name"),
			strVal(ws, "slug"),
			desc,
			wsContext,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, ws)
}

func runWorkspaceMembers(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	headers := []string{"USER ID", "NAME", "EMAIL", "ROLE"}
	rows := make([][]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, []string{
			strVal(m, "user_id"),
			strVal(m, "name"),
			strVal(m, "email"),
			strVal(m, "role"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

