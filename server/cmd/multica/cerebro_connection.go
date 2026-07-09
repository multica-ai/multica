// CEREBRO-PATCH(cerebro-connections-cli): FIR-2835 cerebro-only file — workspace connection CLI.
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

// connectionCmd is the cerebro `multica connection ...` command tree. It mirrors
// the connection registry API (server/internal/cerebro/connections) so a human
// or an agent can create and administer workspace connections from the command
// line — the same surface the admin UI drives. Writes are gated server-side on
// the manage_connections capability; reads are member-level.
//
// A "connection" is a named HTTP MCP server (type mcp_http) or a permission-gated
// REST API (type api) that every runtime and agent in the workspace can call,
// subject to the tool-policy chain. Use --internal to reach a service on the
// runtime's private network (e.g. http://<service>.internal:PORT/api/mcp)
// instead of the public internet.
var connectionCmd = &cobra.Command{
	Use:     "connection",
	Aliases: []string{"connections", "conn"},
	Short:   "Work with workspace connections (cerebro)",
}

var (
	connectionListCmd = &cobra.Command{
		Use:   "list",
		Short: "List workspace connections",
		RunE:  runConnectionList,
	}
	connectionGetCmd = &cobra.Command{
		Use:   "get <id-or-name>",
		Short: "Get one connection",
		Args:  exactArgs(1),
		RunE:  runConnectionGet,
	}
	connectionCreateCmd = &cobra.Command{
		Use:   "create",
		Short: "Create a workspace connection (manage_connections capability required)",
		RunE:  runConnectionCreate,
	}
	connectionUpdateCmd = &cobra.Command{
		Use:   "update <id-or-name>",
		Short: "Update a connection (only provided flags change)",
		Args:  exactArgs(1),
		RunE:  runConnectionUpdate,
	}
	connectionDeleteCmd = &cobra.Command{
		Use:   "delete <id-or-name>",
		Short: "Delete a connection",
		Args:  exactArgs(1),
		RunE:  runConnectionDelete,
	}
	connectionTestCmd = &cobra.Command{
		Use:   "test",
		Short: "Test connection reachability + MCP tool discovery without saving",
		RunE:  runConnectionTest,
	}
)

func init() {
	connectionCmd.AddCommand(connectionListCmd)
	connectionCmd.AddCommand(connectionGetCmd)
	connectionCmd.AddCommand(connectionCreateCmd)
	connectionCmd.AddCommand(connectionUpdateCmd)
	connectionCmd.AddCommand(connectionDeleteCmd)
	connectionCmd.AddCommand(connectionTestCmd)

	connectionListCmd.Flags().String("output", "table", "Output format: table or json")
	connectionListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	connectionGetCmd.Flags().String("output", "json", "Output format: table or json")

	addConnectionWriteFlags(connectionCreateCmd)
	connectionCreateCmd.Flags().String("name", "", "Connection slug: 1-64 chars, lowercase alphanumeric, hyphen or underscore (required)")
	connectionCreateCmd.Flags().String("type", "mcp_http", "Connection type: mcp_http (HTTP MCP server) or api (permission-gated REST API)")
	connectionCreateCmd.Flags().String("output", "json", "Output format: table or json")

	addConnectionWriteFlags(connectionUpdateCmd)
	connectionUpdateCmd.Flags().Bool("enabled", true, "Whether the connection is enabled")
	connectionUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	connectionDeleteCmd.Flags().String("output", "table", "Output format: table or json")

	connectionTestCmd.Flags().String("url", "", "URL to probe (required)")
	connectionTestCmd.Flags().String("type", "mcp_http", "Connection type: mcp_http or api")
	connectionTestCmd.Flags().Bool("internal", false, "Reach the URL over the runtime's private network")
	connectionTestCmd.Flags().String("auth-token", "", "Bearer token sent as Authorization: Bearer <token>")
	connectionTestCmd.Flags().String("api-key", "", "API key value")
	connectionTestCmd.Flags().String("api-key-header", "", "Header name to send the API key under")
	connectionTestCmd.Flags().String("cf-access-id", "", "Cloudflare Access service-token client ID")
	connectionTestCmd.Flags().String("cf-access-secret", "", "Cloudflare Access service-token client secret")
	connectionTestCmd.Flags().String("output", "json", "Output format: table or json")
}

