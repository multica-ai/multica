package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CreateIssueInput is the body for POST /projects/:id/issues. Only fields
// we set are listed; GitLab accepts more.
type CreateIssueInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	// AssigneeIDs is the list of GitLab user IDs to assign. Empty when
	// Multica is assigning to an agent (we use the agent::<slug> label
	// instead).
	AssigneeIDs []int64 `json:"assignee_ids,omitempty"`
	DueDate     string  `json:"due_date,omitempty"`
}

// CreateIssue creates a new issue in the project and returns the GitLab
// representation (which the caller can run through the translator + cache
// upsert).
func (c *Client) CreateIssue(ctx context.Context, token string, projectID int64, input CreateIssueInput) (*Issue, error) {
	var out Issue
	path := fmt.Sprintf("/projects/%d/issues", projectID)
	if err := c.do(ctx, "POST", token, path, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateIssueInput mirrors GitLab's PUT /projects/:id/issues/:iid body.
// All fields are optional: omitted (nil / empty) means "do not touch".
//
// Labels use GitLab's additive/subtractive flags (add_labels / remove_labels)
// rather than the full-replacement "labels" field, so non-scoped labels the
// user has attached directly in GitLab survive a Multica-originated update.
type UpdateIssueInput struct {
	Title        *string
	Description  *string
	AddLabels    []string
	RemoveLabels []string
	AssigneeIDs  *[]int64
	DueDate      *string
	StateEvent   *string
}

// UpdateIssue sends PUT /api/v4/projects/:id/issues/:iid. Comma-joins label
// slices so GitLab accepts them (the API expects comma-separated strings for
// add_labels / remove_labels, not arrays).
func (c *Client) UpdateIssue(ctx context.Context, token string, projectID int64, iid int, in UpdateIssueInput) (*Issue, error) {
	payload := map[string]any{}
	if in.Title != nil {
		payload["title"] = *in.Title
	}
	if in.Description != nil {
		payload["description"] = *in.Description
	}
	if len(in.AddLabels) > 0 {
		payload["add_labels"] = strings.Join(in.AddLabels, ",")
	}
	if len(in.RemoveLabels) > 0 {
		payload["remove_labels"] = strings.Join(in.RemoveLabels, ",")
	}
	if in.AssigneeIDs != nil {
		payload["assignee_ids"] = *in.AssigneeIDs
	}
	if in.DueDate != nil {
		payload["due_date"] = *in.DueDate
	}
	if in.StateEvent != nil {
		payload["state_event"] = *in.StateEvent
	}

	var out Issue
	path := fmt.Sprintf("/projects/%d/issues/%d", projectID, iid)
	if err := c.do(ctx, "PUT", token, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteIssue sends DELETE /api/v4/projects/:id/issues/:iid. Treats 404 as
// success (idempotent delete — if the issue is already gone, that's the
// desired terminal state).
func (c *Client) DeleteIssue(ctx context.Context, token string, projectID int64, iid int) error {
	path := fmt.Sprintf("/projects/%d/issues/%d", projectID, iid)
	err := c.do(ctx, http.MethodDelete, token, path, nil, nil)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

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
