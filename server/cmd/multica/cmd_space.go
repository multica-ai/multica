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

var spaceCmd = &cobra.Command{
	Use:   "space",
	Short: "Work with spaces",
}

var spaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List spaces in the workspace",
	Args:  cobra.NoArgs,
	RunE:  runSpaceList,
}

var spaceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new space",
	Args:  cobra.NoArgs,
	RunE:  runSpaceCreate,
}

var spaceUpdateCmd = &cobra.Command{
	Use:   "update <space-id-or-key>",
	Short: "Update a space",
	Args:  exactArgs(1),
	RunE:  runSpaceUpdate,
}

var spaceArchiveCmd = &cobra.Command{
	Use:   "archive <space-id-or-key>",
	Short: "Archive a space",
	Args:  exactArgs(1),
	RunE:  runSpaceArchive,
}

type spaceCLIResponse struct {
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

type spaceCLIListResponse struct {
	Spaces []spaceCLIResponse `json:"spaces"`
	Total  int                `json:"total"`
}

func init() {
	spaceCmd.AddCommand(spaceListCmd)
	spaceCmd.AddCommand(spaceCreateCmd)
	spaceCmd.AddCommand(spaceUpdateCmd)
	spaceCmd.AddCommand(spaceArchiveCmd)

	spaceListCmd.Flags().String("output", "table", "Output format: table or json")
	spaceListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	spaceCreateCmd.Flags().String("name", "", "Space name (required)")
	spaceCreateCmd.Flags().String("key", "", "Space key / issue prefix")
	spaceCreateCmd.Flags().String("description", "", "Space description")
	spaceCreateCmd.Flags().String("icon", "", "Space icon")
	spaceCreateCmd.Flags().String("output", "json", "Output format: table or json")

	spaceUpdateCmd.Flags().String("name", "", "New space name")
	spaceUpdateCmd.Flags().String("key", "", "New Space key / issue prefix")
	spaceUpdateCmd.Flags().String("description", "", "New space description")
	spaceUpdateCmd.Flags().String("icon", "", "New space icon")
	spaceUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	spaceArchiveCmd.Flags().String("output", "json", "Output format: table or json")
}

func spaceCollectionPath(client *cli.APIClient) string {
	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if len(params) == 0 {
		return "/api/spaces"
	}
	return "/api/spaces?" + params.Encode()
}

func runSpaceList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result spaceCLIListResponse
	if err := client.GetJSON(ctx, spaceCollectionPath(client), &result); err != nil {
		return fmt.Errorf("list spaces: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result.Spaces)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "KEY", "NAME", "ISSUES", "STATE"}
	rows := make([][]string, 0, len(result.Spaces))
	for _, space := range result.Spaces {
		rows = append(rows, []string{
			displayID(space.ID, fullID),
			space.Key,
			space.Name,
			fmt.Sprintf("%d", space.IssueCounter),
			spaceState(space),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSpaceCreate(cmd *cobra.Command, _ []string) error {
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

	var result spaceCLIResponse
	if err := client.PostJSON(ctx, "/api/spaces", body, &result); err != nil {
		return fmt.Errorf("create space: %w", err)
	}
	return printSpaceResult(cmd, result)
}

func runSpaceUpdate(cmd *cobra.Command, args []string) error {
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

	spaceID, err := resolveSpaceRef(ctx, client, args[0])
	if err != nil {
		return err
	}

	var result spaceCLIResponse
	if err := client.PatchJSON(ctx, "/api/spaces/"+url.PathEscape(spaceID), body, &result); err != nil {
		return fmt.Errorf("update space: %w", err)
	}
	return printSpaceResult(cmd, result)
}

func runSpaceArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	spaceID, err := resolveSpaceRef(ctx, client, args[0])
	if err != nil {
		return err
	}

	var result spaceCLIResponse
	if err := client.DeleteJSONResponse(ctx, "/api/spaces/"+url.PathEscape(spaceID), &result); err != nil {
		return fmt.Errorf("archive space: %w", err)
	}
	return printSpaceResult(cmd, result)
}

func resolveSpaceRef(ctx context.Context, client *cli.APIClient, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("space reference is required")
	}
	var result spaceCLIListResponse
	if err := client.GetJSON(ctx, spaceCollectionPath(client), &result); err != nil {
		return "", fmt.Errorf("list spaces: %w", err)
	}
	for _, space := range result.Spaces {
		if space.ID == ref || strings.EqualFold(space.Key, ref) {
			return space.ID, nil
		}
	}
	return "", fmt.Errorf("space %q not found", ref)
}

func resolveSpaceRefs(ctx context.Context, client *cli.APIClient, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		id, err := resolveSpaceRef(ctx, client, ref)
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

func printSpaceResult(cmd *cobra.Command, space spaceCLIResponse) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"ID", "KEY", "NAME", "ISSUES", "STATE"}, [][]string{{
			space.ID,
			space.Key,
			space.Name,
			fmt.Sprintf("%d", space.IssueCounter),
			spaceState(space),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, space)
}

func spaceState(space spaceCLIResponse) string {
	var parts []string
	if space.IsDefault {
		parts = append(parts, "default")
	}
	if space.ArchivedAt != nil && *space.ArchivedAt != "" {
		parts = append(parts, "archived")
	}
	if len(parts) == 0 {
		return "active"
	}
	return strings.Join(parts, ",")
}