// addConnectionWriteFlags registers the flags shared by create and update. The
// URL, auth, and internal-path flags mirror the createRequest / updateRequest
// body fields in server/internal/cerebro/connections/handler.go.
func addConnectionWriteFlags(cmd *cobra.Command) {
	cmd.Flags().String("display-name", "", "Human-readable name shown in the UI")
	cmd.Flags().String("url", "", "Base URL of the MCP server or API")
	cmd.Flags().Bool("internal", false, "Reach the URL over the runtime's private network (e.g. http://svc.internal:3000/api/mcp) instead of the public internet")
	cmd.Flags().String("auth-token", "", "Bearer token sent as Authorization: Bearer <token>")
	cmd.Flags().String("api-key", "", "API key value")
	cmd.Flags().String("api-key-header", "", "Header name to send the API key under (e.g. X-API-Key)")
	cmd.Flags().String("cf-access-id", "", "Cloudflare Access service-token client ID")
	cmd.Flags().String("cf-access-secret", "", "Cloudflare Access service-token client secret")
	cmd.Flags().String("default-access", "", "Baseline verdict for actors with no explicit rule: allow, ask, or deny (default deny)")
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func runConnectionList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conns, err := fetchConnectionList(ctx, client)
	if err != nil {
		return fmt.Errorf("list connections: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, conns)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "TYPE", "URL", "INTERNAL", "ENABLED"}
	rows := make([][]string, 0, len(conns))
	for _, c := range conns {
		rows = append(rows, []string{
			displayID(strVal(c, "id"), fullID),
			strVal(c, "name"),
			strVal(c, "type"),
			strVal(c, "url"),
			boolLabel(c["internal"]),
			boolLabel(c["enabled"]),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runConnectionGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := resolveConnection(ctx, client, args[0])
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "TYPE", "URL", "INTERNAL", "ENABLED"}
		rows := [][]string{{
			strVal(conn, "id"),
			strVal(conn, "name"),
			strVal(conn, "type"),
			strVal(conn, "url"),
			boolLabel(conn["internal"]),
			boolLabel(conn["enabled"]),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, conn)
}

func runConnectionCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}
	displayName, _ := cmd.Flags().GetString("display-name")
	if strings.TrimSpace(displayName) == "" {
		displayName = name
	}
	connURL, _ := cmd.Flags().GetString("url")
	if strings.TrimSpace(connURL) == "" {
		return fmt.Errorf("--url is required")
	}
	connType, _ := cmd.Flags().GetString("type")
	internal, _ := cmd.Flags().GetBool("internal")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace ID is required: use --workspace-id or set MULTICA_WORKSPACE_ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := map[string]any{
		"name":         name,
		"display_name": displayName,
		"type":         connType,
		"url":          connURL,
		"internal":     internal,
		"auth_config":  connectionAuthConfig(cmd),
	}
	if v, _ := cmd.Flags().GetString("default-access"); v != "" {
		body["default_access"] = v
	}

	var result map[string]any
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/connections"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("create connection: %w", err)
	}
	return printConnectionResult(cmd, result)
}

func runConnectionUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The API update is a full PUT: fetch the current row, apply only the
	// flags the caller changed, and send the merged object back. Masked auth
	// fields ("***") are preserved server-side unless a new secret is supplied.
	conn, err := resolveConnection(ctx, client, args[0])
	if err != nil {
		return err
	}
	connID := strVal(conn, "id")

	body := map[string]any{
		"display_name":         conn["display_name"],
		"url":                  conn["url"],
		"internal":             conn["internal"],
		"auth_config":          conn["auth_config"],
		"endpoint_permissions": conn["endpoint_permissions"],
		"scopable_args":        conn["scopable_args"],
		"enabled":              conn["enabled"],
		"default_access":       conn["default_access"],
	}
	if cmd.Flags().Changed("display-name") {
		body["display_name"], _ = cmd.Flags().GetString("display-name")
	}
	if cmd.Flags().Changed("url") {
		body["url"], _ = cmd.Flags().GetString("url")
	}
	if cmd.Flags().Changed("internal") {
		body["internal"], _ = cmd.Flags().GetBool("internal")
	}
	if cmd.Flags().Changed("enabled") {
		body["enabled"], _ = cmd.Flags().GetBool("enabled")
	}
	if cmd.Flags().Changed("default-access") {
		body["default_access"], _ = cmd.Flags().GetString("default-access")
	}
	if auth := connectionAuthConfig(cmd); len(auth) > 0 {
		// Merge changed credential fields over the stored (masked) auth_config so
		// unset secrets are preserved by the server's preserveMaskedAuth path.
		merged := map[string]any{}
		if existing, ok := conn["auth_config"].(map[string]any); ok {
			for k, v := range existing {
				merged[k] = v
			}
		}
		for k, v := range auth {
			merged[k] = v
		}
		body["auth_config"] = merged
	}

	var result map[string]any
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/connections/" + url.PathEscape(connID)
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("update connection: %w", err)
	}
	return printConnectionResult(cmd, result)
}

func runConnectionDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := resolveConnection(ctx, client, args[0])
	if err != nil {
		return err
	}
	connID := strVal(conn, "id")

	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/connections/" + url.PathEscape(connID)
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Connection %s deleted.\n", strVal(conn, "name"))
	return nil
}

func runConnectionTest(cmd *cobra.Command, _ []string) error {
	connURL, _ := cmd.Flags().GetString("url")
	if strings.TrimSpace(connURL) == "" {
		return fmt.Errorf("--url is required")
	}
	connType, _ := cmd.Flags().GetString("type")
	internal, _ := cmd.Flags().GetBool("internal")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace ID is required: use --workspace-id or set MULTICA_WORKSPACE_ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := map[string]any{
		"url":         connURL,
		"type":        connType,
		"internal":    internal,
		"auth_config": connectionAuthConfig(cmd),
	}
	var result map[string]any
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/connections/test"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("test connection: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// connectionAuthConfig assembles the auth_config object from the credential
// flags the caller actually set. Unset flags are omitted so an update leaves
// stored secrets untouched.
func connectionAuthConfig(cmd *cobra.Command) map[string]any {
	auth := map[string]any{}
	if v, _ := cmd.Flags().GetString("auth-token"); v != "" {
		auth["bearer_token"] = v
	}
	if v, _ := cmd.Flags().GetString("api-key"); v != "" {
		auth["api_key"] = v
	}
	if v, _ := cmd.Flags().GetString("api-key-header"); v != "" {
		auth["api_key_header"] = v
	}
	if v, _ := cmd.Flags().GetString("cf-access-id"); v != "" {
		auth["cf_access_id"] = v
	}
	if v, _ := cmd.Flags().GetString("cf-access-secret"); v != "" {
		auth["cf_access_secret"] = v
	}
	return auth
}

func printConnectionResult(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "TYPE", "URL", "INTERNAL", "ENABLED"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "name"),
			strVal(result, "type"),
			strVal(result, "url"),
			boolLabel(result["internal"]),
			boolLabel(result["enabled"]),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// fetchConnectionList reads the workspace connections. The handler returns a
// {"connections": [...]} wrapper; a bare array is accepted for forward-compat.
func fetchConnectionList(ctx context.Context, client *cli.APIClient) ([]map[string]any, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace ID is required: use --workspace-id or set MULTICA_WORKSPACE_ID")
	}
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/connections"
	var raw json.RawMessage
	if err := client.GetJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	var wrapped struct {
		Connections []map[string]any `json:"connections"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Connections != nil {
		return wrapped.Connections, nil
	}
	var conns []map[string]any
	if err := json.Unmarshal(raw, &conns); err != nil {
		return nil, fmt.Errorf("decode connections: %w", err)
	}
	return conns, nil
}

// resolveConnection accepts a connection UUID or its unique name and returns the
// full connection object. Names are unique per workspace, so a name match is
// unambiguous; a UUID is fetched directly.
func resolveConnection(ctx context.Context, client *cli.APIClient, input string) (map[string]any, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("connection id or name is required")
	}
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace ID is required: use --workspace-id or set MULTICA_WORKSPACE_ID")
	}
	if uuidRegexp.MatchString(trimmed) {
		var conn map[string]any
		path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/connections/" + url.PathEscape(trimmed)
		if err := client.GetJSON(ctx, path, &conn); err != nil {
			return nil, fmt.Errorf("get connection: %w", err)
		}
		return conn, nil
	}

	conns, err := fetchConnectionList(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	for _, c := range conns {
		if strings.EqualFold(strVal(c, "name"), trimmed) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no connection found matching %q (pass a UUID or exact name; run `multica connection list`)", input)
}

// boolLabel renders a JSON boolean (which decodes to any) as yes/no for tables.
func boolLabel(v any) string {
	if b, ok := v.(bool); ok && b {
		return "yes"
	}
	return "no"
}
