package gitlab

import (
	"context"
	"fmt"
	"net/http"
)

type Note struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	System    bool   `json:"system"`
	Author    User   `json:"author"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (c *Client) ListNotes(ctx context.Context, token string, projectID int64, issueIID int) ([]Note, error) {
	var all []Note
	path := fmt.Sprintf("/projects/%d/issues/%d/notes?per_page=100&sort=asc&order_by=created_at", projectID, issueIID)
	err := iteratePages[Note](ctx, c, token, path, func(batch []Note) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}

// CreateNote sends POST /api/v4/projects/:id/issues/:iid/notes with the given
// body text, returning the created Note as GitLab persisted it.
func (c *Client) CreateNote(ctx context.Context, token string, projectID int64, issueIID int, body string) (*Note, error) {
	path := fmt.Sprintf("/projects/%d/issues/%d/notes", projectID, issueIID)
	payload := map[string]any{"body": body}
	var out Note
	if err := c.do(ctx, http.MethodPost, token, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
