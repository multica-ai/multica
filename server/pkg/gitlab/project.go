package gitlab

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Project mirrors the subset of GET /api/v4/projects/:id we care about.
type Project struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
	Description       string `json:"description"`
}

// GetProject looks up a project by numeric ID or by URL-encoded path
// ("group/project"). Also tolerates a full GitLab URL ("https://gitlab.com/
// group/project") by stripping the scheme + host before encoding — pasting
// the URL straight from the browser is the most common user mistake.
func (c *Client) GetProject(ctx context.Context, token, idOrPath string) (*Project, error) {
	// Numeric → use as-is; path → URL-encode slashes (GitLab convention).
	ref := idOrPath
	if _, err := strconv.ParseInt(idOrPath, 10, 64); err != nil {
		ref = url.PathEscape(strings.TrimPrefix(normalizeProjectPath(idOrPath), "/"))
	}
	var p Project
	if err := c.get(ctx, token, "/projects/"+ref, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// normalizeProjectPath strips a leading "http(s)://host" if present, so a
// pasted browser URL like "https://gitlab.com/group/project" becomes
// "/group/project". Returns the input unchanged if no scheme is present.
func normalizeProjectPath(s string) string {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	return u.Path
}
