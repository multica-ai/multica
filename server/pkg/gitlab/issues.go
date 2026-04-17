package gitlab

import (
	"context"
	"fmt"
	"net/url"
)

type Issue struct {
	ID          int64    `json:"id"`
	IID         int      `json:"iid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"`
	Labels      []string `json:"labels"`
	Assignees   []User   `json:"assignees"`
	Author      User     `json:"author"`
	DueDate     string   `json:"due_date"`
	UpdatedAt   string   `json:"updated_at"`
	CreatedAt   string   `json:"created_at"`
	WebURL      string   `json:"web_url"`
}

type ListIssuesParams struct {
	State        string
	UpdatedAfter string
}

func (c *Client) ListIssues(ctx context.Context, token string, projectID int64, params ListIssuesParams) ([]Issue, error) {
	state := params.State
	if state == "" {
		state = "all"
	}
	q := url.Values{}
	q.Set("state", state)
	q.Set("per_page", "100")
	q.Set("order_by", "updated_at")
	q.Set("sort", "asc")
	if params.UpdatedAfter != "" {
		q.Set("updated_after", params.UpdatedAfter)
	}
	path := fmt.Sprintf("/projects/%d/issues?%s", projectID, q.Encode())

	var all []Issue
	err := iteratePages[Issue](ctx, c, token, path, func(batch []Issue) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}
