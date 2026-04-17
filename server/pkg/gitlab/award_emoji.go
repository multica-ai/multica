package gitlab

import (
	"context"
	"fmt"
)

type AwardEmoji struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	User      User   `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (c *Client) ListAwardEmoji(ctx context.Context, token string, projectID int64, issueIID int) ([]AwardEmoji, error) {
	var all []AwardEmoji
	path := fmt.Sprintf("/projects/%d/issues/%d/award_emoji?per_page=100", projectID, issueIID)
	err := iteratePages[AwardEmoji](ctx, c, token, path, func(batch []AwardEmoji) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}
