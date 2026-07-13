// CEREBRO-PATCH(cerebro-connections-admin-mcp): FIR-2835 cerebro-only file —
// workspace connection registry admin MCP tools.
package clitools

import (
	"context"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerCerebroConnectionAdminTools wires MCP tools for the workspace
// connection registry (server/internal/cerebro/connections). It mirrors
// `multica connection ...` in cerebro_connection.go so an agent can create and
// administer connections over MCP with the same surface as the human CLI.
//
// A connection is a named HTTP MCP server (type mcp_http) or a permission-gated
// REST API (type api) that every runtime/agent in the workspace may call,
// subject to the tool-policy chain. Set internal=true to reach a service on the
// runtime's private network (e.g. http://svc.internal:3000/api/mcp).
//
// These are DISTINCT from registerConnectionTools, which exposes the individual
// endpoints INSIDE a connection as callable tools. These tools manage the
// connection rows themselves. All routes are workspace-scoped; writes are gated
// server-side on the manage_connections capability.
func registerCerebroConnectionAdminTools(srv *mcp.Server, client *cli.APIClient) {
	connectionsPath := func() (string, bool) {
		if client.WorkspaceID == "" {
			return "", false
		}
		return "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/connections", true
	}

	srv.RegisterTool(mcp.Tool{
		Name:        "list_connections",
		Description: "List the workspace's connections (external HTTP MCP servers and permission-gated REST APIs). Returns id, name, type, url, internal, and enabled for each.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		base, ok := connectionsPath()
		if !ok {
			return mcp.ErrorResult("workspace_id required: set MULTICA_WORKSPACE_ID"), nil
		}
		var result any
		if err := client.GetJSON(ctx, base, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "get_connection",
		Description: "Get one workspace connection by UUID. Auth secrets are returned masked.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"connection_id"},
			"properties": map[string]any{
				"connection_id": map[string]any{"type": "string", "description": "Connection UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		base, ok := connectionsPath()
		if !ok {
			return mcp.ErrorResult("workspace_id required: set MULTICA_WORKSPACE_ID"), nil
		}
		id, err := requireString(args, "connection_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, base+"/"+url.PathEscape(id), &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name: "create_connection",
		Description: "Create a workspace connection. Requires the manage_connections capability. " +
			"Set type=mcp_http for an HTTP MCP server or type=api for a permission-gated REST API. " +
			"Set internal=true to reach the URL over the runtime's private network (e.g. http://svc.internal:3000/api/mcp) instead of the public internet.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"name", "url"},
			"properties": map[string]any{
				"name":             map[string]any{"type": "string", "description": "Slug: 1-64 chars, lowercase alphanumeric, hyphen or underscore. Unique per workspace."},
				"display_name":     map[string]any{"type": "string", "description": "Human-readable name shown in the UI. Defaults to name if omitted."},
				"type":             map[string]any{"type": "string", "description": "mcp_http (default) or api", "enum": []string{"mcp_http", "api"}},
				"url":              map[string]any{"type": "string", "description": "Base URL of the MCP server or API"},
				"internal":         map[string]any{"type": "boolean", "description": "Reach the URL over the runtime's private network instead of the public internet"},
				"auth_token":       map[string]any{"type": "string", "description": "Bearer token sent as Authorization: Bearer <token>"},
				"api_key":          map[string]any{"type": "string", "description": "API key value"},
				"api_key_header":   map[string]any{"type": "string", "description": "Header name to send the API key under (e.g. X-API-Key)"},
				"cf_access_id":     map[string]any{"type": "string", "description": "Cloudflare Access service-token client ID"},
				"cf_access_secret": map[string]any{"type": "string", "description": "Cloudflare Access service-token client secret"},
				"default_access":   map[string]any{"type": "string", "description": "Baseline verdict for actors with no explicit rule: allow, ask, or deny (default deny)", "enum": []string{"allow", "ask", "deny"}},
				"on_behalf_of":     map[string]any{"type": "boolean", "description": "type=api connections only: stamp the calling agent's identity onto every dispatch as X-On-Behalf-Of: agent:<uuid>, so the remote API authorizes the call as that agent's own delegation grant instead of the shared connection key (FIR-2668)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		base, ok := connectionsPath()
		if !ok {
			return mcp.ErrorResult("workspace_id required: set MULTICA_WORKSPACE_ID"), nil
		}
		name, err := requireString(args, "name")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		connURL, err := requireString(args, "url")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		displayName := optString(args, "display_name")
		if displayName == "" {
			displayName = name
		}
		connType := optString(args, "type")
		if connType == "" {
			connType = "mcp_http"
		}
		body := map[string]any{
			"name":         name,
			"display_name": displayName,
			"type":         connType,
			"url":          connURL,
			"internal":     optBool(args, "internal", false),
			"auth_config":  connectionAuthFromArgs(args),
		}
		if v := optString(args, "default_access"); v != "" {
			body["default_access"] = v
		}
		var result any
		if err := client.PostJSON(ctx, base, body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name: "update_connection",
		Description: "Update a workspace connection. Requires the manage_connections capability. Only provided fields change; " +
			"omitted fields keep their stored value and unset auth secrets are preserved.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"connection_id"},
			"properties": map[string]any{
				"connection_id":    map[string]any{"type": "string", "description": "Connection UUID"},
				"display_name":     map[string]any{"type": "string", "description": "Human-readable name shown in the UI"},
				"url":              map[string]any{"type": "string", "description": "Base URL of the MCP server or API"},
				"internal":         map[string]any{"type": "boolean", "description": "Reach the URL over the runtime's private network"},
				"enabled":          map[string]any{"type": "boolean", "description": "Whether the connection is enabled"},
				"auth_token":       map[string]any{"type": "string", "description": "New bearer token (leave unset to keep the stored one)"},
				"api_key":          map[string]any{"type": "string", "description": "New API key value"},
				"api_key_header":   map[string]any{"type": "string", "description": "Header name to send the API key under"},
				"cf_access_id":     map[string]any{"type": "string", "description": "Cloudflare Access service-token client ID"},
				"cf_access_secret": map[string]any{"type": "string", "description": "Cloudflare Access service-token client secret"},
				"default_access":   map[string]any{"type": "string", "description": "allow, ask, or deny", "enum": []string{"allow", "ask", "deny"}},
				"on_behalf_of":     map[string]any{"type": "boolean", "description": "type=api connections only: stamp the calling agent's identity onto every dispatch as X-On-Behalf-Of: agent:<uuid>, so the remote API authorizes the call as that agent's own delegation grant instead of the shared connection key (FIR-2668)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		base, ok := connectionsPath()
		if !ok {
			return mcp.ErrorResult("workspace_id required: set MULTICA_WORKSPACE_ID"), nil
		}
		id, err := requireString(args, "connection_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		// Full PUT: fetch current row, apply provided fields, send merged object.
		var stored map[string]any
		if err := client.GetJSON(ctx, base+"/"+url.PathEscape(id), &stored); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{
			"display_name":         stored["display_name"],
			"url":                  stored["url"],
			"internal":             stored["internal"],
			"auth_config":          stored["auth_config"],
			"endpoint_permissions": stored["endpoint_permissions"],
			"scopable_args":        stored["scopable_args"],
			"enabled":              stored["enabled"],
			"default_access":       stored["default_access"],
		}
		if v, ok := args["display_name"]; ok {
			body["display_name"] = v
		}
		if v, ok := args["url"]; ok {
			body["url"] = v
		}
		if v, ok := args["internal"]; ok {
			body["internal"] = v
		}
		if v, ok := args["enabled"]; ok {
			body["enabled"] = v
		}
		if v := optString(args, "default_access"); v != "" {
			body["default_access"] = v
		}
		if auth := connectionAuthFromArgs(args); len(auth) > 0 {
			merged := map[string]any{}
			if existing, ok := stored["auth_config"].(map[string]any); ok {
				for k, v := range existing {
					merged[k] = v
				}
			}
			for k, v := range auth {
				merged[k] = v
			}
			body["auth_config"] = merged
		}
		var result any
		if err := client.PutJSON(ctx, base+"/"+url.PathEscape(id), body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "delete_connection",
		Description: "Delete a workspace connection by UUID. Requires the manage_connections capability.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"connection_id"},
			"properties": map[string]any{
				"connection_id": map[string]any{"type": "string", "description": "Connection UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		base, ok := connectionsPath()
		if !ok {
			return mcp.ErrorResult("workspace_id required: set MULTICA_WORKSPACE_ID"), nil
		}
		id, err := requireString(args, "connection_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := client.DeleteJSON(ctx, base+"/"+url.PathEscape(id)); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"deleted": id})
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "test_connection",
		Description: "Probe a URL for reachability and (for mcp_http) discover its tools WITHOUT saving a connection. Requires the manage_connections capability. Set internal=true to probe over the runtime's private network.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"url"},
			"properties": map[string]any{
				"url":              map[string]any{"type": "string", "description": "URL to probe"},
				"type":             map[string]any{"type": "string", "description": "mcp_http (default) or api", "enum": []string{"mcp_http", "api"}},
				"internal":         map[string]any{"type": "boolean", "description": "Probe over the runtime's private network"},
				"auth_token":       map[string]any{"type": "string", "description": "Bearer token"},
				"api_key":          map[string]any{"type": "string", "description": "API key value"},
				"api_key_header":   map[string]any{"type": "string", "description": "Header name for the API key"},
				"cf_access_id":     map[string]any{"type": "string", "description": "Cloudflare Access client ID"},
				"cf_access_secret": map[string]any{"type": "string", "description": "Cloudflare Access client secret"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		base, ok := connectionsPath()
		if !ok {
			return mcp.ErrorResult("workspace_id required: set MULTICA_WORKSPACE_ID"), nil
		}
		connURL, err := requireString(args, "url")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		connType := optString(args, "type")
		if connType == "" {
			connType = "mcp_http"
		}
		body := map[string]any{
			"url":         connURL,
			"type":        connType,
			"internal":    optBool(args, "internal", false),
			"auth_config": connectionAuthFromArgs(args),
		}
		var result any
		if err := client.PostJSON(ctx, base+"/test", body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})
}

// connectionAuthFromArgs assembles the auth_config object from whichever
// credential arguments were supplied. Omitted fields are left out so an update
// preserves stored secrets.
func connectionAuthFromArgs(args map[string]any) map[string]any {
	auth := map[string]any{}
	if v := optString(args, "auth_token"); v != "" {
		auth["bearer_token"] = v
	}
	if v := optString(args, "api_key"); v != "" {
		auth["api_key"] = v
	}
	if v := optString(args, "api_key_header"); v != "" {
		auth["api_key_header"] = v
	}
	if v := optString(args, "cf_access_id"); v != "" {
		auth["cf_access_id"] = v
	}
	if v := optString(args, "cf_access_secret"); v != "" {
		auth["cf_access_secret"] = v
	}
	if v, ok := args["on_behalf_of"].(bool); ok {
		auth["on_behalf_of"] = map[string]any{"enabled": v}
	}
	return auth
}
