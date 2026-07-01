package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "Work with teams",
}

var teamListCmd = &cobra.Command{
	Use:   "list",
	Short: "List teams in the workspace",
	Args:  cobra.NoArgs,
	RunE:  runTeamList,
}

var teamCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new team",
	Args:  cobra.NoArgs,
	RunE:  runTeamCreate,
}

var teamUpdateCmd = &cobra.Command{
	Use:   "update <team-id-or-key>",
	Short: "Update a team",
	Args:  exactArgs(1),
	RunE:  runTeamUpdate,
}

var teamArchiveCmd = &cobra.Command{
	Use:   "archive <team-id-or-key>",
	Short: "Archive a team",
	Args:  exactArgs(1),
	RunE:  runTeamArchive,
}

type teamCLIResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Name         string  `json:"name"`
	Key          string  `json:"key"`
	Description  string  `json:"description"`
	Icon         *string `json:"icon"`
	IssueCounter int32   `json:"issue_counter"`
	IsDefault    bool    `json:"is_default"`
	ArchivedAt   *string `json:"archived_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type teamCLIListResponse struct {
	Teams []teamCLIResponse `json:"teams"`
	Total int               `json:"total"`
}

func init() {
	teamCmd.AddCommand(teamListCmd)
	teamCmd.AddCommand(teamCreateCmd)
	teamCmd.AddCommand(teamUpdateCmd)
	teamCmd.AddCommand(teamArchiveCmd)

	teamListCmd.Flags().String("output", "table", "Output format: table or json")
	teamListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	teamCreateCmd.Flags().String("name", "", "Team name (required)")
	teamCreateCmd.Flags().String("key", "", "Team key / issue prefix")
	teamCreateCmd.Flags().String("description", "", "Team description")
	teamCreateCmd.Flags().String("icon", "", "Team icon")
	teamCreateCmd.Flags().String("output", "json", "Output format: table or json")

	teamUpdateCmd.Flags().String("name", "", "New team name")
	teamUpdateCmd.Flags().String("key", "", "New Team key / issue prefix")
	teamUpdateCmd.Flags().String("description", "", "New team description")
	teamUpdateCmd.Flags().String("icon", "", "New team icon")
	teamUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	teamArchiveCmd.Flags().String("output", "json", "Output format: table or json")
}

func teamCollectionPath(client *cli.APIClient) string {
	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if len(params) == 0 {
		return "/api/teams"
	}
	return "/api/teams?" + params.Encode()
}

func runTeamList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result teamCLIListResponse
	if err := client.GetJSON(ctx, teamCollectionPath(client), &result); err != nil {
		return fmt.Errorf("list teams: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result.Teams)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "KEY", "NAME", "ISSUES", "STATE"}
	rows := make([][]string, 0, len(result.Teams))
	for _, team := range result.Teams {
		rows = append(rows, []string{
			displayID(team.ID, fullID),
			team.Key,
			team.Name,
			fmt.Sprintf("%d", team.IssueCounter),
			teamState(team),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTeamCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}

	body := map[string]any{"name": strings.TrimSpace(name)}
	if key, _ := cmd.Flags().GetString("key"); strings.TrimSpace(key) != "" {
		body["key"] = strings.TrimSpace(key)
	}
	if desc, _ := cmd.Flags().GetString("description"); desc != "" {
		body["description"] = desc
	}
	if icon, _ := cmd.Flags().GetString("icon"); icon != "" {
		body["icon"] = icon
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result teamCLIResponse
	if err := client.PostJSON(ctx, "/api/teams", body, &result); err != nil {
		return fmt.Errorf("create team: %w", err)
	}
	return printTeamResult(cmd, result)
}

func runTeamUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body["name"] = strings.TrimSpace(name)
	}
	if cmd.Flags().Changed("key") {
		key, _ := cmd.Flags().GetString("key")
		body["key"] = strings.TrimSpace(key)
	}
	if cmd.Flags().Changed("description") {
		desc, _ := cmd.Flags().GetString("description")
		body["description"] = desc
	}
	if cmd.Flags().Changed("icon") {
		icon, _ := cmd.Flags().GetString("icon")
		body["icon"] = icon
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update; pass --name, --key, --description, or --icon")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	teamID, err := resolveTeamRef(ctx, client, args[0])
	if err != nil {
		return err
	}

	var result teamCLIResponse
	if err := client.PatchJSON(ctx, "/api/teams/"+url.PathEscape(teamID), body, &result); err != nil {
		return fmt.Errorf("update team: %w", err)
	}
	return printTeamResult(cmd, result)
}

func runTeamArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	teamID, err := resolveTeamRef(ctx, client, args[0])
	if err != nil {
		return err
	}

	var result teamCLIResponse
	if err := client.DeleteJSONResponse(ctx, "/api/teams/"+url.PathEscape(teamID), &result); err != nil {
		return fmt.Errorf("archive team: %w", err)
	}
	return printTeamResult(cmd, result)
}

func resolveTeamRef(ctx context.Context, client *cli.APIClient, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("team reference is required")
	}
	var result teamCLIListResponse
	if err := client.GetJSON(ctx, teamCollectionPath(client), &result); err != nil {
		return "", fmt.Errorf("list teams: %w", err)
	}
	for _, team := range result.Teams {
		if team.ID == ref || strings.EqualFold(team.Key, ref) {
			return team.ID, nil
		}
	}
	return "", fmt.Errorf("team %q not found", ref)
}

func resolveTeamRefs(ctx context.Context, client *cli.APIClient, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		id, err := resolveTeamRef(ctx, client, ref)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func printTeamResult(cmd *cobra.Command, team teamCLIResponse) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"ID", "KEY", "NAME", "ISSUES", "STATE"}, [][]string{{
			team.ID,
			team.Key,
			team.Name,
			fmt.Sprintf("%d", team.IssueCounter),
			teamState(team),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, team)
}

func teamState(team teamCLIResponse) string {
	var parts []string
	if team.IsDefault {
		parts = append(parts, "default")
	}
	if team.ArchivedAt != nil && *team.ArchivedAt != "" {
		parts = append(parts, "archived")
	}
	if len(parts) == 0 {
		return "active"
	}
	return strings.Join(parts, ",")
}
