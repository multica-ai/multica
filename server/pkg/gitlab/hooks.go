package gitlab

import (
	"context"
	"errors"
	"fmt"
)

// ProjectHook mirrors GitLab's project hook representation.
type ProjectHook struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

// CreateProjectHookInput is the body for POST /projects/:id/hooks.
// Only the fields we care about are listed; GitLab accepts more.
type CreateProjectHookInput struct {
	URL                      string `json:"url"`
	Token                    string `json:"token"`
	IssuesEvents             bool   `json:"issues_events"`
	ConfidentialIssuesEvents bool   `json:"confidential_issues_events"`
	NoteEvents               bool   `json:"note_events"`
	ConfidentialNoteEvents   bool   `json:"confidential_note_events"`
	EmojiEvents              bool   `json:"emoji_events"`
	ReleasesEvents           bool   `json:"releases_events"`
	// LabelEvents fires for project-level label CRUD. Required by the
	// Label Hook handler, which keeps gitlab_label cache in sync.
	LabelEvents           bool `json:"label_events"`
	EnableSSLVerification bool `json:"enable_ssl_verification"`
}

// CreateProjectHook registers a webhook on the given project.
func (c *Client) CreateProjectHook(ctx context.Context, token string, projectID int64, input CreateProjectHookInput) (*ProjectHook, error) {
	var out ProjectHook
	path := fmt.Sprintf("/projects/%d/hooks", projectID)
	if err := c.do(ctx, "POST", token, path, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProjectHook removes a webhook by its hook ID. Treats 404 as
// success — disconnect should be idempotent against hooks that were
// already removed out of band (e.g. by a project admin in GitLab UI).
func (c *Client) DeleteProjectHook(ctx context.Context, token string, projectID int64, hookID int64) error {
	path := fmt.Sprintf("/projects/%d/hooks/%d", projectID, hookID)
	err := c.do(ctx, "DELETE", token, path, nil, nil)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
