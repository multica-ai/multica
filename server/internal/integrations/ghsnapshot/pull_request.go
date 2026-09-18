package ghsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// FetchPullRequest reads current metadata using the installation's repository
// permissions. It never follows redirects with an installation credential.
func (c *Client) FetchPullRequest(ctx context.Context, installationID int64, owner, repo string, number int32) (json.RawMessage, error) {
	if !c.Enabled() {
		return nil, errors.New("GitHub App credentials unavailable")
	}
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", strings.TrimRight(c.apiBase, "/"), url.PathEscape(owner), url.PathEscape(repo), number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub PR request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		return nil, rateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub PR status %d", resp.StatusCode)
	}
	var raw json.RawMessage
	err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw)
	return raw, err
}
