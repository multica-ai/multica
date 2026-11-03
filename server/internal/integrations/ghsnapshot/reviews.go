package ghsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The reviews endpoint returns an array, unlike the check collection envelopes.
// Fetch every page before publishing; a failed/partial fetch preserves the old
// atomic snapshot. Requests stay on the configured API host and are bounded.
func fetchPRReviews(ctx context.Context, c *Client, installationID int64, path, author string) ([]PRReview, error) {
	if author == "" {
		return nil, errors.New("ghsnapshot: missing pull request author")
	}
	reviews := make([]PRReview, 0)
	seen := make(map[int64]bool)
	for page := 1; page <= maxSnapshotContextPages; page++ {
		data, err := c.restGet(ctx, installationID, fmt.Sprintf("%s?per_page=%d&page=%d", path, snapshotPageSize, page))
		if err != nil {
			return nil, err
		}
		var entries []struct {
			ID          int64     `json:"id"`
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submitted_at"`
			User        struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"user"`
		}
		if json.Unmarshal(data, &entries) != nil || entries == nil || len(entries) > snapshotPageSize {
			return nil, errors.New("ghsnapshot: malformed review collection")
		}
		for _, entry := range entries {
			if entry.ID <= 0 || seen[entry.ID] {
				return nil, errors.New("ghsnapshot: invalid or duplicate review ID")
			}
			seen[entry.ID] = true
			// Unknown/deleted identities fail closed. Drafts, comments and
			// dismissed decisions never satisfy review wakeup conditions.
			if entry.User.Type != "User" || entry.User.Login == "" || strings.EqualFold(entry.User.Login, author) ||
				(entry.State != "APPROVED" && entry.State != "CHANGES_REQUESTED") {
				continue
			}
			if entry.SubmittedAt.IsZero() {
				return nil, errors.New("ghsnapshot: submitted review is missing its timestamp")
			}
			reviews = append(reviews, PRReview{ID: entry.ID, State: entry.State, SubmittedAt: entry.SubmittedAt})
		}
		if len(entries) < snapshotPageSize {
			return reviews, nil
		}
	}
	return nil, errors.New("ghsnapshot: review pagination exceeds page limit")
}
