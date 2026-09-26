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

// ErrDisabled is returned by calls that need the GitHub App API on a
// deployment that has not configured it.
var ErrDisabled = errors.New("github app api not configured")

// PullRequestFile is one changed file of a pull request, as GitHub lists it.
type PullRequestFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	// Status is GitHub's: added, removed, modified, renamed, copied, changed
	// or unchanged.
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	// Patch holds the file's hunks, starting at the first "@@". GitHub leaves
	// it out for binary files and for files too large to diff.
	Patch string `json:"patch"`
}

const pullRequestFilesPageSize = 100

// PullRequestFiles lists a pull request's changed files, up to maxFiles.
// truncated reports that GitHub listed more than that. GitHub itself stops at
// 3000 files.
func (c *Client) PullRequestFiles(ctx context.Context, installationID int64, owner, repo string, number int32, maxFiles int) (files []PullRequestFile, truncated bool, err error) {
	if !c.Enabled() {
		return nil, false, ErrDisabled
	}
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, false, err
	}
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=%d&page=%d",
			strings.TrimRight(c.apiBase, "/"), url.PathEscape(owner), url.PathEscape(repo),
			number, pullRequestFilesPageSize, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, false, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, false, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return nil, false, rateLimitFromResponse(resp, c.now())
		}
		if resp.StatusCode != http.StatusOK {
			return nil, false, fmt.Errorf("github pull request files: unexpected status %d", resp.StatusCode)
		}
		if readErr != nil {
			return nil, false, fmt.Errorf("github pull request files: %w", readErr)
		}
		var batch []PullRequestFile
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, false, errors.New("github pull request files: malformed response")
		}
		for _, f := range batch {
			if len(files) >= maxFiles {
				return files, true, nil
			}
			files = append(files, f)
		}
		if len(batch) < pullRequestFilesPageSize {
			return files, false, nil
		}
	}
}

// PullRequestFiles lists a pull request's changed files through the
// pipeline's client. ErrDisabled when the App API is not configured.
func (m *Manager) PullRequestFiles(ctx context.Context, installationID int64, owner, repo string, number int32, maxFiles int) ([]PullRequestFile, bool, error) {
	if !m.Enabled() {
		return nil, false, ErrDisabled
	}
	return m.client.PullRequestFiles(ctx, installationID, owner, repo, number, maxFiles)
}
