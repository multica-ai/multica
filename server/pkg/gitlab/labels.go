package gitlab

import (
	"context"
	"fmt"
)

type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type CreateLabelInput struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

func (c *Client) ListLabels(ctx context.Context, token string, projectID int64) ([]Label, error) {
	var all []Label
	path := fmt.Sprintf("/projects/%d/labels?per_page=100", projectID)
	err := iteratePages[Label](ctx, c, token, path, func(batch []Label) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}

func (c *Client) CreateLabel(ctx context.Context, token string, projectID int64, input CreateLabelInput) (*Label, error) {
	var out Label
	path := fmt.Sprintf("/projects/%d/labels", projectID)
	if err := c.do(ctx, "POST", token, path, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
