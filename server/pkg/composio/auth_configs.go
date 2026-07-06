package composio

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// AuthConfig mirrors a subset of a Composio auth config — the project-level
// record that defines HOW users authenticate with a toolkit (the OAuth client,
// API-key scheme, etc.). The connect-link flow needs its opaque `id` (ac_…);
// the other fields drive selection when a toolkit has more than one.
//
// Spec: https://docs.composio.dev/reference/v3/api-reference/auth-configs/getAuthConfigs
type AuthConfig struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Toolkit carries at least the slug (and a logo) the config belongs to.
	Toolkit    Toolkit `json:"toolkit"`
	AuthScheme string  `json:"auth_scheme,omitempty"`
	// IsComposioManaged is true for Composio's managed OAuth app and false for a
	// custom (bring-your-own client_id/secret) config — the white-label case.
	IsComposioManaged bool `json:"is_composio_managed"`
	// Status is "ENABLED" or "DISABLED". The list endpoint hides disabled
	// configs by default (show_disabled=false).
	Status        string `json:"status,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	LastUpdatedAt string `json:"last_updated_at,omitempty"`
}

// ListAuthConfigsRequest collects the optional filters of GET /auth_configs.
// Zero values are omitted from the query string.
type ListAuthConfigsRequest struct {
	// ToolkitSlugs filters to specific toolkits; sent as a single
	// comma-separated `toolkit_slug` query param per the v3 spec.
	ToolkitSlugs []string
	// IsComposioManaged, when non-nil, filters by managed vs custom configs.
	IsComposioManaged *bool
	// ShowDisabled includes disabled configs (default false = enabled only).
	ShowDisabled bool
	// Search matches auth configs by name or id.
	Search string
	// Limit is the page size (max 1000 upstream). 0 = upstream default.
	Limit int
	// Cursor pages through results.
	Cursor string
}

// ListAuthConfigsResponse is the typed paginated response.
type ListAuthConfigsResponse struct {
	Items      []AuthConfig `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
	TotalItems int          `json:"total_items,omitempty"`
}

// ListAuthConfigs returns the auth configs registered in the project, with
// optional filters. The project is resolved from the x-api-key (a project API
// key authenticates to exactly one project), so no project id is passed.
func (c *Client) ListAuthConfigs(ctx context.Context, req ListAuthConfigsRequest) (*ListAuthConfigsResponse, error) {
	q := url.Values{}
	if len(req.ToolkitSlugs) > 0 {
		q.Set("toolkit_slug", strings.Join(req.ToolkitSlugs, ","))
	}
	if req.IsComposioManaged != nil {
		q.Set("is_composio_managed", strconv.FormatBool(*req.IsComposioManaged))
	}
	if req.ShowDisabled {
		q.Set("show_disabled", "true")
	}
	if req.Search != "" {
		q.Set("search", req.Search)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}

	path := "/auth_configs"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out ListAuthConfigsResponse
	if err := c.do(c.newRequest(ctx), http.MethodGet, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
