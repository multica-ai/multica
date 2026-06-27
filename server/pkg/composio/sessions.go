package composio

import (
	"context"
	"errors"
	"net/http"
)

// --- Session creation ---------------------------------------------------

// CreateSessionRequest is the body of POST /tool_router/session.
//
// The minimum required field is [UserID]. Everything else is optional and
// maps directly to the v3.1 wire schema:
// https://docs.composio.dev/reference/api-reference/tool-router/postToolRouterSession
//
// The schema is intentionally typed loosely (map-based) for the nested
// `toolkits`, `auth_configs`, `tools`, `tags`, `multi_account`, etc. fields
// because they carry many child attributes and are expected to evolve.
// Callers can still construct strongly typed wrappers on top.
type CreateSessionRequest struct {
	UserID            string             `json:"user_id"`
	Toolkits          map[string]any     `json:"toolkits,omitempty"`
	AuthConfigs       map[string]any     `json:"auth_configs,omitempty"`
	ConnectedAccounts map[string]any     `json:"connected_accounts,omitempty"`
	ManageConnections *ManageConnections `json:"manage_connections,omitempty"`
	Tools             map[string]any     `json:"tools,omitempty"`
	Tags              any                `json:"tags,omitempty"`
	Workbench         map[string]any     `json:"workbench,omitempty"`
	MultiAccount      map[string]any     `json:"multi_account,omitempty"`
	Preload           map[string]any     `json:"preload,omitempty"`
	Search            map[string]any     `json:"search,omitempty"`
	Execute           map[string]any     `json:"execute,omitempty"`
	Experimental      map[string]any     `json:"experimental,omitempty"`
}

// ManageConnections is the typed flavor of the `manage_connections` object —
// the field used most often by integrations.
type ManageConnections struct {
	Enable                   *bool  `json:"enable,omitempty"`
	CallbackURL              string `json:"callback_url,omitempty"`
	EnableWaitForConnections *bool  `json:"enable_wait_for_connections,omitempty"`
	EnableConnectionRemoval  *bool  `json:"enable_connection_removal,omitempty"`
}

// MCPDescriptor is the streamable HTTP entrypoint for the session's MCP.
type MCPDescriptor struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// CreateSessionResponse mirrors the subset of the upstream response the SDK
// currently exposes typed. Additional fields can be added without breaking
// callers.
type CreateSessionResponse struct {
	SessionID       string           `json:"session_id"`
	MCP             MCPDescriptor    `json:"mcp"`
	ToolRouterTools []string         `json:"tool_router_tools,omitempty"`
	Config          map[string]any   `json:"config,omitempty"`
	ConfigVersion   int              `json:"config_version,omitempty"`
	Experimental    map[string]any   `json:"experimental,omitempty"`
	Warnings        []SessionWarning `json:"warnings,omitempty"`
}

// SessionWarning is a non-fatal warning emitted at session creation time.
type SessionWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CreateSession opens a new tool-router (a.k.a. MCP) session for the given
// user. The returned [CreateSessionResponse.MCP.URL] is the URL an
// MCP-compatible client connects to.
//
// Use [Client.MCPAuthHeaders] to obtain the matching headers — the SDK
// returns these separately rather than baking them into the response so
// that callers don't accidentally leak the secret API key through logs.
func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	if req.UserID == "" {
		return nil, errors.New("composio: CreateSession: UserID is required")
	}
	var out CreateSessionResponse
	if err := c.do(c.newRequest(ctx).SetBody(req), http.MethodPost, "/tool_router/session", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MCPAuthHeaders returns the headers an MCP client must send when connecting
// to a session URL produced by [Client.CreateSession].
//
// Composio authenticates MCP streaming the same way it authenticates the
// REST API — with the project's `x-api-key` header. Keeping this as a
// dedicated helper makes it explicit at the call site that bearer
// material is leaving the SDK boundary, so callers can route it through
// their secret-redact pipeline (see server/pkg/redact).
func (c *Client) MCPAuthHeaders() map[string]string {
	return c.APIKeyHeader()
}
