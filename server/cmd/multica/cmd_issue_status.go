package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue-status {list|get|create|update|archive} — the workspace status
// catalog. A status is workspace-shared workflow configuration: every workflow
// picks from the same catalog, and archiving one is refused while any workflow
// still uses it. `multica workflow` DECLARES statuses through its steps file;
// these commands edit the catalog itself, including statuses a workspace
// synced before the declaration carried all of their fields.
//
// The command is top-level rather than `multica workflow status` because the
// resource is not owned by a workflow: the API routes it at /api/issue-statuses
// and a subcommand would take no workflow argument. The one cost is the hyphen
// against `multica issue status <id> <status>`, which changes an issue's
// CURRENT status; the Long help names that other command.
//
// Statuses are addressed by key, name, or id; the CLI resolves name/key to the
// id the API expects. Icon values are validated by the server (presentation
// only: dotted, circle, half, three_quarters, check, slash, cross; empty keeps
// the category default).

type issueStatusDTO struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Color       string  `json:"color"`
	Icon        string  `json:"icon"`
	IsSystem    bool    `json:"is_system"`
	Position    float64 `json:"position"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

var issueStatusCatalogCmd = &cobra.Command{
	Use:   "issue-status",
	Short: "Manage the workspace status catalog (statuses and their icons)",
	Long: `Manage the workspace status catalog: the statuses every workflow picks from.

This edits the catalog. To change an issue's CURRENT status, use
'multica issue status <issue-id> <status>'. Mutations require workspace
owner/admin; reading is open to every member.

Icon values are presentation-only: dotted, circle, half, three_quarters,
check, slash, cross. Empty keeps the category default.`,
}

var issueStatusCatalogListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the workspace status catalog",
	Args:  exactArgs(0),
	RunE:  runIssueStatusList,
}

var issueStatusCatalogGetCmd = &cobra.Command{
	Use:   "get <key-or-name>",
	Short: "Show one status",
	Args:  exactArgs(1),
	RunE:  runIssueStatusGet,
}

var issueStatusCatalogCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a status (workspace owner/admin only)",
	Long: `Create a custom status in the workspace catalog.

  multica issue-status create --name "Spec review" --category started \
      --color "#3b82f6" --icon three_quarters

The key is derived from the name; pass --key to pin it. Icon is optional and
presentation-only. Creating a status also adds it to the Default workflow.`,
	Args: exactArgs(0),
	RunE: runIssueStatusCreate,
}

var issueStatusCatalogUpdateCmd = &cobra.Command{
	Use:   "update <key-or-name>",
	Short: "Update a status's name, description, color, icon, or position (owner/admin only)",
	Long: `Update a custom status's presentation. Category and key are immutable.

  multica issue-status update spec_review --icon three_quarters
  multica issue-status update spec_review --icon ""   # back to the category default

