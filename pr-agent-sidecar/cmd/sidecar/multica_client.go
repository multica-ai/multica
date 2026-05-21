package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Field shapes mirror server/internal/handler/issue.go:
//   - CreateIssueRequest (line 1045-1063): only Title is required; assignee_type
//     + assignee_id must both be set or both omitted.
//   - IssueResponse (line 27-55): we only consume the identifying fields.

type CreateIssueRequest struct {
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	AssigneeType string `json:"assignee_type,omitempty"`
	AssigneeID   string `json:"assignee_id,omitempty"`
}

type IssueResponse struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title,omitempty"`
}

type MulticaClient struct {
	baseURL     string
	pat         string
	workspaceID string
	http        *http.Client
}

func NewMulticaClient(baseURL, pat, workspaceID string) *MulticaClient {
	return &MulticaClient{
		baseURL:     baseURL,
		pat:         pat,
		workspaceID: workspaceID,
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

// CreateIssue calls POST {baseURL}/api/issues. Returns the created issue on
// 201; any other status (or network error) becomes an error. No internal
// retries — duplicate-prevention lives in the delivery dedup layer.
func (c *MulticaClient) CreateIssue(ctx context.Context, in CreateIssueRequest) (*IssueResponse, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/issues", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("X-Workspace-ID", c.workspaceID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call multica: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("multica responded %d: %s", resp.StatusCode, string(raw))
	}

	var out IssueResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode multica response: %w", err)
	}
	return &out, nil
}
