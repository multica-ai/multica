package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Toolkit is the minimal toolkit descriptor used as a nested field inside
// connected accounts, sessions, and the toolkit list endpoint. Only fields
// useful for UI / dispatch decisions are typed.
type Toolkit struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name,omitempty"`
	LogoURL     string         `json:"logo,omitempty"`
	Description string         `json:"description,omitempty"`
	AuthSchemes []string       `json:"auth_schemes,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
	// ComposioManagedAuthSchemes lists the schemes Composio can authenticate
	// with its own managed app — the toolkit is connectable without the project
	// bringing an OAuth client.
	ComposioManagedAuthSchemes []string `json:"composio_managed_auth_schemes,omitempty"`
	// NoAuth is true for toolkits that need no credential at all.
	NoAuth bool `json:"no_auth,omitempty"`
	// AuthConfigDetails describes each auth scheme. GET /toolkits/{slug}
	// reports schemes only here, without auth_schemes or no_auth.
	AuthConfigDetails []ToolkitAuthConfigDetail `json:"auth_config_details,omitempty"`
}

// ToolkitAuthConfigDetail is one auth scheme of a toolkit and the fields it
// needs.
type ToolkitAuthConfigDetail struct {
	Mode   string `json:"mode"`
	Fields struct {
		AuthConfigCreation struct {
			Required []ToolkitAuthField `json:"required"`
		} `json:"auth_config_creation"`
	} `json:"fields"`
}

// ToolkitAuthField is a credential field of an auth scheme.
type ToolkitAuthField struct {
	Name    string  `json:"name"`
	Default *string `json:"default"`
}

// AuthModes returns the auth schemes the toolkit offers, from auth_schemes
// (the list endpoint) or else auth_config_details (GET /toolkits/{slug}).
func (t Toolkit) AuthModes() []string {
	if len(t.AuthSchemes) > 0 {
		return t.AuthSchemes
	}
	modes := make([]string, 0, len(t.AuthConfigDetails))
	for _, d := range t.AuthConfigDetails {
		if d.Mode != "" {
			modes = append(modes, d.Mode)
		}
	}
	return modes
}

// NeedsProjectCredentials reports whether an auth config for mode can only be
// created with a project-level value, i.e. a required field with no default.
// It is false when the toolkit carries no details for mode.
func (t Toolkit) NeedsProjectCredentials(mode string) bool {
	for _, d := range t.AuthConfigDetails {
		if !strings.EqualFold(d.Mode, mode) {
			continue
		}
		for _, f := range d.Fields.AuthConfigCreation.Required {
			if f.Default == nil || *f.Default == "" {
				return true
			}
		}
	}
	return false
}

// ToolkitCategory is one category a toolkit is listed under.
type ToolkitCategory struct {
	ID   string
	Name string
}

// Logo returns the toolkit's logo URL: top-level on the toolkit nested in auth
// configs and tools, under meta in the toolkit list.
func (t Toolkit) Logo() string {
	if t.LogoURL != "" {
		return t.LogoURL
	}
	logo, _ := t.Meta["logo"].(string)
	return logo
}

// PrimaryCategory returns the first category the toolkit is listed under, or
// a zero value when it has none. The v3.1 list endpoint nests categories under
// meta as {id, name} objects.
func (t Toolkit) PrimaryCategory() ToolkitCategory {
	list, _ := t.Meta["categories"].([]any)
	for _, item := range list {
		switch v := item.(type) {
		case string:
			if v != "" {
				return ToolkitCategory{Name: v}
			}
		case map[string]any:
			id, _ := v["id"].(string)
			name, _ := v["name"].(string)
			if id != "" || name != "" {
				return ToolkitCategory{ID: id, Name: name}
			}
		}
	}
	return ToolkitCategory{}
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
