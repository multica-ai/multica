package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
