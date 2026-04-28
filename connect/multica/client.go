package multica

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a PAT-authenticated client for the Multica REST API.
type Client struct {
	BaseURL     string
	PAT         string
	WorkspaceID string
	HTTPClient  *http.Client
}

func NewClient(baseURL, pat, workspaceID string) *Client {
	return &Client{
		BaseURL:     baseURL,
		PAT:         pat,
		WorkspaceID: workspaceID,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.PAT)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", c.WorkspaceID)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// Issue types

type CreateIssueRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Priority    string  `json:"priority,omitempty"`
	Status      string  `json:"status,omitempty"`
}

type Issue struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Prefix string `json:"prefix"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type issueWrapper struct {
	Issue Issue `json:"issue"`
}

func (c *Client) CreateIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	if req.Status == "" {
		req.Status = "todo"
	}
	if req.Priority == "" {
		req.Priority = "none"
	}
	data, err := c.do(ctx, "POST", "/api/issues", req)
	if err != nil {
		return nil, err
	}
	var w issueWrapper
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("unmarshal issue: %w", err)
	}
	return &w.Issue, nil
}

// Chat session types

type CreateChatSessionRequest struct {
	Title string `json:"title,omitempty"`
}

type ChatSession struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (c *Client) CreateChatSession(ctx context.Context, title string) (*ChatSession, error) {
	data, err := c.do(ctx, "POST", "/api/chat/sessions", CreateChatSessionRequest{Title: title})
	if err != nil {
		return nil, err
	}
	var s ChatSession
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &s, nil
}

type SendChatMessageRequest struct {
	Content string `json:"content"`
}

type ChatMessage struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Role    string `json:"role"`
}

func (c *Client) SendChatMessage(ctx context.Context, sessionID, content string) (*ChatMessage, error) {
	data, err := c.do(ctx, "POST", "/api/chat/sessions/"+sessionID+"/messages", SendChatMessageRequest{Content: content})
	if err != nil {
		return nil, err
	}
	var m ChatMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &m, nil
}

// Search issues

type SearchIssuesResponse struct {
	Issues []Issue `json:"issues"`
}

func (c *Client) SearchIssues(ctx context.Context, query string) ([]Issue, error) {
	data, err := c.do(ctx, "GET", "/api/issues/search?q="+query, nil)
	if err != nil {
		return nil, err
	}
	var resp SearchIssuesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal search: %w", err)
	}
	return resp.Issues, nil
}

// GetIssue retrieves a single issue by ID or identifier.
func (c *Client) GetIssue(ctx context.Context, id string) (*Issue, error) {
	data, err := c.do(ctx, "GET", "/api/issues/"+id, nil)
	if err != nil {
		return nil, err
	}
	var w issueWrapper
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("unmarshal issue: %w", err)
	}
	return &w.Issue, nil
}
