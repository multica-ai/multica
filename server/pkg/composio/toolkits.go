package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// Toolkit is the minimal toolkit descriptor used as a nested field inside
// connected accounts, sessions, and the toolkit list endpoint. Only fields
// useful for UI / dispatch decisions are typed.
type Toolkit struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name,omitempty"`
	LogoURL     string         `json:"logo,omitempty"`
	Description string         `json:"description,omitempty"`
	Categories  []string       `json:"categories,omitempty"`
	AuthSchemes []string       `json:"auth_schemes,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}

// ListToolkitsRequest carries the optional filters of GET /toolkits.
type ListToolkitsRequest struct {
	Category string
	Limit    int
	Cursor   string
	// SortBy is the upstream sort order. Per the v3.1 spec the valid enum
	// values are "usage" and "alphabetically".
	SortBy string
}

// ListToolkitsResponse is the typed paginated response.
type ListToolkitsResponse struct {
	Items      []Toolkit `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
	TotalItems int       `json:"total_items,omitempty"`
}

// ListToolkits returns toolkits available to the project.
func (c *Client) ListToolkits(ctx context.Context, req ListToolkitsRequest) (*ListToolkitsResponse, error) {
	q := url.Values{}
	if req.Category != "" {
		q.Set("category", req.Category)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}
	if req.SortBy != "" {
		q.Set("sort_by", req.SortBy)
	}
	path := "/toolkits"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out ListToolkitsResponse
	if err := c.do(c.newRequest(ctx), http.MethodGet, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetToolkit fetches a single toolkit by its slug (e.g. "notion", "github").
func (c *Client) GetToolkit(ctx context.Context, slug string) (*Toolkit, error) {
	if slug == "" {
		return nil, errors.New("composio: GetToolkit: slug is required")
	}
	var out Toolkit
	if err := c.do(c.newRequest(ctx),
		http.MethodGet, "/toolkits/"+url.PathEscape(slug), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
