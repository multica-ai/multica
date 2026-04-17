package gitlab

import (
	"context"
	"fmt"
)

type ProjectMember struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (c *Client) ListProjectMembers(ctx context.Context, token string, projectID int64) ([]ProjectMember, error) {
	var all []ProjectMember
	path := fmt.Sprintf("/projects/%d/members/all?per_page=100", projectID)
	err := iteratePages[ProjectMember](ctx, c, token, path, func(batch []ProjectMember) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}
