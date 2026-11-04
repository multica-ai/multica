package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// ExecuteToolRequest is the body for POST /tools/execute/{tool_slug}.
//
// Spec: https://docs.composio.dev/reference/api-reference/tools/postToolsExecuteByToolSlug
//
// Either ConnectedAccountID or (UserID + the tool's toolkit) is required so
// Composio knows which credential set to use. The SDK does not enforce that
// invariant up front; the upstream returns a 422 with a clear message when
// missing.
type ExecuteToolRequest struct {
	// Arguments is the structured input to the tool. Shape varies per tool.
	Arguments map[string]any `json:"arguments,omitempty"`

	// ConnectedAccountID pins execution to a specific connected account.
	ConnectedAccountID string `json:"connected_account_id,omitempty"`

	// UserID lets Composio resolve the connected account by user when
	// the caller does not have the explicit `ca_` id handy.
	UserID string `json:"user_id,omitempty"`

	// Version pins the tool definition version. Pass "latest" or a dated
	// version like "20251027_00"; defaults to "00000000_00" upstream.
	//
	// The Composio docs note that manual tool execution requires an explicit
	// version; setting this avoids unintended drift when Composio promotes
	// a new latest.
	Version string `json:"version,omitempty"`

	// AllowTracing is the upstream-deprecated debug-tracing flag.
	//
	// Deprecated: marked deprecated on the Composio side (v3.1) — kept here
	// only for backward compatibility with existing callers. Will be removed
	// once Composio drops the field.
	AllowTracing bool `json:"allow_tracing,omitempty"`
}

// ExecuteToolResponse is the typed result. The upstream wire shape varies by
// tool, so [Data] is intentionally generic; callers cast to whatever the
// tool's documented output schema looks like.
type ExecuteToolResponse struct {
	Successful  bool           `json:"successful"`
	Data        map[string]any `json:"data,omitempty"`
	Error       string         `json:"error,omitempty"`
	LogID       string         `json:"log_id,omitempty"`
	SessionInfo map[string]any `json:"session_info,omitempty"`
}

// Tool mirrors the subset of a Composio tool definition the gateway exposes to
// agents: what it is, what it takes, and the hints used to classify it.
//
// Spec: https://docs.composio.dev/reference/v3/api-reference/tools/getTools
type Tool struct {
	Slug            string         `json:"slug"`
	Name            string         `json:"name,omitempty"`
	Description     string         `json:"description,omitempty"`
	InputParameters map[string]any `json:"input_parameters,omitempty"`
	Tags            []string       `json:"tags,omitempty"`
	Toolkit         Toolkit        `json:"toolkit"`
	Version         string         `json:"version,omitempty"`
	IsDeprecated    bool           `json:"is_deprecated,omitempty"`
}

// ListToolsRequest collects the filters of GET /tools.
type ListToolsRequest struct {
	ToolkitSlug string
	Search      string
	// Important limits the result to the toolkit's most-used tools.
	Important bool
	Limit     int
	Cursor    string
}

// ListToolsResponse is the typed paginated response.
type ListToolsResponse struct {
	Items      []Tool `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	TotalItems int    `json:"total_items,omitempty"`
}

// ListTools returns tool definitions, optionally filtered to one toolkit.
func (c *Client) ListTools(ctx context.Context, req ListToolsRequest) (*ListToolsResponse, error) {
	q := url.Values{}
	if req.ToolkitSlug != "" {
		q.Set("toolkit_slug", req.ToolkitSlug)
	}
	if req.Search != "" {
		q.Set("search", req.Search)
	}
	if req.Important {
		q.Set("important", "true")
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}
	q.Set("toolkit_versions", "latest")
	var out ListToolsResponse
	if err := c.do(c.newRequest(ctx), http.MethodGet, "/tools?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTool fetches one tool definition by slug, at its latest version.
func (c *Client) GetTool(ctx context.Context, toolSlug string) (*Tool, error) {
	if toolSlug == "" {
		return nil, errors.New("composio: GetTool: toolSlug is required")
	}
	var out Tool
	if err := c.do(c.newRequest(ctx),
		http.MethodGet, "/tools/"+url.PathEscape(toolSlug)+"?toolkit_versions=latest", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExecuteTool calls a Composio tool by its slug
// (SCREAMING_SNAKE_CASE, e.g. GITHUB_CREATE_ISSUE).
//
// This is the deterministic backend path — it skips MCP/session orchestration
// and is the right call for fixed flows like autopilots or built-in skills.
func (c *Client) ExecuteTool(ctx context.Context, toolSlug string, req ExecuteToolRequest) (*ExecuteToolResponse, error) {
	if toolSlug == "" {
		return nil, errors.New("composio: ExecuteTool: toolSlug is required")
	}
	if req.ConnectedAccountID == "" && req.UserID == "" {
		return nil, errors.New("composio: ExecuteTool: either ConnectedAccountID or UserID must be set")
	}
	var out ExecuteToolResponse
	if err := c.do(c.newRequest(ctx).SetBody(req),
		http.MethodPost, "/tools/execute/"+url.PathEscape(toolSlug), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