Built-in statuses cannot be modified.`,
	Args: exactArgs(1),
	RunE: runIssueStatusUpdate,
}

var issueStatusCatalogArchiveCmd = &cobra.Command{
	Use:   "archive <key-or-name>",
	Short: "Archive a status (owner/admin only; refused while a workflow uses it)",
	Args:  exactArgs(1),
	RunE:  runIssueStatusArchive,
}

func init() {
	issueStatusCatalogCmd.AddCommand(issueStatusCatalogListCmd)
	issueStatusCatalogCmd.AddCommand(issueStatusCatalogGetCmd)
	issueStatusCatalogCmd.AddCommand(issueStatusCatalogCreateCmd)
	issueStatusCatalogCmd.AddCommand(issueStatusCatalogUpdateCmd)
	issueStatusCatalogCmd.AddCommand(issueStatusCatalogArchiveCmd)

	issueStatusCatalogListCmd.Flags().String("output", "table", "Output format: table or json")
	issueStatusCatalogListCmd.Flags().Bool("include-archived", false, "Include archived statuses")
	issueStatusCatalogGetCmd.Flags().String("output", "table", "Output format: table or json")

	issueStatusCatalogCreateCmd.Flags().String("output", "table", "Output format: table or json")
	issueStatusCatalogCreateCmd.Flags().String("name", "", "Status name (required)")
	issueStatusCatalogCreateCmd.Flags().String("category", "", "Category: backlog, unstarted, started, done, or closed (required)")
	issueStatusCatalogCreateCmd.Flags().String("color", "", "Hex color like #3b82f6 (required)")
	issueStatusCatalogCreateCmd.Flags().String("key", "", "Stable key, derived from --name when omitted; immutable afterwards")
	issueStatusCatalogCreateCmd.Flags().String("description", "", "Status description")
	issueStatusCatalogCreateCmd.Flags().String("icon", "", "Icon: dotted, circle, half, three_quarters, check, slash, cross (empty: category default)")

	issueStatusCatalogUpdateCmd.Flags().String("output", "table", "Output format: table or json")
	issueStatusCatalogUpdateCmd.Flags().String("name", "", "New name")
	issueStatusCatalogUpdateCmd.Flags().String("description", "", "New description")
	issueStatusCatalogUpdateCmd.Flags().String("color", "", "New hex color")
	issueStatusCatalogUpdateCmd.Flags().String("icon", "", "New icon; an empty value resets it to the category default")
	issueStatusCatalogUpdateCmd.Flags().Float64("position", 0, "New ordering position within the category")

	issueStatusCatalogArchiveCmd.Flags().String("output", "table", "Output format: table or json")
}

func fetchIssueStatuses(ctx context.Context, client *cli.APIClient, includeArchived bool) ([]issueStatusDTO, error) {
	path := "/api/issue-statuses"
	if includeArchived {
		path += "?include_archived=true"
	}
	var result struct {
		Statuses []issueStatusDTO `json:"statuses"`
	}
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("list issue statuses: %w", err)
	}
	return result.Statuses, nil
}

// resolveIssueStatusRef matches an id, a key, or a name. Key wins over name
// because the key is what agent instructions and workflows reference; the
// API path uses the id.
func resolveIssueStatusRef(statuses []issueStatusDTO, ref string) (issueStatusDTO, error) {
	trimmed := strings.TrimSpace(ref)
	for _, s := range statuses {
		if s.ID == trimmed {
			return s, nil
		}
	}
	for _, s := range statuses {
		if strings.EqualFold(s.Key, trimmed) {
			return s, nil
		}
	}
	for _, s := range statuses {
		if strings.EqualFold(s.Name, trimmed) {
			return s, nil
		}
	}
	available := make([]string, 0, len(statuses))
	for _, s := range statuses {
		available = append(available, s.Key)
	}
	return issueStatusDTO{}, fmt.Errorf("status %q not found; available: %s", ref, strings.Join(available, ", "))
}

func printIssueStatusTable(statuses []issueStatusDTO) {
	headers := []string{"KEY", "NAME", "CATEGORY", "ICON", "COLOR", "ARCHIVED"}
	rows := make([][]string, 0, len(statuses))
	for _, s := range statuses {
		archived := ""
		if s.ArchivedAt != nil {
			archived = "yes"
		}
		rows = append(rows, []string{s.Key, s.Name, s.Category, s.Icon, s.Color, archived})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func runIssueStatusList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	includeArchived, _ := cmd.Flags().GetBool("include-archived")
	statuses, err := fetchIssueStatuses(ctx, client, includeArchived)
	if err != nil {
		return err
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, statuses)
	}
	printIssueStatusTable(statuses)
	return nil
}

func runIssueStatusGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	// Include archived: "get" must be able to explain a status an issue still
	// sits on, which is exactly the case an archived status produces.
	statuses, err := fetchIssueStatuses(ctx, client, true)
	if err != nil {
		return err
	}
	status, err := resolveIssueStatusRef(statuses, args[0])
	if err != nil {
		return err
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, status)
	}
	printIssueStatusTable([]issueStatusDTO{status})
	return nil
}

func runIssueStatusCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	category, _ := cmd.Flags().GetString("category")
	color, _ := cmd.Flags().GetString("color")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}
	if strings.TrimSpace(category) == "" {
		return fmt.Errorf("--category is required: backlog, unstarted, started, done, or closed")
	}
	if strings.TrimSpace(color) == "" {
		return fmt.Errorf("--color is required, for example #3b82f6")
	}
	key, _ := cmd.Flags().GetString("key")
	description, _ := cmd.Flags().GetString("description")
	icon, _ := cmd.Flags().GetString("icon")

	body := map[string]any{"name": name, "category": category, "color": color}
	if key != "" {
		body["key"] = key
	}
	if description != "" {
		body["description"] = description
	}
	if cmd.Flags().Changed("icon") {
		body["icon"] = icon
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var created issueStatusDTO
	if err := client.PostJSON(ctx, "/api/issue-statuses", body, &created); err != nil {
		return fmt.Errorf("create issue status: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, created)
	}
	fmt.Fprintf(os.Stdout, "Status %q created.\n", created.Name)
	printIssueStatusTable([]issueStatusDTO{created})
	return nil
}

func runIssueStatusUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	statuses, err := fetchIssueStatuses(ctx, client, true)
	if err != nil {
		return err
	}
	status, err := resolveIssueStatusRef(statuses, args[0])
	if err != nil {
		return err
	}

	body := map[string]any{}
	for _, flag := range []string{"name", "description", "color", "icon"} {
		if cmd.Flags().Changed(flag) {
			value, _ := cmd.Flags().GetString(flag)
			body[flag] = value
		}
	}
	if cmd.Flags().Changed("position") {
		position, _ := cmd.Flags().GetFloat64("position")
		body["position"] = position
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update; pass --name, --description, --color, --icon, or --position")
	}

	var updated issueStatusDTO
	if err := client.PatchJSON(ctx, "/api/issue-statuses/"+status.ID, body, &updated); err != nil {
		return fmt.Errorf("update issue status: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, updated)
	}
	fmt.Fprintf(os.Stdout, "Status %q updated.\n", updated.Name)
	printIssueStatusTable([]issueStatusDTO{updated})
	return nil
}

func runIssueStatusArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	statuses, err := fetchIssueStatuses(ctx, client, true)
	if err != nil {
		return err
	}
	status, err := resolveIssueStatusRef(statuses, args[0])
	if err != nil {
		return err
	}

	var archived issueStatusDTO
	if err := client.DeleteJSONResponse(ctx, "/api/issue-statuses/"+status.ID, &archived); err != nil {
		return fmt.Errorf("archive issue status: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, archived)
	}
	fmt.Fprintf(os.Stdout, "Status %q archived.\n", archived.Name)
	printIssueStatusTable([]issueStatusDTO{archived})
	return nil
}
