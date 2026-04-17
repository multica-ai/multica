package gitlab

import "context"

// User mirrors the subset of GET /api/v4/user we care about.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// CurrentUser returns the user the given token is authenticated as.
func (c *Client) CurrentUser(ctx context.Context, token string) (*User, error) {
	var u User
	if err := c.get(ctx, token, "/user", &u); err != nil {
		return nil, err
	}
	return &u, nil
}
